// 配置加载与云端厂商注册（对应 Python 版 cloud.py 的 Go 实现）。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GlobalConfig 全局配置（config.yaml）。
type GlobalConfig struct {
	Host               string
	Port               int
	GatewayAPIKey      string
	CORSAllowOrigin    string
	RateLimitPerMinute int
	Aliases            map[string]string
	DefaultModel       string
	Agent              *AgentConfig
}

func defaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Host:               "127.0.0.1",
		Port:               8000,
		CORSAllowOrigin:    "*",
		RateLimitPerMinute: 60,
		DefaultModel:       "deepseek/deepseek-chat",
		Agent:              &AgentConfig{Enabled: false, TimeoutSeconds: 30, MaxOutputBytes: 65536},
	}
}

func loadGlobalConfig(path string) *GlobalConfig {
	cfg := defaultGlobalConfig()
	m, err := loadYAMLFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "警告：config.yaml 解析失败，已回退默认配置（host=127.0.0.1/port=8000/agent 关闭）：", err)
		return cfg
	}
	if srv, ok := m["server"].(map[string]any); ok {
		cfg.Host = yamlStr(srv, "host")
		if cfg.Host == "" {
			cfg.Host = "127.0.0.1"
		}
		if p := yamlInt(srv, "port", 0); p != 0 {
			cfg.Port = p
		}
	}
	cfg.GatewayAPIKey = yamlStr(m, "gateway_api_key")
	if co := yamlStr(m, "cors_allow_origin"); co != "" {
		cfg.CORSAllowOrigin = co
	}
	if rl := yamlInt(m, "rate_limit_per_minute", -1); rl >= 0 {
		cfg.RateLimitPerMinute = rl
	}
	if aliases, ok := m["aliases"].(map[string]any); ok {
		cfg.Aliases = map[string]string{}
		for k, v := range aliases {
			cfg.Aliases[k] = toStr(v)
		}
	}
	if dm := yamlStr(m, "default_model"); dm != "" {
		cfg.DefaultModel = dm
	}
	if agent, ok := m["agent"].(map[string]any); ok {
		cfg.Agent.Enabled = yamlBool(agent, "enabled", false)
		if ac, ok := agent["allow_commands"].([]any); ok {
			cfg.Agent.AllowCommands = nil
			for _, c := range ac {
				cfg.Agent.AllowCommands = append(cfg.Agent.AllowCommands, toStr(c))
			}
		}
		if t := yamlInt(agent, "timeout_seconds", 0); t > 0 {
			cfg.Agent.TimeoutSeconds = t
		}
		if mo := yamlInt(agent, "max_output_bytes", 0); mo > 0 {
			cfg.Agent.MaxOutputBytes = mo
		}
		cfg.Agent.WorkDir = yamlStr(agent, "work_dir")
	}
	return cfg
}

// Provider 云端厂商。
type Provider struct {
	Name       string
	BaseURL    string
	Models     []string
	Vision     map[string]bool
	Keys       []APIKey
	// 可替换的调用实现（默认走 postJSON/postStream；测试可注入桩）。
	ChatHandler   func(p *Provider, model string, messages []map[string]any,
		keySelector string, temperature *float64, maxTokens *int, topP *float64,
		streamOptions map[string]any) (map[string]any, error)
	StreamHandler func(p *Provider, model string, messages []map[string]any,
		keySelector string, temperature *float64, maxTokens *int, topP *float64,
		streamOptions map[string]any, onChunk func(map[string]any) error) error
}

// APIKey 一把云端 Key。
type APIKey struct {
	Name string
	Key  string
}

func resolveEnv(value string) string {
	if len(value) > 3 && strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return os.Getenv(value[2 : len(value)-1])
	}
	return value
}

func parseProviderKeys(cfg map[string]any) []APIKey {
	keys := []APIKey{}
	if raw, ok := cfg["api_keys"].([]any); ok {
		for i, item := range raw {
			if m, ok := item.(map[string]any); ok {
				name := yamlStr(m, "name")
				if name == "" {
					name = "key-" + itoa(i+1)
				}
				keys = append(keys, APIKey{Name: name, Key: resolveEnv(yamlStr(m, "key"))})
			}
		}
	}
	if len(keys) == 0 {
		if k := resolveEnv(yamlStr(cfg, "api_key")); k != "" {
			keys = append(keys, APIKey{Name: "default", Key: k})
		}
	}
	if len(keys) == 0 {
		keys = append(keys, APIKey{Name: "default", Key: ""})
	}
	return keys
}

func (p *Provider) defaultKey() APIKey {
	for _, k := range p.Keys {
		if k.Key != "" {
			return k
		}
	}
	if len(p.Keys) == 0 {
		return APIKey{}
	}
	return p.Keys[0]
}

func (p *Provider) resolveKey(selector string) (APIKey, error) {
	if selector == "" {
		return p.defaultKey(), nil
	}
	for _, k := range p.Keys {
		if k.Name == selector {
			return k, nil
		}
	}
	if isDigits(selector) {
		idx := atoi(selector)
		if idx >= 0 && idx < len(p.Keys) {
			return p.Keys[idx], nil
		}
		return APIKey{}, newPluginError("厂商 "+p.Name+" 的 API Key 索引越界: "+selector, 400, "invalid_request_error")
	}
	return APIKey{}, newPluginError("厂商 "+p.Name+" 未找到名为 "+selector+" 的 API Key", 400, "invalid_request_error")
}

func (p *Provider) keyNames() []map[string]any {
	out := []map[string]any{}
	for _, k := range p.Keys {
		out = append(out, map[string]any{"name": k.Name, "configured": k.Key != ""})
	}
	return out
}

func (p *Provider) validate() error {
	if p.BaseURL == "" {
		return newPluginError("厂商 "+p.Name+" 缺少 base_url 配置", 500, "config_error")
	}
	hasKey := false
	for _, k := range p.Keys {
		if k.Key != "" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		return newPluginError("厂商 "+p.Name+" 未配置有效的 api_key / api_keys", 401, "authentication_error")
	}
	if len(p.Models) == 0 {
		return newPluginError("厂商 "+p.Name+" 的 models 列表为空", 500, "config_error")
	}
	return nil
}

// CloudRegistry 厂商注册表。
type CloudRegistry struct {
	Providers   map[string]*Provider
	ModelRoutes map[string]*modelRoute
}

type modelRoute struct {
	Provider *Provider
	Model    string
}

func newCloudRegistry(pluginsDir string) *CloudRegistry {
	reg := &CloudRegistry{
		Providers:   map[string]*Provider{},
		ModelRoutes: map[string]*modelRoute{},
	}
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return reg
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		cfgPath := filepath.Join(pluginsDir, name, "config.yaml")
		cfg, err := loadYAMLFile(cfgPath)
		if err != nil {
			continue
		}
		provider := &Provider{
			Name:    name,
			BaseURL: strings.TrimRight(yamlStr(cfg, "base_url"), "/"),
			Keys:    parseProviderKeys(cfg),
			Vision:  map[string]bool{},
		}
		if ms, ok := cfg["models"].([]any); ok {
			for _, m := range ms {
				provider.Models = append(provider.Models, toStr(m))
			}
		}
		if vs, ok := cfg["vision_models"].([]any); ok {
			for _, v := range vs {
				provider.Vision[toStr(v)] = true
			}
		}
		reg.Providers[name] = provider
		for _, m := range provider.Models {
			reg.ModelRoutes[m] = &modelRoute{Provider: provider, Model: m}
			reg.ModelRoutes[name+"/"+m] = &modelRoute{Provider: provider, Model: m}
		}
	}
	return reg
}

func (r *CloudRegistry) resolve(model string, aliases map[string]string) (*Provider, string, error) {
	resolved := model
	if aliases != nil {
		if v, ok := aliases[model]; ok {
			resolved = v
		}
	}
	if idx := strings.Index(resolved, "/"); idx >= 0 {
		providerName := resolved[:idx]
		actual := resolved[idx+1:]
		provider, ok := r.Providers[providerName]
		if !ok {
			return nil, "", newPluginError("未知厂商: "+providerName, 404, "invalid_request_error")
		}
		return provider, actual, nil
	}
	route, ok := r.ModelRoutes[resolved]
	if !ok {
		return nil, "", newPluginError("未知模型: "+model, 404, "invalid_request_error")
	}
	return route.Provider, route.Model, nil
}

func (r *CloudRegistry) modelList() []map[string]any {
	data := []map[string]any{}
	names := []string{}
	for n := range r.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := r.Providers[name]
		keys := p.keyNames()
		for _, m := range p.Models {
			data = append(data, map[string]any{
				"id":       name + "/" + m,
				"object":   "model",
				"owned_by": name,
				"api_keys": keys,
				"vision":   p.Vision[m],
			})
		}
	}
	return data
}

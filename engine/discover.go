package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 模型自动发现。
//
// 思路：厂商的 OpenAI 兼容端点都提供 GET {base_url}/models，需要 Bearer 鉴权。
// 拿一把 Key 去打各家这个端点——HTTP 200 就说明 Key 属于这家，同时响应里就是
// 此刻真实可用的模型列表。这样模型清单不必硬编码在仓库里，厂商上新也不用发版。
//
// 注意：这是只读探测，不产生推理计费。

const (
	// 单家探测超时。并发执行，整体耗时约等于最慢的一家。
	discoverTimeout = 10 * time.Second
	modelsCacheName = "models_cache.json"
	modelsCacheVer  = 1
)

// probeTarget 一家厂商的探测目标。
type probeTarget struct {
	Name    string
	BaseURL string
}

// builtinProbes 内置的已知厂商地址，用于「配置里还没有这家插件」时的兜底。
// 已有的 plugins/<厂商>/config.yaml 会覆盖同名项。
var builtinProbes = []probeTarget{
	{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1"},
	{Name: "kimi", BaseURL: "https://api.moonshot.cn/v1"},
	{Name: "openai", BaseURL: "https://api.openai.com/v1"},
	{Name: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
}

// probeAttempt 一次探测的记录，用于告诉用户「为什么不是这家」。
type probeAttempt struct {
	Provider string `json:"provider"`
	Status   int    `json:"status"`
	OK       bool   `json:"ok"`
	Models   int    `json:"models"`
	Error    string `json:"error,omitempty"`
}

// discoveredModel 探测到的模型。
type discoveredModel struct {
	ID     string `json:"id"`
	Vision bool   `json:"vision"`
}

// discoveryResult 一次完整发现的结果。
type discoveryResult struct {
	Provider  string            `json:"provider"`
	BaseURL   string            `json:"base_url"`
	Models    []discoveredModel `json:"models"`
	ElapsedMS int64             `json:"elapsed_ms"`
	Attempts  []probeAttempt    `json:"attempts"`
}

// providerTargets 合成探测目标：先取配置里已有的厂商（地址以配置为准），
// 再补内置表中缺失的厂商。返回按名称排序的稳定顺序。
func providerTargets(reg *CloudRegistry) []probeTarget {
	seen := map[string]bool{}
	out := []probeTarget{}
	if reg != nil {
		names := make([]string, 0, len(reg.Providers))
		for n := range reg.Providers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			p := reg.Providers[n]
			if p == nil || p.BaseURL == "" {
				continue
			}
			seen[n] = true
			out = append(out, probeTarget{Name: n, BaseURL: p.BaseURL})
		}
	}
	for _, t := range builtinProbes {
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		out = append(out, t)
	}
	return out
}

// probeModelsURL 拼出 {base_url}/models，兼容 base_url 已带 /v1 或未带。
func probeModelsURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/models"
	}
	return base + "/v1/models"
}

// probeHTTPGet 发一个 GET，返回响应体与状态码。不走搜索的那套措辞。
func probeHTTPGet(url string, headers map[string]string, timeout time.Duration) ([]byte, int, error) {
	req, err := buildRequest("GET", url, headers, nil)
	if err != nil {
		return nil, 0, err
	}
	client := &netClient{timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.Status, err
	}
	return data, resp.Status, nil
}

// probeProvider 用一把 Key 探测一家厂商。
// 返回 (模型 ID 列表, HTTP 状态码, 错误)。状态码非 200 不算错误——只代表「不是这家」。
func probeProvider(t probeTarget, key string) ([]string, int, error) {
	url := probeModelsURL(t.BaseURL)
	headers := map[string]string{
		"Authorization": "Bearer " + key,
		"Accept":        "application/json",
		"User-Agent":    "GeneralHarness/" + engineVersion,
	}
	data, status, err := probeHTTPGet(url, headers, discoverTimeout)
	if err != nil {
		return nil, status, err
	}
	if status != 200 {
		return nil, status, nil
	}
	// 标准形态：{"object":"list","data":[{"id":"...","object":"model",...}]}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, status, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if strings.TrimSpace(m.ID) != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, status, nil
}

// discoverByKey 并发探测所有目标，返回第一个 200 的厂商及其模型。
//
// 慢厂商不拖累快厂商：一旦某家返回 200 就记下答案，再等最多 900ms 收集其余家的
// 状态（好让用户看到“为什么不是那几家”），然后立刻返回。否则被墙或被限流的
// 厂商会让整个识别卡满超时（实测 openai 不可达时原本要等 10 秒）。
func discoverByKey(key string, targets []probeTarget) *discoveryResult {
	start := time.Now()
	type outcome struct {
		target probeTarget
		models []string
		status int
		err    error
	}
	ch := make(chan outcome, len(targets))
	for _, t := range targets {
		go func(t probeTarget) {
			models, status, err := probeProvider(t, key)
			ch <- outcome{target: t, models: models, status: status, err: err}
		}(t)
	}

	res := &discoveryResult{Attempts: make([]probeAttempt, 0, len(targets))}
	var winner *outcome
	var grace <-chan time.Time
	hard := time.After(discoverTimeout + 2*time.Second)

collect:
	for answered := 0; answered < len(targets); {
		var graceC <-chan time.Time
		if winner != nil {
			graceC = grace
		}
		select {
		case o := <-ch:
			answered++
			ok := o.status == 200 && o.err == nil
			att := probeAttempt{
				Provider: o.target.Name,
				Status:   o.status,
				OK:       ok,
				Models:   len(o.models),
			}
			if o.err != nil {
				att.Error = o.err.Error()
			} else if o.status != 200 {
				att.Error = statusHint(o.status)
			}
			res.Attempts = append(res.Attempts, att)
			if ok && winner == nil {
				hit := o
				winner = &hit
				grace = time.After(900 * time.Millisecond)
			}
		case <-graceC:
			break collect
		case <-hard:
			break collect
		}
	}

	if winner != nil {
		res.Provider = winner.target.Name
		res.BaseURL = winner.target.BaseURL
		res.Models = make([]discoveredModel, 0, len(winner.models))
		for _, id := range winner.models {
			res.Models = append(res.Models, discoveredModel{ID: id, Vision: guessVision(id)})
		}
	}
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res
}

// statusHint 把「不是这家」的状态码翻译成人话。
func statusHint(status int) string {
	switch status {
	case 401:
		return "Key 无效或不属于这家厂商"
	case 403:
		return "无权限访问 /models"
	case 404:
		return "该地址没有 /models 端点"
	case 429:
		return "被限流"
	case 0:
		return "网络不可达"
	}
	if status >= 500 {
		return "服务端错误"
	}
	return ""
}

// guessVision 从模型名推断是否支持图片输入。
// /models 不返回能力字段，只能按命名约定猜；猜错的代价只是列表少一个 [视觉] 标记。
func guessVision(id string) bool {
	s := strings.ToLower(id)
	for _, p := range []string{"vision", "vl-", "-vl", "4o", "omni", "multimodal", "llava", "pixtral", "image", "-it"} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// ---- 探测结果缓存 ----
//
// 缓存放 ROOT/models_cache.json，引擎启动时并入注册表，
// 这样重启后模型下拉里仍然有探测到的模型，不必每次重新探测。

type cachedProvider struct {
	ProbedAt string            `json:"probed_at"`
	BaseURL  string            `json:"base_url"`
	Models   []discoveredModel `json:"models"`
}

type modelsCache struct {
	Version   int                        `json:"version"`
	Providers map[string]*cachedProvider `json:"providers"`
}

func modelsCachePath(root string) string {
	return filepath.Join(root, modelsCacheName)
}

// loadModelsCache 读取缓存；文件不存在或解析失败返回空缓存（不报错）。
func loadModelsCache(root string) *modelsCache {
	cache := &modelsCache{Version: modelsCacheVer, Providers: map[string]*cachedProvider{}}
	data, err := os.ReadFile(modelsCachePath(root))
	if err != nil {
		return cache
	}
	var parsed modelsCache
	if err := json.Unmarshal(data, &parsed); err != nil {
		return cache
	}
	if parsed.Providers != nil {
		cache.Providers = parsed.Providers
	}
	return cache
}

// save 原子写入缓存。
func (c *modelsCache) save(root string) error {
	c.Version = modelsCacheVer
	if c.Providers == nil {
		c.Providers = map[string]*cachedProvider{}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := modelsCachePath(root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// put 记录一次探测结果。
func (c *modelsCache) put(provider, baseURL string, models []discoveredModel) {
	if c.Providers == nil {
		c.Providers = map[string]*cachedProvider{}
	}
	c.Providers[provider] = &cachedProvider{
		ProbedAt: time.Now().UTC().Format(time.RFC3339),
		BaseURL:  baseURL,
		Models:   models,
	}
}

// applyTo 把缓存里的模型并入注册表：
//   - 追加到 Provider.Models（去重，配置里已有的跳过）
//   - 注册裸名与「厂商/模型」两条路由，使 /v1/models 列出的都能直接 --model 使用
//
// 只处理配置里已存在的厂商；缓存里多出来的厂商忽略（没有 base_url 与 Key 也无从调用）。
func (c *modelsCache) applyTo(reg *CloudRegistry) int {
	if reg == nil || len(c.Providers) == 0 {
		return 0
	}
	added := 0
	names := make([]string, 0, len(c.Providers))
	for n := range c.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := reg.Providers[name]
		if p == nil {
			continue
		}
		existing := make(map[string]bool, len(p.Models))
		for _, m := range p.Models {
			existing[m] = true
		}
		for _, m := range c.Providers[name].Models {
			if m.ID == "" || existing[m.ID] {
				continue
			}
			existing[m.ID] = true
			p.Models = append(p.Models, m.ID)
			if m.Vision {
				if p.Vision == nil {
					p.Vision = map[string]bool{}
				}
				p.Vision[m.ID] = true
			}
			if reg.ModelRoutes != nil {
				reg.ModelRoutes[m.ID] = &modelRoute{Provider: p, Model: m.ID}
				reg.ModelRoutes[name+"/"+m.ID] = &modelRoute{Provider: p, Model: m.ID}
			}
			added++
		}
	}
	return added
}

// applyDiscoveryToRegistry 把一次探测结果并入注册表（供 API 实时生效）。
func applyDiscoveryToRegistry(reg *CloudRegistry, res *discoveryResult) int {
	if reg == nil || res == nil || res.Provider == "" {
		return 0
	}
	cache := &modelsCache{Providers: map[string]*cachedProvider{}}
	cache.put(res.Provider, res.BaseURL, res.Models)
	return cache.applyTo(reg)
}

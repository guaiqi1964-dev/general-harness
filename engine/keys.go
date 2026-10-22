package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// keys.yaml 是 GUI / CLI 写入的 Key 覆盖层，优先级高于 plugins/<厂商>/config.yaml。
// 单独一个文件的原因：宁可新增一个可删除的覆盖层，也不去改写用户手工维护、
// 带注释的插件配置（手写 YAML 写回会丢注释，且会破坏 ${ENV_VAR} 写法）。
const keysFileName = "keys.yaml"

func keysFilePath(root string) string {
	return filepath.Join(root, keysFileName)
}

// loadKeyOverrides 读取 keys.yaml；文件不存在或解析失败时返回空 map。
func loadKeyOverrides(root string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(keysFilePath(root))
	if err != nil {
		return out
	}
	m, err := parseYAMLString(string(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, "keys.yaml 解析失败，已忽略:", err)
		return out
	}
	for k, v := range m {
		if key := strings.TrimSpace(toStr(v)); key != "" {
			out[k] = key
		}
	}
	return out
}

// writeKeyOverrides 原子写入 keys.yaml（按键名排序，便于人工查看）。
// 文件权限 0600：Key 是凭据，不应对同机其他用户可读。
func writeKeyOverrides(root string, overrides map[string]string) error {
	names := make([]string, 0, len(overrides))
	for k := range overrides {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("# 由 General Harness 的 GUI / CLI 写入，优先级高于 plugins/<厂商>/config.yaml\n")
	b.WriteString("# 删除本文件即可回到插件目录里的 Key 配置\n")
	for _, name := range names {
		b.WriteString(name + ": " + quoteYAMLValue(overrides[name]) + "\n")
	}
	path := keysFilePath(root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// quoteYAMLValue 给含特殊字符的值加双引号，避免手写 YAML 解析器把 # 之类当注释。
func quoteYAMLValue(v string) string {
	plain := v != ""
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' || r == ':' || r == '/') {
			plain = false
			break
		}
	}
	if plain {
		return v
	}
	escaped := strings.ReplaceAll(v, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// maskKey 只回显首尾少量字符，避免接口把完整 Key 送回浏览器。
func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 12 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// placeholderKey 判断 Key 是否只是插件配置里的示例占位符（sk-xxxxxx / sk-yyyy）。
// 这类占位符非空，会被当成「已配置」，但真正调用上游一定 401，界面需要区分出来。
func placeholderKey(key string) bool {
	if !strings.HasPrefix(key, "sk-") || len(key) <= 3 {
		return false
	}
	for _, r := range key[3:] {
		if r != 'x' && r != 'X' && r != 'y' && r != 'Y' {
			return false
		}
	}
	return true
}

// applyKeyOverrides 把覆盖层里的 Key 应用到已加载的厂商（替换第一把 Key）。
func applyKeyOverrides(cloud *CloudRegistry, base map[string]APIKey, overrides map[string]string) {
	for name, p := range cloud.Providers {
		key, ok := overrides[name]
		if !ok {
			if b, has := base[name]; has && len(p.Keys) > 0 {
				p.Keys[0].Key = b.Key
			}
			continue
		}
		if len(p.Keys) == 0 {
			p.Keys = []APIKey{{Name: "default", Key: key}}
			continue
		}
		p.Keys[0].Key = key
	}
}

// baseKeys 记录插件配置里的原始第一把 Key，便于覆盖层删除后回退。
func baseKeys(cloud *CloudRegistry) map[string]APIKey {
	out := map[string]APIKey{}
	for name, p := range cloud.Providers {
		if len(p.Keys) > 0 {
			out[name] = p.Keys[0]
		} else {
			out[name] = APIKey{}
		}
	}
	return out
}

// refreshKeysIfChanged 在 keys.yaml 变更（含删除）后热加载，
// 让用户在 GUI / CLI 里改完 Key 不必重启引擎。
func (e *Engine) refreshKeysIfChanged() {
	if e.KeysPath == "" || e.Cloud == nil {
		return
	}
	st, err := os.Stat(e.KeysPath)
	root := filepath.Dir(e.KeysPath)
	if err != nil {
		// 文件被删除：退回插件配置里的 Key。
		if !e.keysMod.IsZero() {
			e.keysMod = time.Time{}
			e.keysSize = 0
			applyKeyOverrides(e.Cloud, e.baseKeys, nil)
			fmt.Fprintln(os.Stderr, "keys.yaml 已删除，回退到插件配置的 Key")
		}
		return
	}
	if st.ModTime().Equal(e.keysMod) && st.Size() == e.keysSize {
		return
	}
	e.keysMod = st.ModTime()
	e.keysSize = st.Size()
	applyKeyOverrides(e.Cloud, e.baseKeys, loadKeyOverrides(root))
	fmt.Fprintln(os.Stderr, "keys.yaml 变更已热加载")
}

// isLoopbackAddr 判断连接对端是否是本机（Key 管理接口只允许本机调用）。
func isLoopbackAddr(addr string) bool {
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}

// handleKeys 是本机 Key 管理接口：GET 查看状态，POST 写入覆盖层。
// 不校验网关鉴权（它本身就是用来配置凭据的），但限制只能从回环地址调用。
func (e *Engine) handleKeys(req *httpRequest, w *responseWriter) {
	if !isLoopbackAddr(clientIP(req)) {
		w.JSON(403, map[string]any{"error": map[string]any{
			"message": "Key 管理接口只允许本机访问", "type": "permission_error", "code": 403,
		}})
		return
	}
	// 每次访问都先对齐磁盘状态：用户在外部删除或改写 keys.yaml 也能立即反映。
	e.refreshKeysIfChanged()
	switch req.Method {
	case "GET":
		names := make([]string, 0, len(e.Cloud.Providers))
		for name := range e.Cloud.Providers {
			names = append(names, name)
		}
		sort.Strings(names)
		list := make([]map[string]any, 0, len(names))
		for _, name := range names {
			p := e.Cloud.Providers[name]
			k := p.defaultKey()
			list = append(list, map[string]any{
				"name":        name,
				"configured":  k.Key != "",
				"placeholder": placeholderKey(k.Key),
				"masked":      maskKey(k.Key),
				"models":      p.Models,
				"key_names":   p.keyNames(),
			})
		}
		w.JSON(200, map[string]any{"providers": list, "keys_file": e.KeysPath})
	case "POST":
		var body struct {
			Provider string `json:"provider"`
			Key      string `json:"key"`
		}
		if err := jsonBody(req, &body); err != nil {
			w.JSON(400, map[string]any{"error": map[string]any{
				"message": "请求体解析失败", "type": "invalid_request_error", "code": 400,
			}})
			return
		}
		provider := strings.TrimSpace(body.Provider)
		key := strings.TrimSpace(body.Key)
		if provider == "" || key == "" {
			w.JSON(400, map[string]any{"error": map[string]any{
				"message": "provider 与 key 都不能为空", "type": "invalid_request_error", "code": 400,
			}})
			return
		}
		if _, ok := e.Cloud.Providers[provider]; !ok {
			w.JSON(404, map[string]any{"error": map[string]any{
				"message": "未知的厂商: " + provider, "type": "not_found", "code": 404,
			}})
			return
		}
		root := filepath.Dir(e.KeysPath)
		overrides := loadKeyOverrides(root)
		overrides[provider] = key
		if err := writeKeyOverrides(root, overrides); err != nil {
			fmt.Fprintln(os.Stderr, "写入 keys.yaml 失败:", err)
			w.JSON(500, map[string]any{"error": map[string]any{
				"message": "写入 keys.yaml 失败", "type": "internal_error", "code": 500,
			}})
			return
		}
		e.keysMod = time.Time{}
		e.refreshKeysIfChanged()
		w.JSON(200, map[string]any{
			"ok": true, "provider": provider, "masked": maskKey(key), "applied": true,
		})
	default:
		w.JSON(405, map[string]any{"error": map[string]any{
			"message": "Method Not Allowed", "type": "method_not_allowed", "code": 405,
		}})
	}
}

// handleKeyDiscovery 用一把 Key 探测「这是哪家厂商」并拉回该 Key 真实可用的模型。
//
// 两种用法：
//   - {"key":"sk-..."}：拿这把 Key 挨家打 /models，第一个 200 的就是答案（自动识别厂商）。
//   - {"provider":"deepseek"}：只用已保存的 Key 刷新这一家的模型列表。
//
// 只发 GET /models，不产生推理计费。结果写入 models_cache.json 并立即并入注册表，
// 因此 /v1/models 与 /model 立刻就能看到新模型，不必重启引擎。
func (e *Engine) handleKeyDiscovery(req *httpRequest, w *responseWriter) {
	if !isLoopbackAddr(clientIP(req)) {
		w.JSON(403, map[string]any{"error": map[string]any{
			"message": "Key 管理接口只允许本机访问", "type": "permission_error", "code": 403,
		}})
		return
	}
	if req.Method != "POST" {
		w.JSON(405, map[string]any{"error": map[string]any{
			"message": "Method Not Allowed", "type": "method_not_allowed", "code": 405,
		}})
		return
	}
	var body struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if err := jsonBody(req, &body); err != nil {
		w.JSON(400, map[string]any{"error": map[string]any{
			"message": "请求体解析失败", "type": "invalid_request_error", "code": 400,
		}})
		return
	}
	e.refreshKeysIfChanged()

	provider := strings.TrimSpace(body.Provider)
	key := strings.TrimSpace(body.Key)

	var targets []probeTarget
	if provider != "" {
		p, ok := e.Cloud.Providers[provider]
		if !ok {
			w.JSON(404, map[string]any{"error": map[string]any{
				"message": "未知的厂商: " + provider, "type": "not_found", "code": 404,
			}})
			return
		}
		if key == "" {
			key = p.defaultKey().Key
		}
		targets = []probeTarget{{Name: p.Name, BaseURL: p.BaseURL}}
	} else {
		targets = providerTargets(e.Cloud)
	}
	if key == "" {
		w.JSON(400, map[string]any{"error": map[string]any{
			"message": "没有可用的 Key：请在请求里带上 key，或先保存该厂商的 Key",
			"type":    "invalid_request_error", "code": 400,
		}})
		return
	}

	result := discoverByKey(key, targets)
	if result.Provider == "" {
		w.JSON(404, map[string]any{"error": map[string]any{
			"message": "没有任何厂商接受这把 Key（详见 attempts）",
			"type":    "not_found", "code": 404,
			"attempts": result.Attempts,
		}})
		return
	}

	// 并入缓存与注册表：重启后依然保留，本进程内立即可用。
	root := filepath.Dir(e.KeysPath)
	if e.Models == nil {
		e.Models = loadModelsCache(root)
	}
	e.Models.put(result.Provider, result.BaseURL, result.Models)
	if err := e.Models.save(root); err != nil {
		fmt.Fprintln(os.Stderr, "写入 models_cache.json 失败:", err)
	}
	added := e.Models.applyTo(e.Cloud)
	if added > 0 {
		fmt.Fprintf(os.Stderr, "模型发现：%s 新增 %d 个模型\n", result.Provider, added)
	}

	w.JSON(200, map[string]any{
		"provider":   result.Provider,
		"base_url":   result.BaseURL,
		"models":     result.Models,
		"elapsed_ms": result.ElapsedMS,
		"attempts":   result.Attempts,
		"added":      added,
	})
}

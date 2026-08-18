// HTTP API 服务：路由与处理器（Go 版）。
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Engine 聚合所有能力。
type Engine struct {
	Config *GlobalConfig
	Cloud  *CloudRegistry
	GGUF   *GGUFRegistry
	Ollama *OllamaAdapter
	Usage  *UsageStore
	Rate   *RateLimiter
}

func newEngine(cfg *GlobalConfig, root string) *Engine {
	cloud := newCloudRegistry(root + "/plugins")
	gguf := newGGUFRegistry(root + "/models")
	gguf.scan()
	usage := newUsageStore(root + "/usage_stats.json")
	engine := &Engine{
		Config: cfg,
		Cloud:  cloud,
		GGUF:   gguf,
		Usage:  usage,
		Rate:   newRateLimiter(),
	}
	engine.Ollama = newOllamaAdapter(cloud, gguf, cfg.Aliases)
	return engine
}

func runServer(args []string) {
	cfg := loadGlobalConfig(ROOT + "/config.yaml")
	// 命令行覆盖 host/port
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--host":
			if i+1 < len(args) {
				cfg.Host = args[i+1]
				i++
			}
		case "--port":
			if i+1 < len(args) {
				cfg.Port, _ = strconv.Atoi(args[i+1])
				i++
			}
		}
	}
	engine := newEngine(cfg, ROOT)
	addr := cfg.Host + ":" + itoa(cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "监听失败:", err)
		os.Exit(1)
	}
	// 写入 PID 文件
	_ = os.WriteFile(ROOT+"/gateway.pid", []byte(itoa(os.Getpid())), 0o644)
	fmt.Printf("引擎就绪：%d 个云端厂商，%d 个本地 GGUF 模型，监听 %s\n",
		len(engine.Cloud.Providers), len(engine.GGUF.Models), addr)
	serve(ln, engine.dispatch)
}

// dispatch 请求路由。
func (e *Engine) dispatch(req *httpRequest, w *responseWriter) {
	// CORS 预检
	if req.Method == "OPTIONS" {
		w.writeHead(204, corsHeaders())
		return
	}
	path := req.Path
	// 统一限流（health 与根路径除外）。
	if path != "/health" && path != "/" && !e.rateLimit(req, w) {
		return
	}
	switch {
	case path == "/" || path == "/health":
		if path == "/health" {
			w.JSON(200, map[string]any{"status": "ok"})
		} else {
			w.JSON(200, map[string]any{"service": "General Harness", "docs": "/docs"})
		}
	case path == "/v1/models":
		e.handleModels(req, w)
	case path == "/v1/chat/completions":
		e.handleChatCompletions(req, w)
	case path == "/v1/usage/stats":
		e.handleUsageStats(req, w)
	case path == "/api/tags":
		e.handleOllamaTags(req, w)
	case path == "/api/chat":
		e.handleOllamaChat(req, w)
	case path == "/api/models":
		e.handleModels(req, w)
	case path == "/api/generate":
		e.handleOllamaGenerate(req, w)
	case strings.HasPrefix(path, "/api/gguf/"):
		e.handleGGUFInfo(req, w)
	default:
		w.JSON(404, map[string]any{"error": map[string]any{"message": "Not Found", "type": "not_found", "code": 404}})
	}
}

func corsHeaders() map[string]string {
	return map[string]string{
		"Access-Control-Allow-Origin":      "*",
		"Access-Control-Allow-Methods":     "GET, POST, OPTIONS",
		"Access-Control-Allow-Headers":     "*",
	}
}

// writeEngineError 把 error 写为统一 JSON 错误响应。
func writeEngineError(w *responseWriter, err error) {
	if pe, ok := err.(*PluginError); ok {
		w.JSON(pe.StatusCode, pe.asDict())
		return
	}
	w.JSON(500, map[string]any{
		"error": map[string]any{"message": err.Error(), "type": "internal_error", "code": 500},
	})
}

// checkAuth 网关鉴权。
func (e *Engine) checkAuth(req *httpRequest) bool {
	key := e.Config.GatewayAPIKey
	if key == "" {
		return true
	}
	auth := req.Headers["authorization"]
	token := ""
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		token = auth[len("Bearer "):]
	}
	if token == "" || token != key {
		return false
	}
	return true
}

// rateLimit 限流检查（返回 true 表示放行）。
func (e *Engine) rateLimit(req *httpRequest, w *responseWriter) bool {
	limit := e.Config.RateLimitPerMinute
	if limit <= 0 || req.Path == "/health" || req.Path == "/" {
		return true
	}
	ip := clientIP(req)
	if !e.Rate.Allow(ip, limit, 60) {
		w.JSON(429, map[string]any{
			"error": map[string]any{
				"message": fmt.Sprintf("请求过于频繁（单 IP %d 次/分钟），请稍后再试", limit),
				"type":    "rate_limit_error", "code": 429,
			},
		})
		return false
	}
	return true
}

func clientIP(req *httpRequest) string {
	host := req.Headers["x-forwarded-for"]
	if host != "" {
		if idx := strings.Index(host, ","); idx >= 0 {
			host = host[:idx]
		}
		return strings.TrimSpace(host)
	}
	// 无转发头时使用连接对端地址（去端口），避免所有本机客户端共享同一限流桶。
	if h, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		return h
	}
	return req.RemoteAddr
}

// ---- 模型列表 ----

func (e *Engine) handleModels(req *httpRequest, w *responseWriter) {
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": map[string]any{"message": "网关鉴权失败：缺少或错误的 API Key", "type": "authentication_error", "code": 401}})
		return
	}
	data := e.Cloud.modelList()
	// 附加本地 GGUF 模型
	for _, m := range e.GGUF.list() {
		data = append(data, map[string]any{
			"id":       "local/" + toStr(m["name"]),
			"object":   "model",
			"owned_by": "local",
			"api_keys": []map[string]any{},
			"vision":   false,
		})
	}
	w.JSON(200, map[string]any{"object": "list", "data": data})
}

// ---- 聊天补全 ----

type chatRequestBody struct {
	Model       string            `json:"model"`
	Messages    []map[string]any  `json:"messages"`
	Stream      bool              `json:"stream"`
	Temperature *float64          `json:"temperature"`
	MaxTokens   *int              `json:"max_tokens"`
	TopP        *float64          `json:"top_p"`
	SessionID   string            `json:"session_id"`
	StreamOpts  map[string]any    `json:"stream_options"`
}

func (e *Engine) handleChatCompletions(req *httpRequest, w *responseWriter) {
	if req.Method != "POST" {
		w.JSON(405, map[string]any{"error": map[string]any{"message": "Method Not Allowed", "type": "method_not_allowed", "code": 405}})
		return
	}
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": map[string]any{"message": "网关鉴权失败：缺少或错误的 API Key", "type": "authentication_error", "code": 401}})
		return
	}
	var body chatRequestBody
	if err := jsonBody(req, &body); err != nil {
		w.JSON(400, map[string]any{"error": map[string]any{"message": "请求体解析失败", "type": "invalid_request_error", "code": 400}})
		return
	}
	requestID := "req-" + randHex(6)
	sessionID := body.SessionID
	keySelector := req.Headers["x-gateway-api-key"]

	// 本地 GGUF 模型
	if body.Model != "" && e.GGUF.get(localName(body.Model)) != nil {
		reader := e.GGUF.get(localName(body.Model))
		reply := localGGUFReply(reader)
		if body.Stream {
			w.SSE()
			w.SSEEvent(mustJSON(map[string]any{"id": requestID, "content": reply, "model": body.Model}))
			w.Close()
			return
		}
		w.JSON(200, map[string]any{
			"id": requestID, "content": reply, "model": body.Model,
			"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
		})
		return
	}

	modelName := body.Model
	if modelName == "" {
		modelName = e.Config.DefaultModel
	}
	provider, actual, err := e.Cloud.resolve(modelName, e.Config.Aliases)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	selected, err := provider.resolveKey(keySelector)
	if err != nil {
		writeEngineError(w, err)
		return
	}

	if body.Stream {
		w.SSE()
		recorded := false
		err := provider.streamChatCompletion(actual, body.Messages, keySelector,
			body.Temperature, body.MaxTokens,
			func(chunk map[string]any) error {
				if !recorded {
					if usage, ok := chunk["usage"].(map[string]any); ok {
						recorded = true
						e.Usage.Record(sessionID, requestID, selected.Name, actual,
							int(toInt64(usage["prompt_tokens"])),
							int(toInt64(usage["completion_tokens"])),
							int(toInt64(usage["total_tokens"])), 0)
					}
				}
				w.SSEEvent(mustJSON(chunk))
				return nil
			})
		if err != nil {
			if pe, ok := err.(*PluginError); ok {
				w.SSEEvent(mustJSON(pe.asDict()))
			} else {
				w.SSEEvent(mustJSON(map[string]any{"error": map[string]any{"message": err.Error(), "type": "internal_error", "code": 500}}))
			}
		}
		w.Close()
		return
	}

	resp, err := provider.chatCompletion(actual, body.Messages, keySelector,
		body.Temperature, body.MaxTokens)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	if usage, ok := resp["usage"].(map[string]any); ok {
		e.Usage.Record(sessionID, requestID, selected.Name, actual,
			int(toInt64(usage["prompt_tokens"])),
			int(toInt64(usage["completion_tokens"])),
			int(toInt64(usage["total_tokens"])), 0)
	}
	w.JSON(200, resp)
}

func localName(model string) string {
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// ---- 用量统计 ----

func (e *Engine) handleUsageStats(req *httpRequest, w *responseWriter) {
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": map[string]any{"message": "网关鉴权失败", "type": "authentication_error", "code": 401}})
		return
	}
	q := req.Query
	timeRange := strings.TrimSpace(q["time_range"])
	limitRaw := strings.TrimSpace(q["limit"])

	if limitRaw != "" {
		n, err := parseLimit(limitRaw)
		if err != nil {
			w.JSON(400, map[string]any{"error": map[string]any{"message": err.Error(), "type": "invalid_request_error", "code": 400}})
			return
		}
		items, _ := e.Usage.QueryRecentConversations(n)
		w.JSON(200, map[string]any{"mode": "conversation", "data": items})
		return
	}
	if timeRange != "" {
		key := strings.ToLower(timeRange)
		if !contains(timeRangeOptions, key) {
			w.JSON(400, map[string]any{"error": map[string]any{"message": "无效的 time_range: " + timeRange, "type": "invalid_request_error", "code": 400}})
			return
		}
		var startTS, endTS *float64
		if v, ok := q["start_ts"]; ok && v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				startTS = &f
			}
		}
		if v, ok := q["end_ts"]; ok && v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				endTS = &f
			}
		}
		if key == "custom" && (startTS == nil || endTS == nil) {
			w.JSON(400, map[string]any{"error": map[string]any{"message": "custom 模式必须同时提供 start_ts 与 end_ts", "type": "invalid_request_error", "code": 400}})
			return
		}
		items, err := e.Usage.QueryTimeSeries(key, startTS, endTS)
		if err != nil {
			w.JSON(400, map[string]any{"error": map[string]any{"message": err.Error(), "type": "invalid_request_error", "code": 400}})
			return
		}
		w.JSON(200, map[string]any{"mode": "time", "data": items})
		return
	}
	w.JSON(400, map[string]any{"error": map[string]any{"message": "缺少查询参数：time_range 或 limit 至少提供一个", "type": "invalid_request_error", "code": 400}})
}

var timeRangeOptions = []string{"10min", "0.5h", "1h", "2h", "5h", "10h", "1d", "7d", "30d", "0.5y", "total", "custom"}

func parseLimit(raw string) (int, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "total" {
		return -1, nil
	}
	if isDigits(v) {
		n := atoi(v)
		if n >= 1 && n <= 100000 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("无效的 limit: %s", raw)
}

// ---- Ollama 兼容端点 ----

func (e *Engine) handleOllamaTags(req *httpRequest, w *responseWriter) {
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": "unauthorized"})
		return
	}
	w.JSON(200, e.Ollama.listModels())
}

type ollamaChatBody struct {
	Model    string           `json:"model"`
	Messages []map[string]any `json:"messages"`
	Stream   bool             `json:"stream"`
	Options  map[string]any   `json:"options"`
}

func (e *Engine) handleOllamaChat(req *httpRequest, w *responseWriter) {
	if req.Method != "POST" {
		w.JSON(405, map[string]any{"error": "method not allowed"})
		return
	}
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": "unauthorized"})
		return
	}
	var body ollamaChatBody
	if err := jsonBody(req, &body); err != nil {
		w.JSON(400, map[string]any{"error": "bad request"})
		return
	}
	keySelector := req.Headers["x-gateway-api-key"]
	if body.Stream {
		w.SSE()
		err := e.Ollama.streamChatOllama(body.Model, body.Messages, body.Options, keySelector,
			func(block map[string]any) error {
				w.SSEEvent(mustJSON(block))
				return nil
			})
		_ = err
		w.Close()
		return
	}
	resp, err := e.Ollama.chatOllama(body.Model, body.Messages, body.Options, keySelector)
	if err != nil {
		w.JSON(500, map[string]any{"error": err.Error()})
		return
	}
	w.JSON(200, resp)
}

type ollamaGenerateBody struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options"`
}

func (e *Engine) handleOllamaGenerate(req *httpRequest, w *responseWriter) {
	if req.Method != "POST" {
		w.JSON(405, map[string]any{"error": "method not allowed"})
		return
	}
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": "unauthorized"})
		return
	}
	var body ollamaGenerateBody
	if err := jsonBody(req, &body); err != nil {
		w.JSON(400, map[string]any{"error": "bad request"})
		return
	}
	keySelector := req.Headers["x-gateway-api-key"]
	messages := []map[string]any{{"role": "user", "content": body.Prompt}}
	if body.Stream {
		w.SSE()
		err := e.Ollama.streamChatOllama(body.Model, messages, body.Options, keySelector,
			func(block map[string]any) error {
				if msg, ok := block["message"].(map[string]any); ok {
					w.SSEEvent(mustJSON(map[string]any{"response": msg["content"], "done": block["done"]}))
				} else {
					w.SSEEvent(mustJSON(map[string]any{"done": true}))
				}
				return nil
			})
		_ = err
		w.Close()
		return
	}
	resp, err := e.Ollama.chatOllama(body.Model, messages, body.Options, keySelector)
	if err != nil {
		w.JSON(500, map[string]any{"error": err.Error()})
		return
	}
	content := ""
	if msg, ok := resp["message"].(map[string]any); ok {
		content = toStr(msg["content"])
	}
	w.JSON(200, map[string]any{"response": content, "done": true, "model": body.Model})
}

// ---- GGUF 信息 ----

func (e *Engine) handleGGUFInfo(req *httpRequest, w *responseWriter) {
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": "unauthorized"})
		return
	}
	name := strings.TrimPrefix(req.Path, "/api/gguf/")
	name = strings.TrimSuffix(name, "/")
	reader := e.GGUF.get(name)
	if reader == nil {
		w.JSON(404, map[string]any{"error": "模型不存在: " + name})
		return
	}
	st, _ := os.Stat(reader.Path)
	size := int64(0)
	if st != nil {
		size = st.Size()
	}
	w.JSON(200, reader.toDict(size))
}

// ---- 工具 ----

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case float32:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case int32:
		return int64(t)
	case uint32:
		return int64(t)
	case uint64:
		return int64(t)
	case uint16:
		return int64(t)
	case int16:
		return int64(t)
	case uint8:
		return int64(t)
	case int8:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	}
	return 0
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

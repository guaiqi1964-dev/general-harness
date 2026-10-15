// chat 侧联网检索集成测试（P2）：验证注入、sources 回传、流式 phase 帧、降级与隐私边界。
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// chatSearchHarness 记录假厂商收到的消息与假搜索后端收到的检索词。
type chatSearchHarness struct {
	addr     string
	stop     func()
	mu       sync.Mutex
	queries  []string
	messages [][]map[string]any
}

func (h *chatSearchHarness) recordQuery(q string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queries = append(h.queries, q)
}

func (h *chatSearchHarness) recordMessages(msgs []map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msgs)
}

func (h *chatSearchHarness) queriesSeen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.queries...)
}

func (h *chatSearchHarness) calls() [][]map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][]map[string]any{}, h.messages...)
}

// startChatSearchServer 启动引擎（假厂商 + 可选假搜索后端）；sc.Enabled / sc.Backend 由调用方设置，
// useFake 为 true 时把名为 fake 的后端指向本测试的假 SearXNG。
func startChatSearchServer(t *testing.T, sc *SearchConfig, useFake bool) *chatSearchHarness {
	t.Helper()
	h := &chatSearchHarness{}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			w.WriteHeader(404)
			return
		}
		h.recordQuery(r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		base := "http://" + r.Host
		fmt.Fprintf(w, `{"results":[
			{"title":"DeepSeek 发布新模型","url":"https://example.com/news/a?utm_source=x&id=1#frag","content":"DeepSeek 今天发布了新模型。","engine":"test"},
			{"title":"本地页面","url":"%s/page1","content":"本地摘要。","engine":"test"}
		]}`, base)
	}))
	t.Cleanup(upstream.Close)

	if sc != nil && useFake {
		sc.Backends = map[string]*SearchBackend{"fake": {
			Name: "fake", Type: "http_json", BaseURL: upstream.URL, Path: "/search", Method: "GET",
			QueryParam: "q", Params: map[string]string{"format": "json"}, ResultPath: "results",
			FieldMap: map[string]string{"title": "title", "url": "url", "snippet": "content", "source": "engine"},
		}}
	}

	dir := tempDir(t)
	_ = os.MkdirAll(filepath.Join(dir, "plugins", "fake"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "plugins", "fake", "config.yaml"),
		[]byte("plugin: openai_compatible\nbase_url: https://fake.local/v1\napi_key: sk-test\nmodels:\n  - fake-model\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	cfg := &GlobalConfig{Host: "127.0.0.1", Port: 0, RateLimitPerMinute: 0, DefaultModel: "fake/fake-model", Search: sc}
	e := newEngine(cfg, dir)

	fake := &Provider{Name: "fake", BaseURL: "http://fake.local", Keys: []APIKey{{Name: "default", Key: "sk-test"}}}
	fake.Models = []string{"fake-model"}
	fake.ChatHandler = func(p *Provider, model string, messages []map[string]any,
		keySelector string, temperature *float64, maxTokens *int, topP *float64,
		streamOptions map[string]any) (map[string]any, error) {
		h.recordMessages(messages)
		content := "回答正文"
		// 改写调用：system 提示词即改写指令，返回模型给出的检索词。
		if len(messages) > 0 {
			if sys, _ := messages[0]["content"].(string); sys == webSearchRewritePrompt {
				content = "deepseek 新模型 2026"
			}
		}
		return map[string]any{
			"id": "mock", "content": content, "model": model,
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		}, nil
	}
	fake.StreamHandler = func(p *Provider, model string, messages []map[string]any,
		keySelector string, temperature *float64, maxTokens *int, topP *float64,
		streamOptions map[string]any, onChunk func(map[string]any) error) error {
		h.recordMessages(messages)
		if err := onChunk(map[string]any{"id": "s1", "content": "回答", "usage": nil}); err != nil {
			return err
		}
		return onChunk(map[string]any{"id": "s1", "content": "正文", "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3}})
	}
	e.Cloud.Providers["fake"] = fake
	e.Cloud.ModelRoutes["fake-model"] = &modelRoute{Provider: fake, Model: "fake-model"}
	e.Cloud.ModelRoutes["fake/fake-model"] = &modelRoute{Provider: fake, Model: "fake-model"}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go serve(ln, e.dispatch)
	h.addr = ln.Addr().String()
	h.stop = func() { _ = ln.Close() }
	return h
}

// postChatRaw 发一次 chat 请求，返回状态码与响应体原文（流式时为原始 SSE 文本）。
func postChatRaw(t *testing.T, addr, body string) (int, string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := fmt.Sprintf("POST /v1/chat/completions HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n"+
		"Content-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", addr, len(body), body)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	raw := string(buf)
	status := 0
	if i := strings.Index(raw, "\r\n"); i > 0 {
		_, _ = fmt.Sscanf(raw[:i], "HTTP/1.1 %d", &status)
	}
	payload := raw
	if i := strings.Index(raw, "\r\n\r\n"); i >= 0 {
		payload = raw[i+4:]
	}
	return status, payload
}

func chatBody(extra string) string {
	base := `{"model":"fake/fake-model","messages":[{"role":"user","content":"请帮我查一下今天 DeepSeek 发布了什么新模型？"}]`
	if extra == "" {
		return base + `}`
	}
	return base + `,` + extra + `}`
}

// sseDataFrames 抽取 SSE 的 data 帧原文（不含 "data: " 前缀）。
func sseDataFrames(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data: ") {
			out = append(out, strings.TrimPrefix(line, "data: "))
		}
	}
	return out
}

// enabledSearchConfig 返回启用检索、后端名为 fake 的配置。
func enabledSearchConfig() *SearchConfig {
	sc := defaultSearchConfig()
	sc.Enabled = true
	sc.Backend = "fake"
	return sc
}

// 根路径返回服务标识与版本，且不再宣称不存在的 /docs（v0.3.0 修正）。
func TestRootEndpointAdvertisesVersionOnly(t *testing.T) {
	h := startChatSearchServer(t, defaultSearchConfig(), true)
	defer h.stop()

	status, body := httpGet(t, h.addr, "/")
	if status != 200 {
		t.Fatalf("GET / = %d", status)
	}
	if body["service"] != "General Harness" {
		t.Errorf("service = %v", body["service"])
	}
	if body["version"] != engineVersion {
		t.Errorf("version = %v，期望 %s", body["version"], engineVersion)
	}
	if _, ok := body["docs"]; ok {
		t.Errorf("不应再返回 docs 字段（引擎没有 /docs 路由）：%v", body)
	}
}

// 未带 web_search 时：不触发检索，响应不出现 sources（G2 零影响）。
func TestChatWithoutWebSearchUnchanged(t *testing.T) {
	h := startChatSearchServer(t, enabledSearchConfig(), true)
	defer h.stop()

	status, payload := postChatRaw(t, h.addr, chatBody(""))
	if status != 200 {
		t.Fatalf("状态 = %d，期望 200（%s）", status, payload)
	}
	if strings.Contains(payload, "sources") {
		t.Errorf("未请求检索时不应出现 sources：%s", payload)
	}
	if q := h.queriesSeen(); len(q) != 0 {
		t.Errorf("未请求检索时不应访问搜索后端，实际检索词 %v", q)
	}
	calls := h.calls()
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0]["role"] != "user" {
		t.Errorf("消息被意外改动：%v", calls)
	}
}

// web_search=true：注入 system 上下文并回传 sources；发给搜索服务的是关键词而非问题原文（Q7）。
func TestChatWebSearchInjectsContextAndSources(t *testing.T) {
	h := startChatSearchServer(t, enabledSearchConfig(), true)
	defer h.stop()

	status, payload := postChatRaw(t, h.addr, chatBody(`"web_search":true`))
	if status != 200 {
		t.Fatalf("状态 = %d，期望 200（%s）", status, payload)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, payload)
	}
	sources, ok := body["sources"].([]any)
	if !ok || len(sources) == 0 {
		t.Fatalf("sources 缺失或为空：%v", body["sources"])
	}
	first, _ := sources[0].(map[string]any)
	if first["index"] != float64(1) {
		t.Errorf("首个来源序号 = %v，期望 1", first["index"])
	}
	url, _ := first["url"].(string)
	if url == "" || strings.Contains(url, "utm_source") || strings.Contains(url, "#") {
		t.Errorf("来源 URL 未归一化：%q", url)
	}

	calls := h.calls()
	if len(calls) != 1 {
		t.Fatalf("厂商调用次数 = %d，期望 1", len(calls))
	}
	msgs := calls[0]
	if len(msgs) != 2 {
		t.Fatalf("注入后消息数 = %d，期望 2：%v", len(msgs), msgs)
	}
	if msgs[0]["role"] != "system" || msgs[1]["role"] != "user" {
		t.Fatalf("注入位置错误：%v", msgs)
	}
	sys, _ := msgs[0]["content"].(string)
	for _, want := range []string{"不可信", "[1]", "URL:", url} {
		if !strings.Contains(sys, want) {
			t.Errorf("注入上下文缺少 %q：%s", want, sys)
		}
	}
	if user, _ := msgs[1]["content"].(string); !strings.Contains(user, "请帮我查一下") {
		t.Errorf("用户消息被改动：%q", user)
	}

	queries := h.queriesSeen()
	if len(queries) != 1 {
		t.Fatalf("检索次数 = %d，期望 1：%v", len(queries), queries)
	}
	if strings.Contains(queries[0], "请帮我") || strings.Contains(queries[0], "？") {
		t.Errorf("检索词仍是问题原文（违反 Q7 隐私红线）：%q", queries[0])
	}
	if !strings.Contains(strings.ToLower(queries[0]), "deepseek") {
		t.Errorf("检索词丢失关键实体：%q", queries[0])
	}
}

// 显式 query 直接使用，不做关键词提取。
func TestChatWebSearchExplicitQuery(t *testing.T) {
	h := startChatSearchServer(t, enabledSearchConfig(), true)
	defer h.stop()

	status, payload := postChatRaw(t, h.addr, chatBody(`"web_search":{"enabled":true,"query":"explicit search terms"}`))
	if status != 200 {
		t.Fatalf("状态 = %d，期望 200（%s）", status, payload)
	}
	queries := h.queriesSeen()
	if len(queries) != 1 || queries[0] != "explicit search terms" {
		t.Errorf("检索词 = %v，期望 [explicit search terms]", queries)
	}
}

// query_mode=rewrite：改写在自有厂商完成，搜索服务只收到改写后的词。
func TestChatWebSearchRewriteMode(t *testing.T) {
	sc := enabledSearchConfig()
	sc.QueryMode = "rewrite"
	h := startChatSearchServer(t, sc, true)
	defer h.stop()

	status, payload := postChatRaw(t, h.addr, chatBody(`"web_search":true`))
	if status != 200 {
		t.Fatalf("状态 = %d，期望 200（%s）", status, payload)
	}
	queries := h.queriesSeen()
	if len(queries) != 1 || queries[0] != "deepseek 新模型 2026" {
		t.Errorf("检索词 = %v，期望 [deepseek 新模型 2026]", queries)
	}
	if calls := h.calls(); len(calls) != 2 {
		t.Errorf("厂商调用次数 = %d，期望 2（改写 + 回答）", len(calls))
	}
}

// 流式：phase 帧先于正文块，未请求检索时没有 phase 帧。
func TestChatWebSearchStreamPhaseFrame(t *testing.T) {
	h := startChatSearchServer(t, enabledSearchConfig(), true)
	defer h.stop()

	status, raw := postChatRaw(t, h.addr, chatBody(`"stream":true,"web_search":true`))
	if status != 200 {
		t.Fatalf("状态 = %d，期望 200（%s）", status, raw)
	}
	frames := sseDataFrames(raw)
	if len(frames) < 3 {
		t.Fatalf("SSE 帧过少：%v", frames)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(frames[0]), &first); err != nil {
		t.Fatalf("首个帧不是 JSON：%v（%q）", err, frames[0])
	}
	if first["phase"] != "search" {
		t.Errorf("首个帧 phase = %v，期望 search（%q）", first["phase"], frames[0])
	}
	if src, ok := first["sources"].([]any); !ok || len(src) == 0 {
		t.Errorf("phase 帧缺少 sources：%q", frames[0])
	}
	if _, ok := first["finish_reason"]; !ok {
		t.Errorf("phase 帧应显式带 finish_reason：%q", frames[0])
	}
	if frames[len(frames)-1] != "[DONE]" {
		t.Errorf("末帧 = %q，期望 [DONE]", frames[len(frames)-1])
	}
	for _, f := range frames[1 : len(frames)-1] {
		var chunk map[string]any
		if err := json.Unmarshal([]byte(f), &chunk); err != nil {
			t.Fatalf("正文帧不是 JSON：%v（%q）", err, f)
		}
		if chunk["phase"] != nil {
			t.Errorf("正文帧不应含 phase：%q", f)
		}
	}

	status2, raw2 := postChatRaw(t, h.addr, chatBody(`"stream":true`))
	if status2 != 200 {
		t.Fatalf("未请求检索的流式状态 = %d", status2)
	}
	for _, f := range sseDataFrames(raw2) {
		if strings.Contains(f, `"phase"`) {
			t.Errorf("未请求检索时应无 phase 帧：%q", f)
		}
	}
}

// 检索失败（后端不可达）：降级为无来源回答，请求仍成功。
func TestChatWebSearchDegradesOnBackendFailure(t *testing.T) {
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	_ = dead.Close()

	sc := enabledSearchConfig()
	sc.TimeoutSeconds = 3
	sc.Backend = "dead"
	sc.Backends = map[string]*SearchBackend{"dead": {
		Name: "dead", Type: "http_json", BaseURL: "http://" + deadAddr, Path: "/search", Method: "GET",
		QueryParam: "q", ResultPath: "results",
		FieldMap: map[string]string{"title": "title", "url": "url", "snippet": "content"},
	}}
	h := startChatSearchServer(t, sc, false)
	defer h.stop()

	status, payload := postChatRaw(t, h.addr, chatBody(`"web_search":true`))
	if status != 200 {
		t.Fatalf("检索失败不应影响补全：状态 = %d（%s）", status, payload)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if body["content"] != "回答正文" {
		t.Errorf("content = %v，期望模型回答仍返回", body["content"])
	}
	src, ok := body["sources"].([]any)
	if !ok || len(src) != 0 {
		t.Errorf("降级时 sources 应为空数组，实际 %v", body["sources"])
	}
}

// 全局关闭 search 但请求要求检索：403 permission_error。
func TestChatWebSearchDisabledReturns403(t *testing.T) {
	off := defaultSearchConfig() // Enabled = false
	off.Backend = "fake"
	h := startChatSearchServer(t, off, true)
	defer h.stop()

	status, payload := postChatRaw(t, h.addr, chatBody(`"web_search":true`))
	if status != 403 {
		t.Fatalf("状态 = %d，期望 403（%s）", status, payload)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["type"] != "permission_error" {
		t.Errorf("错误类型 = %v，期望 permission_error", errObj["type"])
	}
	if len(h.calls()) != 0 {
		t.Errorf("未启用时不应调用厂商：%v", h.calls())
	}
}

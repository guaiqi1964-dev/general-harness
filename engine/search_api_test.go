// /v1/search 与 /v1/search/backends 的 HTTP 集成测试（真实 TCP 监听 + 手写请求）。
package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startSearchTestServer 启动带 search 配置的临时引擎，返回地址与清理函数。
func startSearchTestServer(t *testing.T, cfg *GlobalConfig) (string, func()) {
	t.Helper()
	dir := tempDir(t)
	_ = os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	e := newEngine(cfg, dir)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go serve(ln, e.dispatch)
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func searchTestGlobalConfig(sc *SearchConfig) *GlobalConfig {
	return &GlobalConfig{Host: "127.0.0.1", Port: 0, RateLimitPerMinute: 0, Search: sc}
}

func httpSend(t *testing.T, addr, method, path, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n", method, path, addr)
	for k, v := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	if body != "" {
		fmt.Fprintf(&b, "Content-Type: application/json\r\nContent-Length: %d\r\n", len(body))
	}
	b.WriteString("\r\n")
	b.WriteString(body)
	if _, err := conn.Write([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	resp := readRawResponse(t, conn)
	return resp.status, resp.bodyMap()
}

// 关闭搜索时不应注册路由：请求落到 404（G2：未启用对既有行为零影响）。
func TestSearchEndpointsDisabledReturns404(t *testing.T) {
	addr, stop := startSearchTestServer(t, searchTestGlobalConfig(defaultSearchConfig()))
	defer stop()

	if status, _ := httpGet(t, addr, "/v1/search/backends"); status != 404 {
		t.Errorf("GET /v1/search/backends 关闭时应为 404，得到 %d", status)
	}
	if status, _ := httpSend(t, addr, "POST", "/v1/search", `{"query":"x"}`, nil); status != 404 {
		t.Errorf("POST /v1/search 关闭时应为 404，得到 %d", status)
	}
	// search 段完全缺失时同样安全
	addr2, stop2 := startSearchTestServer(t, searchTestGlobalConfig(nil))
	defer stop2()
	if status, _ := httpGet(t, addr2, "/v1/search/backends"); status != 404 {
		t.Errorf("缺少 search 段时应为 404，得到 %d", status)
	}
}

func TestSearchEndpointsEnabled(t *testing.T) {
	srv := fakeSearchUpstream(t)
	cfg := defaultSearchConfig()
	cfg.Enabled = true
	cfg.Backend = "fake"
	cfg.MaxResults = 3
	cfg.TimeoutSeconds = 5
	cfg.Fetch = FetchConfig{MaxPages: 2, MaxCharsPerPage: 500, TimeoutSeconds: 5, UserAgent: "Test/1.0", AllowPrivate: true}
	cfg.Backends = map[string]*SearchBackend{"fake": fakeSearchBackend(srv)}
	addr, stop := startSearchTestServer(t, searchTestGlobalConfig(cfg))
	defer stop()

	status, body := httpGet(t, addr, "/v1/search/backends")
	if status != 200 {
		t.Fatalf("GET /v1/search/backends = %d，期望 200（%v）", status, body)
	}
	if body["enabled"] != true || body["query_mode"] != "keywords" || body["fetch_content"] != false {
		t.Errorf("后端列表元信息异常：%v", body)
	}
	data, _ := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("应列出 1 个后端，得到 %v", body["data"])
	}
	info, _ := data[0].(map[string]any)
	if info["name"] != "fake" || info["ready"] != true || info["type"] != "http_json" {
		t.Errorf("后端条目异常：%v", info)
	}

	status, body = httpSend(t, addr, "POST", "/v1/search", `{"query":"DeepSeek 模型","max_results":3}`, nil)
	if status != 200 {
		t.Fatalf("POST /v1/search = %d，期望 200（%v）", status, body)
	}
	if body["backend"] != "fake" || body["cached"] != false {
		t.Errorf("检索响应元信息异常：%v", body)
	}
	results, _ := body["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("应返回 3 条结果，得到 %v", body["results"])
	}
	first, _ := results[0].(map[string]any)
	if first["index"] != float64(1) || first["url"] == "" || first["title"] == "" {
		t.Errorf("结果条目异常：%v", first)
	}
	if strings.Contains(fmt.Sprint(first["url"]), "utm_") {
		t.Errorf("返回的 URL 未去跟踪参数：%v", first["url"])
	}

	// 空 query → 400
	if status, _ := httpSend(t, addr, "POST", "/v1/search", `{"query":"  "}`, nil); status != 400 {
		t.Errorf("空 query 应为 400，得到 %d", status)
	}
	// 未知后端 → 400
	if status, _ := httpSend(t, addr, "POST", "/v1/search", `{"query":"x","backend":"nope"}`, nil); status != 400 {
		t.Errorf("未知后端应为 400，得到 %d", status)
	}
	// 方法不匹配 → 405
	if status, _ := httpGet(t, addr, "/v1/search"); status != 405 {
		t.Errorf("GET /v1/search 应为 405，得到 %d", status)
	}
	if status, _ := httpSend(t, addr, "POST", "/v1/search/backends", `{}`, nil); status != 405 {
		t.Errorf("POST /v1/search/backends 应为 405，得到 %d", status)
	}
	// 非法 JSON → 400
	if status, _ := httpSend(t, addr, "POST", "/v1/search", `{`, nil); status != 400 {
		t.Errorf("非法 JSON 应为 400，得到 %d", status)
	}
}

func TestSearchEndpointsRequireAuth(t *testing.T) {
	srv := fakeSearchUpstream(t)
	cfg := defaultSearchConfig()
	cfg.Enabled = true
	cfg.Backend = "fake"
	cfg.Backends = map[string]*SearchBackend{"fake": fakeSearchBackend(srv)}
	gcfg := searchTestGlobalConfig(cfg)
	gcfg.GatewayAPIKey = "test-key-123"
	addr, stop := startSearchTestServer(t, gcfg)
	defer stop()

	if status, _ := httpSend(t, addr, "POST", "/v1/search", `{"query":"x"}`, nil); status != 401 {
		t.Errorf("缺少凭据应为 401，得到 %d", status)
	}
	bad := map[string]string{"Authorization": "Bearer wrong"}
	if status, _ := httpSend(t, addr, "POST", "/v1/search", `{"query":"x"}`, bad); status != 401 {
		t.Errorf("错误凭据应为 401，得到 %d", status)
	}
	good := map[string]string{"Authorization": "Bearer test-key-123"}
	if status, _ := httpSend(t, addr, "POST", "/v1/search", `{"query":"DeepSeek 模型"}`, good); status != 200 {
		t.Errorf("正确凭据应为 200，得到 %d", status)
	}
	if status, _ := httpGet(t, addr, "/v1/search/backends"); status != 401 {
		t.Errorf("后端列表同样需要凭据，得到 %d", status)
	}
}

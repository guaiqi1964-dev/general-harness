// 引擎 HTTP 集成测试（真实 TCP 监听 + 手写 HTTP 客户端）。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startTestServer 启动临时引擎服务，返回地址与清理函数。
func startTestServer(t *testing.T) (string, func()) {
	t.Helper()
	dir := tempDir(t)
	_ = os.MkdirAll(filepath.Join(dir, "plugins", "fake"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "plugins", "fake", "config.yaml"),
		[]byte("plugin: openai_compatible\nbase_url: https://fake.local/v1\napi_key: sk-test\nmodels:\n  - fake-model\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	cfg := &GlobalConfig{Host: "127.0.0.1", Port: 0, RateLimitPerMinute: 0, DefaultModel: "fake/fake-model"}
	e := newEngine(cfg, dir)
	// 覆盖云端代理：直接注入假厂商
	fake := &Provider{Name: "fake", BaseURL: "http://fake.local", Keys: []APIKey{{Name: "default", Key: "sk-test"}}}
	fake.Models = []string{"fake-model"}
	fake.ChatHandler = func(p *Provider, model string, messages []map[string]any,
		keySelector string, temperature *float64, maxTokens *int) (map[string]any, error) {
		return map[string]any{
			"id": "mock", "content": "pong", "model": model,
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		}, nil
	}
	fake.StreamHandler = func(p *Provider, model string, messages []map[string]any,
		keySelector string, temperature *float64, maxTokens *int,
		onChunk func(map[string]any) error) error {
		// 模拟真实 DeepSeek：首个块 usage:null（经 parseUsage 变为类型化 nil map）
		if err := onChunk(map[string]any{"id": "s1", "content": "你", "usage": parseUsage(map[string]any{"usage": nil})}); err != nil {
			return err
		}
		if err := onChunk(map[string]any{"id": "s1", "content": "好", "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3}}); err != nil {
			return err
		}
		return nil
	}
	e.Cloud.Providers["fake"] = fake
	e.Cloud.ModelRoutes["fake-model"] = &modelRoute{Provider: fake, Model: "fake-model"}
	e.Cloud.ModelRoutes["fake/fake-model"] = &modelRoute{Provider: fake, Model: "fake-model"}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go serve(ln, e.dispatch)
	return ln.Addr().String(), func() { _ = ln.Close() }
}

var _ = json.Marshal
var _ = strings.Contains
func httpGet(t *testing.T, addr, path string) (int, map[string]any) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", path, addr)
	resp := readRawResponse(t, conn)
	return resp.status, resp.bodyMap()
}

type rawResp struct {
	status int
	body   []byte
}

func (r *rawResp) bodyMap() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.body, &m)
	return m
}

func readRawResponse(t *testing.T, conn net.Conn) *rawResp {
	t.Helper()
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(line), " ")
	status := 0
	if len(parts) >= 2 {
		fmt.Sscanf(parts[1], "%d", &status)
	}
	headers := map[string]string{}
	for {
		hl, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		hl = strings.TrimSpace(hl)
		if hl == "" {
			break
		}
		if idx := strings.Index(hl, ":"); idx > 0 {
			headers[strings.ToLower(hl[:idx])] = strings.TrimSpace(hl[idx+1:])
		}
	}
	body := []byte{}
	if cl, ok := headers["content-length"]; ok {
		n := 0
		fmt.Sscanf(cl, "%d", &n)
		if n > 0 && n < 10*1024*1024 {
			body = make([]byte, n)
			readFull(t, reader, body)
		}
	}
	return &rawResp{status: status, body: body}
}

func readFull(t *testing.T, r *bufio.Reader, buf []byte) {
	t.Helper()
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()
	status, body := httpGet(t, addr, "/health")
	if status != 200 || body["status"] != "ok" {
		t.Fatalf("health = %d %v", status, body)
	}
}

func TestModelsEndpoint(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()
	status, body := httpGet(t, addr, "/v1/models")
	if status != 200 {
		t.Fatalf("models = %d", status)
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("models data = %v", body["data"])
	}
}

func TestChatCompletionsRecording(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"fake/fake-model","messages":[{"role":"user","content":"hi"}],"session_id":"go-test-sess"}`
	fmt.Fprintf(conn, "POST /v1/chat/completions HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		addr, len(body), body)
	resp := readRawResponse(t, conn)
	conn.Close()
	if resp.status != 200 {
		t.Fatalf("chat = %d %s", resp.status, resp.body)
	}
	m := resp.bodyMap()
	if m["content"] != "pong" {
		t.Errorf("content = %v", m["content"])
	}
}

func TestUsageStatsEndpoint(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()
	// 先写一条记录
	dir := tempDir(t)
	_ = dir
	status, body := httpGet(t, addr, "/v1/usage/stats?limit=10")
	if status != 200 {
		t.Fatalf("stats = %d", status)
	}
	if body["mode"] != "conversation" {
		t.Errorf("mode = %v", body["mode"])
	}
	status, _ = httpGet(t, addr, "/v1/usage/stats?time_range=1h")
	if status != 200 {
		t.Fatalf("time stats = %d", status)
	}
	status, _ = httpGet(t, addr, "/v1/usage/stats?time_range=bogus")
	if status != 400 {
		t.Fatalf("bogus = %d", status)
	}
	status, _ = httpGet(t, addr, "/v1/usage/stats")
	if status != 400 {
		t.Fatalf("no params = %d", status)
	}
}

func TestOllamaTagsEndpoint(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()
	status, body := httpGet(t, addr, "/api/tags")
	if status != 200 {
		t.Fatalf("tags = %d", status)
	}
	if _, ok := body["models"].([]any); !ok {
		t.Errorf("tags models = %v", body)
	}
}

func TestOllamaChatEndpoint(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"fake/fake-model","messages":[{"role":"user","content":"hi"}]}`
	fmt.Fprintf(conn, "POST /api/chat HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		addr, len(body), body)
	resp := readRawResponse(t, conn)
	conn.Close()
	if resp.status != 200 {
		t.Fatalf("ollama chat = %d %s", resp.status, resp.body)
	}
	m := resp.bodyMap()
	if m["done"] != true {
		t.Errorf("done = %v", m["done"])
	}
	if msg, ok := m["message"].(map[string]any); !ok || msg["content"] != "pong" {
		t.Errorf("message = %v", m["message"])
	}
}

func TestAuthRequired(t *testing.T) {
	dir := tempDir(t)
	_ = os.MkdirAll(filepath.Join(dir, "plugins", "fake"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "plugins", "fake", "config.yaml"),
		[]byte("plugin: openai_compatible\nbase_url: https://fake.local/v1\napi_key: sk-test\nmodels:\n  - fake-model\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	cfg := &GlobalConfig{Host: "127.0.0.1", Port: 0, RateLimitPerMinute: 0, DefaultModel: "fake/fake-model", GatewayAPIKey: "secret"}
	e := newEngine(cfg, dir)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go serve(ln, e.dispatch)
	defer ln.Close()

	conn, _ := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	defer conn.Close()
	fmt.Fprintf(conn, "GET /v1/models HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	resp := readRawResponse(t, conn)
	if resp.status != 401 {
		t.Fatalf("no auth = %d, want 401", resp.status)
	}
}

func TestRateLimit(t *testing.T) {
	dir := tempDir(t)
	_ = os.MkdirAll(filepath.Join(dir, "plugins", "fake"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "plugins", "fake", "config.yaml"),
		[]byte("plugin: openai_compatible\nbase_url: https://fake.local/v1\napi_key: sk-test\nmodels:\n  - fake-model\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	cfg := &GlobalConfig{Host: "127.0.0.1", Port: 0, RateLimitPerMinute: 2, DefaultModel: "fake/fake-model"}
	e := newEngine(cfg, dir)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go serve(ln, e.dispatch)
	defer ln.Close()
	var last int
	for i := 0; i < 4; i++ {
		status, _ := httpGet(t, ln.Addr().String(), "/v1/usage/stats?limit=10")
		last = status
	}
	if last != 429 {
		t.Fatalf("last = %d, want 429", last)
	}
}

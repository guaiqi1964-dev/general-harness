// 引擎单元测试。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "gh-test-"+randHex(4))
	_ = os.MkdirAll(dir, 0o755)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// 构造带临时厂商配置的测试引擎。
func testEngine(t *testing.T) *Engine {
	t.Helper()
	dir := tempDir(t)
	pluginsDir := filepath.Join(dir, "plugins")
	_ = os.MkdirAll(pluginsDir, 0o755)
	_ = os.WriteFile(filepath.Join(pluginsDir, "fake", "config.yaml"), nil, 0o644)
	_ = os.MkdirAll(filepath.Join(pluginsDir, "fake"), 0o755)
	_ = os.WriteFile(filepath.Join(pluginsDir, "fake", "config.yaml"),
		[]byte("plugin: openai_compatible\nbase_url: https://fake.local/v1\napi_key: sk-test\nmodels:\n  - fake-model\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	cfg := &GlobalConfig{Host: "127.0.0.1", Port: 8000, RateLimitPerMinute: 0, DefaultModel: "fake/fake-model"}
	e := newEngine(cfg, dir)
	return e
}

func TestYAMLParser(t *testing.T) {
	src := `server:
  host: 127.0.0.1
  port: 8000
rate_limit_per_minute: 60
aliases:
  fast: deepseek/deepseek-chat
default_model: deepseek/deepseek-chat
api_keys:
  - name: default
    key: ${DEEPSEEK_API_KEY}
`
	m, err := parseYAMLString(src)
	if err != nil {
		t.Fatal(err)
	}
	if srv, ok := m["server"].(map[string]any); ok {
		if yamlStr(srv, "host") != "127.0.0.1" {
			t.Errorf("host = %v", srv["host"])
		}
		if yamlInt(srv, "port", 0) != 8000 {
			t.Errorf("port = %v", srv["port"])
		}
	}
	if yamlInt(m, "rate_limit_per_minute", 0) != 60 {
		t.Error("rate_limit 解析失败")
	}
	if aliases, ok := m["aliases"].(map[string]any); ok {
		if yamlStr(aliases, "fast") != "deepseek/deepseek-chat" {
			t.Error("aliases 解析失败")
		}
	}
	if keys, ok := m["api_keys"].([]any); ok {
		if len(keys) != 1 {
			t.Errorf("api_keys len = %d", len(keys))
		}
	}
}

func TestGGUFParser(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "mini.gguf")
	// 构造最小 GGUF：magic + version + tensor_count + kv_count + 2 个 KV
	buf := []byte("GGUF")
	buf = appendU32(buf, 3)    // version
	buf = appendU64(buf, 0)    // tensor_count
	buf = appendU64(buf, 2)    // kv_count
	buf = appendGGUFString(buf, "general.architecture")
	buf = appendU32(buf, ggufString)
	buf = appendGGUFString(buf, "llama")
	buf = appendGGUFString(buf, "llama.context_length")
	buf = appendU32(buf, ggufUint32)
	buf = appendU32(buf, 4096)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	reader, err := openGGUF(path)
	if err != nil {
		t.Fatal(err)
	}
	if reader.architecture() != "llama" {
		t.Errorf("arch = %q", reader.architecture())
	}
	if reader.contextLength() != 4096 {
		t.Errorf("ctx = %d", reader.contextLength())
	}
	if reader.Version != 3 {
		t.Errorf("version = %d", reader.Version)
	}
}

func TestGGUFInvalidMagic(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "bad.gguf")
	_ = os.WriteFile(path, []byte("XXXX"), 0o644)
	if _, err := openGGUF(path); err == nil {
		t.Error("非法魔数应当报错")
	}
}

func TestUsageStore(t *testing.T) {
	dir := tempDir(t)
	store := newUsageStore(filepath.Join(dir, "usage.json"))
	now := float64(time.Now().Unix())
	store.Record("sess-1", "r1", "k", "m1", 10, 20, 100, now-100)
	store.Record("sess-1", "r2", "k", "m1", 10, 20, 50, now-50)
	store.Record("sess-2", "r3", "k", "m2", 0, 0, 30, now-10)

	convs, err := store.QueryRecentConversations(-1)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]int64{}
	for _, c := range convs {
		byID[c.SessionID] = c.Tokens
	}
	if byID["sess-1"] != 150 {
		t.Errorf("sess-1 = %d, want 150", byID["sess-1"])
	}
	if byID["sess-2"] != 30 {
		t.Errorf("sess-2 = %d, want 30", byID["sess-2"])
	}
	// 会话 1 最近活跃是 sess-2
	if convs[0].SessionID != "sess-2" {
		t.Errorf("top = %s, want sess-2", convs[0].SessionID)
	}

	items, err := store.QueryTimeSeries("1h", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	for _, i := range items {
		total += i.Tokens
	}
	if total != 180 {
		t.Errorf("1h total = %d, want 180", total)
	}

	// 持久化重载
	store2 := newUsageStore(store.path)
	convs2, _ := store2.QueryRecentConversations(-1)
	if len(convs2) != 2 {
		t.Errorf("reload convs = %d", len(convs2))
	}
}

func TestUsageTimeSeriesTotalAndCustom(t *testing.T) {
	dir := tempDir(t)
	store := newUsageStore(filepath.Join(dir, "u.json"))
	now := float64(time.Now().Unix())
	store.Record("s", "r1", "k", "m", 0, 0, 100, now-5000)
	store.Record("s", "r2", "k", "m", 0, 0, 200, now-100)
	items, _ := store.QueryTimeSeries("total", nil, nil)
	total := int64(0)
	for _, i := range items {
		total += i.Tokens
	}
	if total != 300 {
		t.Errorf("total = %d, want 300", total)
	}
	start, end := now-300, now
	items, _ = store.QueryTimeSeries("custom", &start, &end)
	total = 0
	for _, i := range items {
		total += i.Tokens
	}
	if total != 200 {
		t.Errorf("custom = %d, want 200", total)
	}
}

func TestProviderKeyResolution(t *testing.T) {
	p := &Provider{Name: "t", BaseURL: "http://x", Keys: []APIKey{
		{Name: "a", Key: "sk-a"}, {Name: "b", Key: "sk-b"},
	}}
	k, err := p.resolveKey("b")
	if err != nil || k.Name != "b" {
		t.Fatalf("by name: %+v %v", k, err)
	}
	k, _ = p.resolveKey("1")
	if k.Name != "b" {
		t.Fatalf("by index: %+v", k)
	}
	k, _ = p.resolveKey("")
	if k.Name != "a" {
		t.Fatalf("default: %+v", k)
	}
	if _, err := p.resolveKey("nope"); err == nil {
		t.Error("unknown should error")
	}
}

func TestCloudRegistry(t *testing.T) {
	e := testEngine(t)
	if len(e.Cloud.Providers) == 0 {
		t.Fatal("厂商未加载")
	}
	p, actual, err := e.Cloud.resolve("fake/fake-model", nil)
	if err != nil || actual != "fake-model" || p.Name != "fake" {
		t.Fatalf("resolve: %v %v %v", p, actual, err)
	}
	if _, _, err := e.Cloud.resolve("nope", nil); err == nil {
		t.Error("unknown model should error")
	}
}

func TestParseLimit(t *testing.T) {
	if n, _ := parseLimit("total"); n != -1 {
		t.Errorf("total = %d", n)
	}
	if n, _ := parseLimit("100"); n != 100 {
		t.Errorf("100 = %d", n)
	}
	if _, err := parseLimit("abc"); err == nil {
		t.Error("abc should error")
	}
}

func TestHTTPRequestParsing(t *testing.T) {
	req := &httpRequest{Path: "/v1/usage/stats"}
	req.Query = map[string]string{"time_range": "1d", "limit": "10"}
	if req.Query["time_range"] != "1d" {
		t.Error("query parse fail")
	}
}

// ---- JSON 工具 ----

func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendU64(b []byte, v uint64) []byte {
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

func appendGGUFString(b []byte, s string) []byte {
	b = appendU64(b, uint64(len(s)))
	return append(b, s...)
}

var _ = json.Marshal
var _ = strings.Contains

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeModelsURL(t *testing.T) {
	cases := []struct{ base, want string }{
		{"https://api.deepseek.com/v1", "https://api.deepseek.com/v1/models"},
		{"https://api.deepseek.com/v1/", "https://api.deepseek.com/v1/models"},
		{"https://api.deepseek.com", "https://api.deepseek.com/v1/models"},
		{"http://127.0.0.1:8888", "http://127.0.0.1:8888/v1/models"},
	}
	for _, c := range cases {
		if got := probeModelsURL(c.base); got != c.want {
			t.Fatalf("probeModelsURL(%q) = %q, want %q", c.base, got, c.want)
		}
	}
}

func TestGuessVision(t *testing.T) {
	yes := []string{"qwen-vl-plus", "gpt-4o", "gpt-4o-mini", "llava-1.5", "pixtral-12b", "some-vision-model", "gemini-omni"}
	no := []string{"deepseek-chat", "deepseek-flash", "moonshot-v1-8k", "qwen-turbo", "gpt-3.5-turbo"}
	for _, m := range yes {
		if !guessVision(m) {
			t.Fatalf("guessVision(%q) = false, want true", m)
		}
	}
	for _, m := range no {
		if guessVision(m) {
			t.Fatalf("guessVision(%q) = true, want false", m)
		}
	}
}

func TestStatusHint(t *testing.T) {
	if got := statusHint(401); got == "" {
		t.Fatal("401 应该有提示")
	}
	if got := statusHint(429); !strings.Contains(got, "限流") {
		t.Fatalf("429 提示 = %q", got)
	}
	if got := statusHint(0); got == "" {
		t.Fatal("0 应该有提示")
	}
}

func TestModelsCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cache := loadModelsCache(dir)
	if len(cache.Providers) != 0 {
		t.Fatal("空目录应返回空缓存")
	}
	cache.put("deepseek", "https://api.deepseek.com/v1", []discoveredModel{
		{ID: "deepseek-flash"},
		{ID: "deepseek-v4-pro"},
	})
	if err := cache.save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded := loadModelsCache(dir)
	p := reloaded.Providers["deepseek"]
	if p == nil {
		t.Fatal("重新加载后 deepseek 丢失")
	}
	if len(p.Models) != 2 || p.Models[0].ID != "deepseek-flash" {
		t.Fatalf("模型列表 = %+v", p.Models)
	}
	if p.ProbedAt == "" {
		t.Fatal("probed_at 应被记录")
	}
	if _, err := os.Stat(modelsCachePath(dir)); err != nil {
		t.Fatalf("缓存文件不存在: %v", err)
	}
}

func TestModelsCacheApplyToRegistry(t *testing.T) {
	reg := &CloudRegistry{
		Providers: map[string]*Provider{
			"deepseek": {Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", Models: []string{"deepseek-chat"}, Vision: map[string]bool{}},
			"kimi":     {Name: "kimi", BaseURL: "https://api.moonshot.cn/v1", Models: []string{"moonshot-v1-8k"}, Vision: map[string]bool{}},
		},
		ModelRoutes: map[string]*modelRoute{
			"deepseek-chat": {Provider: &Provider{Name: "deepseek"}, Model: "deepseek-chat"},
		},
	}
	cache := &modelsCache{Providers: map[string]*cachedProvider{}}
	cache.put("deepseek", "https://api.deepseek.com/v1", []discoveredModel{
		{ID: "deepseek-chat"},              // 已存在，不应重复
		{ID: "deepseek-flash"},             // 新增
		{ID: "qwen-vl-plus", Vision: true}, // 新增且标记视觉
	})
	// 缓存里出现配置中不存在的厂商：应被忽略
	cache.put("ghost", "https://ghost.example/v1", []discoveredModel{{ID: "ghost-1"}})

	added := cache.applyTo(reg)
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	ds := reg.Providers["deepseek"]
	if len(ds.Models) != 3 {
		t.Fatalf("deepseek 模型数 = %d, want 3 (%v)", len(ds.Models), ds.Models)
	}
	if !ds.Vision["qwen-vl-plus"] {
		t.Fatal("vision 标记未生效")
	}
	if _, ok := reg.ModelRoutes["deepseek-flash"]; !ok {
		t.Fatal("裸名路由未注册")
	}
	if _, ok := reg.ModelRoutes["deepseek/deepseek-flash"]; !ok {
		t.Fatal("厂商前缀路由未注册")
	}
	if _, ok := reg.Providers["ghost"]; ok {
		t.Fatal("不该凭空创建厂商")
	}

	// 再次合并应幂等
	if again := cache.applyTo(reg); again != 0 {
		t.Fatalf("重复合并 added = %d, want 0", again)
	}
}

// fakeProvider 起一个假厂商：path 命中的地址返回给定状态码与模型列表。
func fakeProvider(t *testing.T, status int, models []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(404)
			return
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		payload := map[string]any{"object": "list", "data": []map[string]any{}}
		for _, m := range models {
			payload["data"] = append(payload["data"].([]map[string]any), map[string]any{"id": m, "object": "model"})
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverByKeyFindsOwner(t *testing.T) {
	good := fakeProvider(t, 200, []string{"model-a", "gpt-4o-mini"})
	bad := fakeProvider(t, 401, nil)
	dead := "http://127.0.0.1:9"

	targets := []probeTarget{
		{Name: "bad", BaseURL: bad.URL},
		{Name: "good", BaseURL: good.URL},
		{Name: "dead", BaseURL: dead},
	}
	res := discoverByKey("sk-test", targets)
	if res.Provider != "good" {
		t.Fatalf("provider = %q, want good（attempts=%+v）", res.Provider, res.Attempts)
	}
	if len(res.Models) != 2 {
		t.Fatalf("模型数 = %d, want 2", len(res.Models))
	}
	if !res.Models[1].Vision {
		t.Fatal("gpt-4o-mini 应标记为视觉模型")
	}
	seen := map[string]bool{}
	for _, a := range res.Attempts {
		seen[a.Provider] = true
		if a.Provider == "good" && !a.OK {
			t.Fatal("good 应标记 ok")
		}
		if a.Provider == "bad" && a.Status != 401 {
			t.Fatalf("bad 状态 = %d, want 401", a.Status)
		}
	}
	if !seen["good"] || !seen["bad"] {
		t.Fatalf("attempts 缺少记录: %+v", res.Attempts)
	}
}

func TestDiscoverByKeyAllRejected(t *testing.T) {
	bad := fakeProvider(t, 401, nil)
	res := discoverByKey("sk-wrong", []probeTarget{{Name: "bad", BaseURL: bad.URL}})
	if res.Provider != "" {
		t.Fatalf("provider = %q, want 空", res.Provider)
	}
	if len(res.Attempts) != 1 || res.Attempts[0].OK {
		t.Fatalf("attempts = %+v", res.Attempts)
	}
	if res.Attempts[0].Error == "" {
		t.Fatal("失败应带原因")
	}
}

func TestProviderTargetsMerge(t *testing.T) {
	reg := &CloudRegistry{Providers: map[string]*Provider{
		"deepseek": {Name: "deepseek", BaseURL: "https://custom.example/v1"},
	}}
	targets := providerTargets(reg)
	byName := map[string]string{}
	for _, t := range targets {
		byName[t.Name] = t.BaseURL
	}
	if byName["deepseek"] != "https://custom.example/v1" {
		t.Fatalf("配置里的地址应优先，实际 = %q", byName["deepseek"])
	}
	for _, name := range []string{"kimi", "openai", "qwen"} {
		if byName[name] == "" {
			t.Fatalf("内置厂商 %s 缺失", name)
		}
	}
}

func TestModelsCacheIgnoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, modelsCacheName), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := loadModelsCache(dir)
	if len(cache.Providers) != 0 {
		t.Fatal("损坏的缓存应被忽略，而不是 panic")
	}
}

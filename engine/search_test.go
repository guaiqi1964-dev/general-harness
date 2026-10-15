// 联网搜索核心逻辑测试（全程离线：本机 httptest 充当 SearXNG 与网页）。
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// 仓库自带的 config.yaml 必须能被手写 YAML 解析器完整解析（含 v0.3.0 新增 search 段）。
func TestRepoConfigYAMLSearchSection(t *testing.T) {
	data, err := os.ReadFile("../config.yaml")
	if err != nil {
		t.Skip("仓库根目录缺少 config.yaml")
	}
	m, err := parseYAMLString(string(data))
	if err != nil {
		t.Fatalf("config.yaml 解析失败: %v", err)
	}
	sc := parseSearchConfig(m)
	if sc == nil {
		t.Fatal("parseSearchConfig 返回 nil")
	}
	if sc.Enabled {
		t.Error("仓库默认配置应关闭联网搜索")
	}
	if sc.Backend != "searxng" || sc.QueryMode != "keywords" {
		t.Errorf("默认后端/查询模式异常：%q / %q", sc.Backend, sc.QueryMode)
	}
	if sc.Fetch.AllowPrivate {
		t.Error("默认必须禁止抓取私网地址（SSRF 防护）")
	}
	if _, ok := sc.Backends["searxng"]; !ok {
		t.Errorf("缺少 searxng 后端示例：%v", sc.Backends)
	}
	if b := sc.Backends["searxng"]; b != nil && (b.QueryParam != "q" || b.ResultPath != "results") {
		t.Errorf("searxng 后端字段异常：%+v", b)
	}
	// 嵌套块映射必须真的被解析出来：手写 YAML 子集解析器不认行内 {} / []，
	// 若示例改成行内写法，这里会静默变成空 map（SearXNG 会收到非 JSON 响应 → 503）。
	b := sc.Backends["searxng"]
	if b == nil {
		t.Fatal("searxng 后端缺失")
	}
	if got := b.Params["format"]; got != "json" {
		t.Errorf("searxng params.format = %q，期望 json（params 必须是块映射）", got)
	}
	if got := b.FieldMap["url"]; got != "url" {
		t.Errorf("searxng map.url = %q，期望 url（map 必须是块映射）", got)
	}
	if got := b.FieldMap["snippet"]; got != "content" {
		t.Errorf("searxng map.snippet = %q，期望 content", got)
	}
	// agent.allow_commands 同样必须是块序列，否则白名单为空、所有命令被拒。
	if raw, ok := m["agent"]; ok {
		if am, isMap := raw.(map[string]any); isMap {
			if list, isList := am["allow_commands"].([]any); !isList || len(list) == 0 {
				t.Errorf("agent.allow_commands 解析结果异常（应为非空块序列）：%#v", am["allow_commands"])
			}
		} else {
			t.Errorf("agent 段应为映射，实际 %T", raw)
		}
	}
}

func TestDefaultSearchConfig(t *testing.T) {
	cfg := defaultSearchConfig()
	if cfg.Enabled {
		t.Error("默认应为关闭（G6：不启用时零影响）")
	}
	if cfg.Backend != "searxng" || cfg.QueryMode != "keywords" {
		t.Errorf("默认后端/查询模式异常：%q / %q", cfg.Backend, cfg.QueryMode)
	}
	if cfg.MaxResults != 5 || cfg.InjectBudgetChars != 4000 || cfg.CacheTTLSeconds != 300 {
		t.Errorf("默认数值异常：max=%d budget=%d ttl=%d", cfg.MaxResults, cfg.InjectBudgetChars, cfg.CacheTTLSeconds)
	}
	if cfg.FetchContent {
		t.Error("默认不抓取正文（Q4：由开关控制）")
	}
	if cfg.Fetch.AllowPrivate {
		t.Error("默认不允许内网地址（SSRF 防护）")
	}
}

func TestParseSearchConfig(t *testing.T) {
	src := `search:
  enabled: true
  backend: searxng
  query_mode: rewrite
  rewrite_model: deepseek/deepseek-chat
  max_results: 7
  timeout_seconds: 6
  inject_budget_chars: 1500
  cache_ttl_seconds: 60
  max_concurrent: 3
  fetch_content: true
  fetch:
    max_pages: 2
    max_chars_per_page: 800
    timeout_seconds: 4
    user_agent: TestAgent/1.0
    allow_private_hosts: true
  backends:
    searxng:
      type: http_json
      base_url: http://127.0.0.1:8888
      path: /search
      method: get
      query_param: q
      result_path: results
      params:
        format: json
      headers:
        X-Token: Bearer ${TEST_SEARCH_TOKEN}
      map:
        title: title
        url: url
        snippet: content
`
	m, err := parseYAMLString(src)
	if err != nil {
		t.Fatalf("解析 YAML 失败：%v", err)
	}
	cfg := parseSearchConfig(m)
	if !cfg.Enabled || cfg.Backend != "searxng" {
		t.Errorf("enabled/backend 解析异常：%v / %q", cfg.Enabled, cfg.Backend)
	}
	if cfg.QueryMode != "rewrite" || cfg.RewriteModel != "deepseek/deepseek-chat" {
		t.Errorf("query_mode/rewrite_model 解析异常：%q / %q", cfg.QueryMode, cfg.RewriteModel)
	}
	if cfg.MaxResults != 7 || cfg.TimeoutSeconds != 6 || cfg.InjectBudgetChars != 1500 ||
		cfg.CacheTTLSeconds != 60 || cfg.MaxConcurrent != 3 || !cfg.FetchContent {
		t.Errorf("数值项解析异常：%+v", cfg)
	}
	if cfg.Fetch.MaxPages != 2 || cfg.Fetch.MaxCharsPerPage != 800 || cfg.Fetch.TimeoutSeconds != 4 ||
		cfg.Fetch.UserAgent != "TestAgent/1.0" || !cfg.Fetch.AllowPrivate {
		t.Errorf("fetch 段解析异常：%+v", cfg.Fetch)
	}
	b, ok := cfg.Backends["searxng"]
	if !ok {
		t.Fatal("backends.searxng 未解析出来")
	}
	if b.Type != "http_json" || b.BaseURL != "http://127.0.0.1:8888" || b.Path != "/search" ||
		b.Method != "GET" || b.QueryParam != "q" || b.ResultPath != "results" {
		t.Errorf("后端字段解析异常：%+v", b)
	}
	if b.Params["format"] != "json" {
		t.Errorf("params 解析异常：%+v", b.Params)
	}
	if b.FieldMap["snippet"] != "content" {
		t.Errorf("map 解析异常：%+v", b.FieldMap)
	}
}

func TestSearchBackendReadyWithEnv(t *testing.T) {
	t.Setenv("TEST_SEARCH_KEY", "")
	b := &SearchBackend{Name: "x", Type: "http_json", BaseURL: "http://127.0.0.1:1",
		Headers: map[string]string{"Authorization": "Bearer ${TEST_SEARCH_KEY}"}}
	if ok, reason := b.ready(); ok || !strings.Contains(reason, "TEST_SEARCH_KEY") {
		t.Errorf("缺环境变量时应不可用，得到 ok=%v reason=%q", ok, reason)
	}
	t.Setenv("TEST_SEARCH_KEY", "abc")
	if ok, reason := b.ready(); !ok {
		t.Errorf("环境变量就绪后应可用，得到 reason=%q", reason)
	}
	if got := expandEnv("Bearer ${TEST_SEARCH_KEY}"); got != "Bearer abc" {
		t.Errorf("expandEnv 内联展开失败：%q", got)
	}
	if ok, reason := (&SearchBackend{Name: "y", Type: "unknown", BaseURL: "http://x"}).ready(); ok {
		t.Errorf("未知 type 应不可用，得到 reason=%q", reason)
	}
}

func TestExtractKeywords(t *testing.T) {
	got := extractKeywords("今天 DeepSeek 发布了什么新模型？请帮我查一下", 8)
	if !strings.Contains(got, "deepseek") {
		t.Errorf("应保留主体词 deepseek：%q", got)
	}
	if !strings.Contains(got, "今天") {
		t.Errorf("时效词「今天」不应被剔除：%q", got)
	}
	for _, bad := range []string{"什么", "请帮我", "查一下", "？", "发布了"} {
		if strings.Contains(got, bad) {
			t.Errorf("检索词残留指令词 %q：%q", bad, got)
		}
	}
	if strings.ContainsAny(got, "？，。！、") {
		t.Errorf("检索词不应含标点：%q", got)
	}

	en := extractKeywords("What is the latest DeepSeek model", 8)
	if strings.Contains(en, "what") || strings.Contains(en, " the ") {
		t.Errorf("英文停用词未剔除：%q", en)
	}
	if !strings.Contains(en, "latest") || !strings.Contains(en, "deepseek") {
		t.Errorf("英文关键词丢失：%q", en)
	}

	long := extractKeywords(strings.Repeat("模型能力评估 ", 30), 20)
	if n := len([]rune(long)); n > maxQueryRunes {
		t.Errorf("检索词长度 %d 超过上限 %d：%q", n, maxQueryRunes, long)
	}

	if got := extractKeywords("一 二 三", 8); got != "" {
		t.Errorf("单字词应被剔除，得到 %q", got)
	}
}

func TestNormalizeResultURL(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		wantOkay bool
	}{
		{"HTTPS://Example.COM/Path?utm_source=x&id=1#frag", "https://example.com/Path?id=1", true},
		{"https://example.com/a?gclid=1&fbclid=2&q=3", "https://example.com/a?q=3", true},
		{"http://example.com:8080/x", "http://example.com:8080/x", true},
		{"http://example.com:80/x", "http://example.com/x", true},
		{"ftp://example.com/x", "", false},
		{"javascript:alert(1)", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeResultURL(c.in)
		if ok != c.wantOkay || got != c.want {
			t.Errorf("normalizeResultURL(%q) = (%q, %v)，期望 (%q, %v)", c.in, got, ok, c.want, c.wantOkay)
		}
	}
}

// fakeSearchUpstream 启动一个假 SearXNG + 两个假网页。
func fakeSearchUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			base := "http://" + r.Host
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"results":[
				{"title":"DeepSeek 发布新模型","url":"https://example.com/news/a?utm_source=x&id=1#frag","content":"DeepSeek 今天发布了新模型，主打推理能力。","engine":"duckduckgo"},
				{"title":"DeepSeek 发布新模型（跟踪参数重复项）","url":"https://example.com/news/a?utm_medium=y&id=1","content":"重复链接","engine":"bing"},
				{"title":"本地页面一","url":"%s/page1","content":"本地页面摘要。","engine":"test"},
				{"title":"本地页面二（非 HTML）","url":"%s/page2","content":"非 HTML 内容。","engine":"test"},
				{"title":"缺少链接的条目","content":"应被丢弃","engine":"test"},
				{"title":"非法协议条目","url":"javascript:alert(1)","content":"应被丢弃","engine":"test"}
			]}`, base, base)
		case "/page1":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, "<html><head><style>b{}</style></head><body><nav>导航</nav><article><p>"+
				strings.Repeat("这是正文段落，包含 DeepSeek 与模型。", 10)+"</p></article></body></html>")
		case "/page2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"not":"html"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fakeSearchBackend(srv *httptest.Server) *SearchBackend {
	return &SearchBackend{
		Name: "fake", Type: "http_json", BaseURL: srv.URL, Path: "/search", Method: "GET",
		QueryParam: "q", Params: map[string]string{"format": "json"}, ResultPath: "results",
		FieldMap: map[string]string{"title": "title", "url": "url", "snippet": "content", "source": "engine"},
	}
}

func fakeSearchService(srv *httptest.Server) *searchService {
	cfg := defaultSearchConfig()
	cfg.Enabled = true
	cfg.Backend = "fake"
	cfg.MaxResults = 3
	cfg.TimeoutSeconds = 5
	cfg.CacheTTLSeconds = 60
	cfg.MaxConcurrent = 4
	cfg.Fetch = FetchConfig{MaxPages: 2, MaxCharsPerPage: 500, TimeoutSeconds: 5, UserAgent: "Test/1.0", AllowPrivate: true}
	cfg.Backends = map[string]*SearchBackend{"fake": fakeSearchBackend(srv)}
	return newSearchService(cfg)
}

func searchErrStatus(t *testing.T, err error) int {
	t.Helper()
	pe, ok := err.(*PluginError)
	if !ok {
		t.Fatalf("期望 *PluginError，得到 %v", err)
	}
	return pe.StatusCode
}

func TestSearchRetrieveNormalizeDedupeAndCap(t *testing.T) {
	srv := fakeSearchUpstream(t)
	svc := fakeSearchService(srv)

	resp, err := svc.Search(searchRequest{Query: "DeepSeek 模型", MaxResults: 3})
	if err != nil {
		t.Fatalf("检索失败：%v", err)
	}
	if resp.Backend != "fake" || resp.Cached {
		t.Errorf("响应元信息异常：backend=%q cached=%v", resp.Backend, resp.Cached)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("去重后应剩 3 条，得到 %d 条：%+v", len(resp.Results), resp.Results)
	}
	seen := map[string]bool{}
	for i, r := range resp.Results {
		if r.Index != i+1 {
			t.Errorf("编号应为 %d，得到 %d", i+1, r.Index)
		}
		if strings.Contains(r.URL, "utm_") || strings.Contains(r.URL, "#") {
			t.Errorf("URL 未归一化（残留跟踪参数或 fragment）：%q", r.URL)
		}
		if r.Title == "" || r.Source == "" {
			t.Errorf("标题/来源不应为空：%+v", r)
		}
		if seen[r.URL] {
			t.Errorf("去重失效，URL 重复：%q", r.URL)
		}
		seen[r.URL] = true
	}
	// 标题命中检索词的条目应排在前列
	if !strings.Contains(resp.Results[0].Title, "DeepSeek") {
		t.Errorf("打分排序异常，首位为 %q", resp.Results[0].Title)
	}

	// 超过上限的 max_results 被夹到 20，而不是报错
	if _, err := svc.Search(searchRequest{Query: "DeepSeek", MaxResults: 999}); err != nil {
		t.Errorf("max_results 超限应被夹取而非报错：%v", err)
	}
}

func TestSearchCacheAndFetchContent(t *testing.T) {
	srv := fakeSearchUpstream(t)
	svc := fakeSearchService(srv)

	// 抓取正文：HTML 页面得到 content，非 HTML 页面静默跳过
	yes := true
	resp, err := svc.Search(searchRequest{Query: "本地页面", MaxResults: 3, FetchContent: &yes})
	if err != nil {
		t.Fatalf("检索失败：%v", err)
	}
	var withContent, withoutContent int
	for _, r := range resp.Results {
		if r.Content != "" {
			withContent++
			if !strings.Contains(r.Content, "正文段落") {
				t.Errorf("正文抽取异常：%q", r.Content)
			}
			if strings.Contains(r.Content, "导航") {
				t.Errorf("正文残留导航噪声：%q", r.Content)
			}
		} else {
			withoutContent++
		}
	}
	if withContent != 1 || withoutContent != 2 {
		t.Errorf("正文开关结果异常：有正文 %d 条，无正文 %d 条", withContent, withoutContent)
	}

	// 第二次同请求命中缓存（不重复打上游）
	cached, err := svc.Search(searchRequest{Query: "本地页面", MaxResults: 3, FetchContent: &yes})
	if err != nil {
		t.Fatalf("检索失败：%v", err)
	}
	if !cached.Cached {
		t.Error("第二次相同请求应命中缓存")
	}
}

func TestSearchErrors(t *testing.T) {
	srv := fakeSearchUpstream(t)
	svc := fakeSearchService(srv)

	if _, err := svc.Search(searchRequest{Query: "   "}); searchErrStatus(t, err) != 400 {
		t.Errorf("空 query 应返回 400，得到 %v", err)
	}
	if _, err := svc.Search(searchRequest{Query: "x", Backend: "nope"}); searchErrStatus(t, err) != 400 {
		t.Errorf("未知后端应返回 400，得到 %v", err)
	}

	// 后端不可达 → 503
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	svc2 := fakeSearchService(srv)
	svc2.cfg.Backends["dead"] = &SearchBackend{Name: "dead", Type: "http_json", BaseURL: deadURL,
		Path: "/search", Method: "GET", QueryParam: "q", ResultPath: "results"}
	if _, err := svc2.Search(searchRequest{Query: "x", Backend: "dead"}); searchErrStatus(t, err) != 503 {
		t.Errorf("后端不可达应返回 503，得到 %v", err)
	}

	// 缺少环境变量的后端 → 503
	svc2.cfg.Backends["noenv"] = &SearchBackend{Name: "noenv", Type: "http_json", BaseURL: srv.URL,
		Headers: map[string]string{"Authorization": "Bearer ${TEST_ABSENT_KEY}"}}
	if _, err := svc2.Search(searchRequest{Query: "x", Backend: "noenv"}); searchErrStatus(t, err) != 503 {
		t.Errorf("后端缺少环境变量应返回 503，得到 %v", err)
	}

	// 关闭状态 → 403
	off := fakeSearchService(srv)
	off.cfg.Enabled = false
	if _, err := off.Search(searchRequest{Query: "x"}); searchErrStatus(t, err) != 403 {
		t.Errorf("未启用应返回 403，得到 %v", err)
	}
	if off.enabled() {
		t.Error("enabled() 应为 false")
	}
	// nil 配置（未配置 search 段）不应 panic，且处于关闭状态
	if nilSvc := newSearchService(nil); nilSvc.enabled() {
		t.Error("search 段缺失时应处于关闭状态")
	}
}

func TestSearchBackendsInfo(t *testing.T) {
	srv := fakeSearchUpstream(t)
	svc := fakeSearchService(srv)
	svc.cfg.Backends["noenv"] = &SearchBackend{Name: "noenv", Type: "http_json", BaseURL: srv.URL,
		Params: map[string]string{"token": "${TEST_ABSENT_KEY_2}"}}
	infos := svc.backendsInfo()
	if len(infos) != 2 {
		t.Fatalf("应报告 2 个后端，得到 %d 个", len(infos))
	}
	found := false
	for _, info := range infos {
		if info["name"] == "noenv" {
			found = true
			if info["ready"] != false {
				t.Errorf("noenv 应标记为未就绪：%+v", info)
			}
			if !strings.Contains(fmt.Sprint(info["reason"]), "TEST_ABSENT_KEY_2") {
				t.Errorf("未就绪原因应指出环境变量名：%+v", info)
			}
		}
	}
	if !found {
		t.Error("backendsInfo 未列出 noenv")
	}
}

func TestBuildSearchContext(t *testing.T) {
	small := []*searchResult{
		{Index: 1, Title: "标题一", URL: "https://example.com/1", Snippet: "摘要一"},
		{Index: 2, Title: "标题二", URL: "https://example.com/2", Content: strings.Repeat("长正文", 100)},
	}
	ctx := buildSearchContext(small, 200)
	if !strings.Contains(ctx, "不可信") || !strings.Contains(ctx, "[序号]") {
		t.Errorf("上下文缺少不可信声明与引用要求：%q", ctx)
	}
	if !strings.Contains(ctx, "[1] 标题一") || !strings.Contains(ctx, "URL: https://example.com/1") {
		t.Errorf("上下文格式异常：%q", ctx)
	}
	if strings.Contains(ctx, "[2] 标题二") {
		t.Errorf("预算裁剪失效，应只保留第一条：%q", ctx)
	}
	if n := len([]rune(ctx)); n > 264 {
		t.Errorf("上下文超预算：%d 字符", n)
	}

	// 有正文时优先注入正文；预算为 0 用默认预算（4000）
	withBody := []*searchResult{{Index: 1, Title: "标题", URL: "https://example.com/x", Snippet: "摘要", Content: strings.Repeat("正文内容", 200)}}
	full := buildSearchContext(withBody, 0)
	if !strings.Contains(full, strings.Repeat("正文内容", 200)) {
		t.Error("预算为 0 时应使用默认预算并保留正文")
	}
	if strings.Contains(full, "摘要: 摘要") {
		t.Error("有正文时不应把摘要当作正文注入")
	}
	if got := buildSearchContext(nil, 1000); got != "" {
		t.Errorf("空结果应返回空串，得到 %q", got)
	}
}

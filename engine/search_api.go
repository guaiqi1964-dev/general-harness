// 联网搜索的 HTTP 接口层：
//   - P1：POST /v1/search、GET /v1/search/backends；
//   - P2：/v1/chat/completions 的 web_search 增量字段与 sources 回传。
package main

import (
	"os"
	"strings"
)

// webSearchOptions chat 请求中 web_search 字段的解析结果。
type webSearchOptions struct {
	Enabled      bool
	MaxResults   int
	Backend      string
	QueryMode    string
	Query        string
	FetchContent *bool
}

// parseWebSearchOption 解析 web_search 字段（布尔形态或对象形态）。
// 字段缺失时返回 nil，表示本次请求不使用联网检索。
func parseWebSearchOption(v any) *webSearchOptions {
	switch raw := v.(type) {
	case nil:
		return nil
	case bool:
		return &webSearchOptions{Enabled: raw}
	case map[string]any:
		opts := &webSearchOptions{Enabled: true}
		if b, ok := raw["enabled"].(bool); ok {
			opts.Enabled = b
		}
		opts.MaxResults = int(toInt64(raw["max_results"]))
		opts.Backend = toStr(raw["backend"])
		opts.QueryMode = strings.ToLower(toStr(raw["query_mode"]))
		opts.Query = toStr(raw["query"])
		if b, ok := raw["fetch_content"].(bool); ok {
			opts.FetchContent = &b
		}
		return opts
	default:
		return &webSearchOptions{Enabled: true}
	}
}

// webSearchEnabled 判断本次 chat 请求是否启用联网检索。
func webSearchEnabled(opts *webSearchOptions) bool {
	return opts != nil && opts.Enabled
}

// ---- P1：检索端点 ----

func (e *Engine) handleSearch(req *httpRequest, w *responseWriter) {
	if req.Method != "POST" {
		w.JSON(405, map[string]any{"error": map[string]any{"message": "Method Not Allowed", "type": "method_not_allowed", "code": 405}})
		return
	}
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": map[string]any{"message": "网关鉴权失败：缺少或错误的 API Key", "type": "authentication_error", "code": 401}})
		return
	}
	var body map[string]any
	if err := jsonBody(req, &body); err != nil {
		w.JSON(400, map[string]any{"error": map[string]any{"message": "请求体解析失败", "type": "invalid_request_error", "code": 400}})
		return
	}
	query := strings.TrimSpace(toStr(body["query"]))
	if query == "" {
		w.JSON(400, map[string]any{"error": map[string]any{"message": "query 不能为空", "type": "invalid_request_error", "code": 400}})
		return
	}
	reqObj := searchRequest{
		Query:      query,
		MaxResults: int(toInt64(body["max_results"])),
		Backend:    toStr(body["backend"]),
	}
	if b, ok := body["fetch_content"].(bool); ok {
		reqObj.FetchContent = &b
	}
	resp, err := e.Search.Search(reqObj)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	// 只记录后端、耗时与结果数，不回显检索词。
	fmtSearchLog(resp.Backend, resp.ElapsedMS, len(resp.Results), resp.Cached)
	w.JSON(200, map[string]any{
		"query":      resp.Query,
		"backend":    resp.Backend,
		"elapsed_ms": resp.ElapsedMS,
		"cached":     resp.Cached,
		"results":    resp.Results,
	})
}

func (e *Engine) handleSearchBackends(req *httpRequest, w *responseWriter) {
	if req.Method != "GET" {
		w.JSON(405, map[string]any{"error": map[string]any{"message": "Method Not Allowed", "type": "method_not_allowed", "code": 405}})
		return
	}
	if !e.checkAuth(req) {
		w.JSON(401, map[string]any{"error": map[string]any{"message": "网关鉴权失败：缺少或错误的 API Key", "type": "authentication_error", "code": 401}})
		return
	}
	cfg := e.Search.cfg
	queryMode := "keywords"
	if cfg != nil && cfg.QueryMode != "" {
		queryMode = cfg.QueryMode
	}
	w.JSON(200, map[string]any{
		"enabled":       true,
		"query_mode":    queryMode,
		"fetch_content": cfg != nil && cfg.FetchContent,
		"data":          e.Search.backendsInfo(),
	})
}

// fmtSearchLog 检索日志：只记后端/耗时/命中数，不记检索词。
func fmtSearchLog(backend string, elapsedMS int64, hits int, cached bool) {
	state := "miss"
	if cached {
		state = "hit"
	}
	os.Stderr.WriteString("搜索 " + backend + " 耗时 " + itoa(int(elapsedMS)) + "ms 结果 " + itoa(hits) + " 条 缓存 " + state + "\n")
}

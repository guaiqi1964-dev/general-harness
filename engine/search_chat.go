// 联网搜索与对话的集成层（P2）：
//   - 由 chat 请求的 web_search 字段决定是否检索；
//   - 隐私前置地构造检索词：显式覆盖 > rewrite（只发自有厂商模型）> 本地关键词提取（Q7）；
//   - 把检索结果作为 system 上下文注入 messages，并把 sources 回传给调用方。
package main

import (
	"fmt"
	"os"
	"strings"
)

// webSearchRewritePrompt rewrite 模式的改写指令（该问题只发送给自有厂商模型，不发往搜索服务）。
const webSearchRewritePrompt = "把用户的问题改写成适合搜索引擎的检索关键词。" +
	"只输出关键词本身，用空格分隔，不要标点、不要编号、不要引号、不要任何解释，最多 8 个词。"

// prepareWebSearch 执行一次 chat 侧检索。
// 返回（注入上下文, sources, 错误）：sources 为 nil 表示本次请求没有请求联网检索；
// 检索链路失败时一律降级为无来源回答（sources 为空数组 + stderr 留痕），不阻断补全。
func (e *Engine) prepareWebSearch(raw any, messages []map[string]any, provider *Provider, model, keySelector string) (string, []map[string]any, error) {
	opts := parseWebSearchOption(raw)
	if !webSearchEnabled(opts) {
		return "", nil, nil
	}
	if !e.Search.enabled() {
		return "", nil, newPluginError("联网搜索未启用（config.yaml 中 search.enabled=false）", 403, "permission_error")
	}
	empty := []map[string]any{}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		query = e.buildSearchQuery(opts, messages, provider, model, keySelector)
	}
	if query == "" {
		fmt.Fprintln(os.Stderr, "联网检索跳过：未能构造检索词")
		return "", empty, nil
	}
	resp, err := e.Search.Search(searchRequest{
		Query:        query,
		MaxResults:   opts.MaxResults,
		Backend:      opts.Backend,
		FetchContent: opts.FetchContent,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "联网检索失败，降级为无来源回答:", err)
		return "", empty, nil
	}
	fmtSearchLog(resp.Backend, resp.ElapsedMS, len(resp.Results), resp.Cached)
	if len(resp.Results) == 0 {
		return "", empty, nil
	}
	budget := 4000
	if e.Search.cfg != nil && e.Search.cfg.InjectBudgetChars > 0 {
		budget = e.Search.cfg.InjectBudgetChars
	}
	return buildSearchContext(resp.Results, budget), sourcesOf(resp.Results), nil
}

// buildSearchQuery 构造检索词：显式 query 已在调用方处理，这里只处理 keywords / rewrite。
// 说明：不实现 raw 模式——用户问题原文绝不发往搜索服务（Q7）。
func (e *Engine) buildSearchQuery(opts *webSearchOptions, messages []map[string]any, provider *Provider, model, keySelector string) string {
	question := lastUserMessage(messages)
	if question == "" {
		return ""
	}
	cfg := e.Search.cfg
	var rewriteModel string
	if cfg != nil {
		rewriteModel = cfg.RewriteModel
	}
	mode := strings.ToLower(strings.TrimSpace(opts.QueryMode))
	if mode == "" && cfg != nil {
		mode = strings.ToLower(strings.TrimSpace(cfg.QueryMode))
	}
	if mode == "rewrite" {
		if q := e.rewriteQuery(question, rewriteModel, provider, model, keySelector); q != "" {
			return q
		}
	}
	return extractKeywords(question, 8)
}

// rewriteQuery 用自有厂商模型把问题改写成检索词；任何失败都回退到本地关键词提取。
func (e *Engine) rewriteQuery(question, rewriteModel string, provider *Provider, model, keySelector string) string {
	target, targetModel := provider, model
	if rm := strings.TrimSpace(rewriteModel); rm != "" && e.Cloud != nil {
		p, m, err := e.Cloud.resolve(rm, e.Config.Aliases)
		if err != nil {
			fmt.Fprintln(os.Stderr, "rewrite_model 解析失败，回退关键词提取:", err)
			return ""
		}
		target, targetModel = p, m
	}
	if target == nil {
		return ""
	}
	maxTokens := 64
	resp, err := target.chatCompletion(targetModel, []map[string]any{
		{"role": "system", "content": webSearchRewritePrompt},
		{"role": "user", "content": question},
	}, keySelector, nil, &maxTokens, nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "检索词改写失败，回退关键词提取:", err)
		return ""
	}
	content, _ := resp["content"].(string)
	return sanitizeQuery(content)
}

// sanitizeQuery 清理模型返回的检索词：只取第一行、去引号与标点、按词边界限长。
func sanitizeQuery(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, "\"'“”‘’`")
	if len([]rune(s)) < 2 {
		return ""
	}
	return truncateKeywords(s, maxQueryRunes)
}

// lastUserMessage 取最后一条用户消息的文本；兼容 content 为分片数组的形态。
func lastUserMessage(messages []map[string]any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		role, _ := messages[i]["role"].(string)
		if role != "user" {
			continue
		}
		if s := messageText(messages[i]["content"]); s != "" {
			return s
		}
	}
	return ""
}

// messageText 提取消息文本：字符串直接用，分片数组拼接其中的 text 片段。
func messageText(v any) string {
	switch c := v.(type) {
	case string:
		return strings.TrimSpace(c)
	case []any:
		parts := make([]string, 0, len(c))
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t := strings.TrimSpace(toStr(m["text"])); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return ""
}

// withSearchContext 把检索上下文作为首个 system 消息注入。
func withSearchContext(messages []map[string]any, ctx string) []map[string]any {
	if ctx == "" {
		return messages
	}
	out := make([]map[string]any, 0, len(messages)+1)
	out = append(out, map[string]any{"role": "system", "content": ctx})
	return append(out, messages...)
}

// sourcesOf 把检索结果整理为回传给客户端的 sources 列表。
func sourcesOf(results []*searchResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"index":   r.Index,
			"title":   r.Title,
			"url":     r.URL,
			"snippet": r.Snippet,
		})
	}
	return out
}

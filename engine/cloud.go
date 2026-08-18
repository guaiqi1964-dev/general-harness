// 云端 API 代理：OpenAI 兼容上游调用（Go 版）。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// PluginError 统一错误。
type PluginError struct {
	Message    string
	StatusCode int
	ErrorType  string
	Code       int
}

func (e *PluginError) Error() string { return e.Message }

func newPluginError(message string, status int, errType string) *PluginError {
	return &PluginError{Message: message, StatusCode: status, ErrorType: errType, Code: status}
}

func (e *PluginError) asDict() map[string]any {
	return map[string]any{
		"error": map[string]any{"message": e.Message, "type": e.ErrorType, "code": e.Code},
	}
}

const (
	connectTimeout = 10 * time.Second
	readTimeout    = 180 * time.Second
)

// postJSON 非流式 POST 到上游（带重试与错误映射）。
func postJSON(url string, headers map[string]string, payload any) (map[string]any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		respBody, status, err := doPost(url, headers, data)
		if err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(time.Duration(1<<attempt) * time.Second)
			}
			continue
		}
	if status >= 400 {
		pe := mapStatusError(status, respBody)
		if pe.StatusCode == 429 || pe.StatusCode >= 500 {
			lastErr = pe
			if attempt < 2 {
				time.Sleep(time.Duration(1<<attempt) * time.Second)
			}
			continue
		}
		return nil, pe
	}
		var data map[string]any
		if err := json.Unmarshal(respBody, &data); err != nil {
			return nil, fmt.Errorf("上游响应解析失败: %w", err)
		}
		return data, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("上游请求失败")
	}
	return nil, lastErr
}

// postStream 流式 POST，逐行回调。
func postStream(url string, headers map[string]string, payload any,
	onLine func(string) error) (int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := buildRequest("POST", url, headers, data)
	if err != nil {
		return 0, err
	}
	client := &netClient{timeout: readTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, newPluginError("上游连接错误: "+err.Error(), 502, "upstream_error")
	}
	defer resp.Body.Close()
	if resp.Status >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return resp.Status, mapStatusError(resp.Status, body)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := onLine(scanner.Text()); err != nil {
			return resp.Status, err
		}
	}
	return resp.Status, scanner.Err()
}

func doPost(url string, headers map[string]string, body []byte) ([]byte, int, error) {
	req, err := buildRequest("POST", url, headers, body)
	if err != nil {
		return nil, 0, err
	}
	client := &netClient{timeout: readTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, newPluginError("上游连接错误: "+err.Error(), 502, "upstream_error")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Status, err
	}
	return data, resp.Status, nil
}

func buildRequest(method, rawURL string, headers map[string]string, body []byte) (*httpReq, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	req := &httpReq{
		Method:  method,
		Host:    u.Host,
		Path:    u.RequestURI(),
		Headers: map[string]string{},
		Body:    body,
	}
	for k, v := range headers {
		req.Headers[k] = v
	}
	if body != nil {
		req.Headers["Content-Length"] = fmt.Sprintf("%d", len(body))
	}
	if req.Headers["Host"] == "" {
		req.Headers["Host"] = u.Host
	}
	return req, nil
}

// ---- 极简 HTTP 客户端 ----

type httpReq struct {
	Method  string
	Host    string
	Path    string
	Headers map[string]string
	Body    []byte
}

type httpResp struct {
	Status  int
	Headers map[string]string
	Body    io.ReadCloser
}

type netClient struct {
	timeout time.Duration
}

func (c *netClient) Do(req *httpReq) (*httpResp, error) {
	conn, err := net.DialTimeout("tcp", req.Host, connectTimeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	var b strings.Builder
	b.WriteString(req.Method + " " + req.Path + " HTTP/1.1\r\n")
	for k, v := range req.Headers {
		b.WriteString(k + ": " + v + "\r\n")
	}
	b.WriteString("\r\n")
	if len(req.Body) > 0 {
		b.Write(req.Body)
	}
	if _, err := conn.Write([]byte(b.String())); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 64*1024)
	line, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
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
			conn.Close()
			return nil, err
		}
		hl = strings.TrimSpace(hl)
		if hl == "" {
			break
		}
		if idx := strings.Index(hl, ":"); idx > 0 {
			headers[strings.ToLower(strings.TrimSpace(hl[:idx]))] = strings.TrimSpace(hl[idx+1:])
		}
	}
	body := &connReader{conn: conn, reader: reader}
	return &httpResp{Status: status, Headers: headers, Body: body}, nil
}

type connReader struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (r *connReader) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r *connReader) Close() error               { return r.conn.Close() }

func mapStatusError(status int, body []byte) *PluginError {
	msg := extractMessage(body)
	switch status {
	case 401:
		return newPluginError(orDefault(msg, "认证失败，请检查 API Key"), 401, "authentication_error")
	case 403:
		return newPluginError(orDefault(msg, "无权限访问上游资源"), 403, "permission_error")
	case 429:
		return newPluginError(orDefault(msg, "请求过于频繁（上游限流）"), 429, "rate_limit_error")
	default:
		if status >= 500 {
			return newPluginError(orDefault(msg, "上游服务内部错误"), 502, "upstream_error")
		}
		return newPluginError(orDefault(msg, fmt.Sprintf("上游返回错误（HTTP %d）", status)), status, "invalid_request_error")
	}
}

func extractMessage(body []byte) string {
	var data struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &data) == nil && data.Error.Message != "" {
		return data.Error.Message
	}
	return ""
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ---- 统一响应映射（OpenAI -> 统一格式） ----

func parseUsage(data map[string]any) map[string]any {
	u, ok := data["usage"].(map[string]any)
	if !ok {
		return nil
	}
	return map[string]any{
		"prompt_tokens":     toInt64(u["prompt_tokens"]),
		"completion_tokens": toInt64(u["completion_tokens"]),
		"total_tokens":      toInt64(u["total_tokens"]),
	}
}

func toUnifiedResponse(data map[string]any) map[string]any {
	choices, _ := data["choices"].([]any)
	choice := map[string]any{}
	if len(choices) > 0 {
		choice, _ = choices[0].(map[string]any)
	}
	message, _ := choice["message"].(map[string]any)
	return map[string]any{
		"id":            toStr(data["id"]),
		"content":       message["content"],
		"usage":         parseUsage(data),
		"finish_reason": choice["finish_reason"],
		"model":         toStr(data["model"]),
	}
}

func chunkToUnifiedResponse(chunk map[string]any) map[string]any {
	choices, _ := chunk["choices"].([]any)
	choice := map[string]any{}
	if len(choices) > 0 {
		choice, _ = choices[0].(map[string]any)
	}
	delta, _ := choice["delta"].(map[string]any)
	return map[string]any{
		"id":            toStr(chunk["id"]),
		"content":       delta["content"],
		"thinking":      delta["reasoning_content"],
		"usage":         parseUsage(chunk),
		"finish_reason": choice["finish_reason"],
		"model":         toStr(chunk["model"]),
	}
}

// ---- 云端补全（Provider 方法） ----

func (p *Provider) buildPayload(model string, messages []map[string]any, stream bool,
	temperature *float64, maxTokens *int, streamOptions map[string]any) map[string]any {
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}
	if temperature != nil {
		payload["temperature"] = *temperature
	}
	if maxTokens != nil {
		payload["max_tokens"] = *maxTokens
	}
	if stream && streamOptions != nil {
		payload["stream_options"] = streamOptions
	}
	return payload
}

func (p *Provider) headers(key APIKey) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + key.Key,
		"Content-Type":  "application/json",
	}
}

func (p *Provider) chatCompletion(model string, messages []map[string]any,
	keySelector string, temperature *float64, maxTokens *int) (map[string]any, error) {
	if p.ChatHandler != nil {
		return p.ChatHandler(p, model, messages, keySelector, temperature, maxTokens)
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	selected, err := p.resolveKey(keySelector)
	if err != nil {
		return nil, err
	}
	payload := p.buildPayload(model, messages, false, temperature, maxTokens, nil)
	data, err := postJSON(p.BaseURL+"/chat/completions", p.headers(selected), payload)
	if err != nil {
		return nil, err
	}
	return toUnifiedResponse(data), nil
}

func (p *Provider) streamChatCompletion(model string, messages []map[string]any,
	keySelector string, temperature *float64, maxTokens *int,
	onChunk func(map[string]any) error) error {
	if p.StreamHandler != nil {
		return p.StreamHandler(p, model, messages, keySelector, temperature, maxTokens, onChunk)
	}
	if err := p.validate(); err != nil {
		return err
	}
	selected, err := p.resolveKey(keySelector)
	if err != nil {
		return err
	}
	payload := p.buildPayload(model, messages, true, temperature, maxTokens,
		map[string]any{"include_usage": true})
	streamID := ""
	_, err = postStream(p.BaseURL+"/chat/completions", p.headers(selected), payload,
		func(line string) error {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				return nil
			}
			dataStr := strings.TrimSpace(line[len("data:"):])
			if dataStr == "[DONE]" {
				return nil
			}
			var chunk map[string]any
			if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
				return nil
			}
			unified := chunkToUnifiedResponse(chunk)
			if id, ok := unified["id"].(string); ok && id != "" {
				if streamID == "" {
					streamID = id
				}
				unified["id"] = streamID
			}
			return onChunk(unified)
		})
	return err
}

// 云端 API 代理：OpenAI 兼容上游调用（Go 版）。
package main

import (
	"bufio"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
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
		respBody, status, retryAfter, err := doPost(url, headers, data)
		if err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(retryDelay(retryAfter, attempt))
			}
			continue
		}
		if status >= 400 {
			pe := mapStatusError(status, respBody)
			if pe.StatusCode == 429 || pe.StatusCode >= 500 {
				lastErr = pe
				if attempt < 2 {
					time.Sleep(retryDelay(retryAfter, attempt))
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
		fmt.Fprintln(os.Stderr, "上游连接错误:", err)
		return 0, newPluginError("上游连接错误，请稍后重试", 502, "upstream_error")
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

func doPost(url string, headers map[string]string, body []byte) ([]byte, int, string, error) {
	req, err := buildRequest("POST", url, headers, body)
	if err != nil {
		return nil, 0, "", err
	}
	client := &netClient{timeout: readTimeout}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "上游连接错误:", err)
		return nil, 0, "", newPluginError("上游连接错误，请稍后重试", 502, "upstream_error")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Status, "", err
	}
	return data, resp.Status, resp.Headers["retry-after"], nil
}

// retryDelay 根据 Retry-After 头（秒）计算重试等待时间，缺省用指数退避。
func retryDelay(retryAfter string, attempt int) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
			if secs > 60 {
				secs = 60
			}
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

func buildRequest(method, rawURL string, headers map[string]string, body []byte) (*httpReq, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	// 拨号地址必须包含端口：URL 未显式指定时按 scheme 补默认端口
	// （http→80、https→443），否则 net.Dial 报 "missing port in address"。
	host := u.Host
	dialAddr := host
	hostOnly := host
	if h, p, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
		dialAddr = net.JoinHostPort(h, p)
	} else {
		switch u.Scheme {
		case "http":
			dialAddr = net.JoinHostPort(host, "80")
		case "https":
			dialAddr = net.JoinHostPort(host, "443")
		default:
			return nil, fmt.Errorf("不支持的 URL scheme: %s", u.Scheme)
		}
	}
	req := &httpReq{
		Method:  method,
		Host:    dialAddr,
		Path:    u.RequestURI(),
		Headers: map[string]string{},
		Body:    body,
		UseTLS:  u.Scheme == "https",
	}
	for k, v := range headers {
		req.Headers[k] = v
	}
	if body != nil {
		req.Headers["Content-Length"] = fmt.Sprintf("%d", len(body))
	}
	if req.Headers["Host"] == "" {
		// HTTP/1.1 Host 头应使用原始主机名（不含端口，除非非默认）。
		req.Headers["Host"] = host
	}
	_ = hostOnly
	return req, nil
}

// ---- 极简 HTTP 客户端 ----

type httpReq struct {
	Method    string
	Host      string
	Path      string
	Headers   map[string]string
	Body      []byte
	UseTLS    bool
	ProxyPath string // HTTP 经代理时使用的绝对 URI（http://host/path）
}

type httpResp struct {
	Status  int
	Headers map[string]string
	Body    io.ReadCloser
}

type netClient struct {
	timeout time.Duration
}

// Do 发送请求，自动跟随 3xx 重定向（最多 10 次）。
func (c *netClient) Do(req *httpReq) (*httpResp, error) {
	for redirect := 0; ; redirect++ {
		resp, err := c.doOnce(req)
		if err != nil {
			return nil, err
		}
		if !isRedirect(resp.Status) || resp.Headers["location"] == "" {
			return resp, nil
		}
		loc := resp.Headers["location"]
		resp.Body.Close()
		if redirect >= 9 {
			return nil, fmt.Errorf("重定向次数过多")
		}
		req, err = c.redirectRequest(req, loc)
		if err != nil {
			return nil, err
		}
	}
}

// doOnce 发送一次请求（不跟随重定向）。
func (c *netClient) doOnce(req *httpReq) (*httpResp, error) {
	var conn net.Conn
	var err error
	if proxyAddr := proxyFor(req); proxyAddr != "" {
		conn, err = c.dialViaProxy(req, proxyAddr)
	} else {
		conn, err = net.DialTimeout("tcp", req.Host, connectTimeout)
		if err == nil {
			// 建连后立即设置截止时间，覆盖 TLS 握手阶段。
			_ = conn.SetDeadline(time.Now().Add(c.timeout))
			if req.UseTLS {
				tlsConn := tls.Client(conn, &tls.Config{ServerName: tlsServerName(req.Host)})
				if hErr := tlsConn.Handshake(); hErr != nil {
					conn.Close()
					return nil, hErr
				}
				conn = tlsConn
			}
		}
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	resp, err := c.roundTrip(conn, req)
	if err != nil {
		return nil, err
	}
	if err := wrapResponseBody(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func isRedirect(status int) bool {
	return status == 301 || status == 302 || status == 303 || status == 307 || status == 308
}

// redirectRequest 构造重定向请求；跨主机时移除 Authorization 防止密钥泄露。
func (c *netClient) redirectRequest(req *httpReq, loc string) (*httpReq, error) {
	u, err := url.Parse(loc)
	if err != nil {
		return nil, fmt.Errorf("重定向 Location 解析失败: %v", err)
	}
	if !u.IsAbs() {
		scheme := "http"
		if req.UseTLS {
			scheme = "https"
		}
		base, _ := url.Parse(scheme + "://" + req.Host)
		u = base.ResolveReference(u)
	}
	headers := req.Headers
	if u.Host != "" && !strings.EqualFold(u.Hostname(), tlsServerName(req.Host)) {
		headers = map[string]string{}
		for k, v := range req.Headers {
			if !strings.EqualFold(k, "Authorization") {
				headers[k] = v
			}
		}
	}
	return buildRequest(req.Method, u.String(), headers, req.Body)
}

// proxyFor 按环境变量选择代理地址：HTTPS_PROXY / HTTP_PROXY / ALL_PROXY。
// 返回 "" 表示直连。
func proxyFor(req *httpReq) string {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if v := os.Getenv(name); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	return ""
}

// dialViaProxy 建立到代理的 TCP 连接，并通过 CONNECT 隧道（HTTPS）
// 或绝对 URI 转发（HTTP）到达目标。
func (c *netClient) dialViaProxy(req *httpReq, proxyAddr string) (net.Conn, error) {
	pu, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("代理地址解析失败: %v", err)
	}
	proxyHost := pu.Host
	if _, _, err2 := net.SplitHostPort(proxyHost); err2 != nil {
		switch pu.Scheme {
		case "https":
			proxyHost = net.JoinHostPort(proxyHost, "443")
		default:
			proxyHost = net.JoinHostPort(proxyHost, "80")
		}
	}
	conn, err := net.DialTimeout("tcp", proxyHost, connectTimeout)
	if err != nil {
		return nil, fmt.Errorf("无法连接代理 %s: %v", proxyHost, err)
	}
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	// 代理本身是 https 时也要 TLS。
	if pu.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: tlsServerName(proxyHost)})
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("代理 TLS 握手失败: %v", err)
		}
		conn = tlsConn
	}
	if req.UseTLS {
		// HTTPS 目标：CONNECT 隧道。
		var b strings.Builder
		b.WriteString("CONNECT " + req.Host + " HTTP/1.1\r\n")
		b.WriteString("Host: " + req.Host + "\r\n")
		b.WriteString("Proxy-Connection: keep-alive\r\n\r\n")
		if _, err := conn.Write([]byte(b.String())); err != nil {
			conn.Close()
			return nil, err
		}
		reader := bufio.NewReaderSize(conn, 4096)
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
		// 读完 CONNECT 响应头。
		for {
			hl, err := reader.ReadString('\n')
			if err != nil {
				conn.Close()
				return nil, err
			}
			if strings.TrimSpace(hl) == "" {
				break
			}
		}
		if status != 200 {
			conn.Close()
			return nil, fmt.Errorf("代理 CONNECT 失败: HTTP %d", status)
		}
		// 隧道已建立：用 bufferedConn 保留 reader 中可能已缓冲的字节，
		// 再在上层做 TLS 握手。
		tunneled := &bufferedConn{Conn: conn, r: reader}
		tlsConn := tls.Client(tunneled, &tls.Config{ServerName: tlsServerName(req.Host)})
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("上游 TLS 握手失败: %v", err)
		}
		return tlsConn, nil
	}
	// HTTP 目标：直接返回代理连接，roundTrip 时改用绝对 URI。
	req.ProxyPath = "http://" + req.Host + req.Path
	return conn, nil
}

// bufferedConn 包装 bufio.Reader 中可能已缓冲的字节。
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (bc *bufferedConn) Read(p []byte) (int, error) { return bc.r.Read(p) }

func (c *netClient) roundTrip(conn net.Conn, req *httpReq) (*httpResp, error) {
	path := req.Path
	if req.ProxyPath != "" {
		path = req.ProxyPath
	}
	var b strings.Builder
	b.WriteString(req.Method + " " + path + " HTTP/1.1\r\n")
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

// readCloser 组合 io.Reader 与关闭函数。
type readCloser struct {
	io.Reader
	close func() error
}

func (r *readCloser) Close() error { return r.close() }

// chunkedReader 解码 HTTP chunked 传输编码。
type chunkedReader struct {
	br   *bufio.Reader
	left int
	done bool
}

func newChunkedReader(r io.Reader) *chunkedReader {
	return &chunkedReader{br: bufio.NewReader(r)}
}

func (cr *chunkedReader) Read(p []byte) (int, error) {
	if cr.done {
		return 0, io.EOF
	}
	if cr.left == 0 {
		size, err := cr.readChunkSize()
		if err != nil {
			return 0, err
		}
		if size == 0 {
			cr.done = true
			return 0, io.EOF
		}
		cr.left = size
	}
	if len(p) > cr.left {
		p = p[:cr.left]
	}
	n, err := cr.br.Read(p)
	cr.left -= n
	if err != nil {
		return n, err
	}
	if cr.left == 0 {
		if err := cr.skipCRLF(); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (cr *chunkedReader) readChunkSize() (int, error) {
	line, err := cr.readLine()
	if err != nil {
		return 0, err
	}
	if i := strings.IndexByte(line, ';'); i >= 0 {
		line = line[:i]
	}
	size, err := strconv.ParseUint(strings.TrimSpace(line), 16, 32)
	if err != nil {
		return 0, fmt.Errorf("无效的 chunk size: %q", line)
	}
	return int(size), nil
}

func (cr *chunkedReader) readLine() (string, error) {
	line, err := cr.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (cr *chunkedReader) skipCRLF() error {
	_, err := cr.readLine()
	return err
}

// wrapResponseBody 处理 Transfer-Encoding: chunked 与 Content-Encoding: gzip。
func wrapResponseBody(resp *httpResp) error {
	base := resp.Body
	body := io.Reader(base)
	if strings.Contains(strings.ToLower(resp.Headers["transfer-encoding"]), "chunked") {
		body = newChunkedReader(body)
	}
	if strings.ToLower(resp.Headers["content-encoding"]) == "gzip" {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return err
		}
		body = gz
	}
	resp.Body = &readCloser{Reader: body, close: base.Close}
	return nil
}

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

// tlsServerName 从 dial 地址中提取 TLS SNI 主机名（去掉端口）。
func tlsServerName(hostPort string) string {
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		return h
	}
	return hostPort
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

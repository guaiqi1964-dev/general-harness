// 极简 HTTP/1.1 服务器（仅 net 包手写，零第三方依赖）。
// 支持 GET/POST、Content-Length 请求体、JSON 响应、SSE 流式输出。
// 仅覆盖本项目 API 所需子集，体积远小于 net/http 标准库。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type httpRequest struct {
	Method     string
	Path       string
	Query      map[string]string
	Headers    map[string]string
	Body       []byte
	RemoteAddr string
}

type httpResponse struct {
	Status  int
	Headers map[string]string
}

type httpHandler func(req *httpRequest, w *responseWriter)

type responseWriter struct {
	conn     net.Conn
	wrote    bool
	streamed bool
}

func (w *responseWriter) writeHead(status int, headers map[string]string) {
	if w.wrote {
		return
	}
	w.wrote = true
	reason := statusText(status)
	var b strings.Builder
	b.WriteString("HTTP/1.1 ")
	b.WriteString(strconv.Itoa(status))
	b.WriteString(" ")
	b.WriteString(reason)
	b.WriteString("\r\n")
	for k, v := range headers {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	_, _ = w.conn.Write([]byte(b.String()))
}

func (w *responseWriter) JSON(status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte(`{"error":{"message":"内部序列化错误","type":"internal_error","code":500}}`)
		status = 500
	}
	body := string(data)
	w.writeHead(status, map[string]string{
		"Content-Type":   "application/json; charset=utf-8",
		"Content-Length": strconv.Itoa(len(body)),
		"Connection":     "close",
	})
	_, _ = w.conn.Write([]byte(body))
}

func (w *responseWriter) SSE() {
	w.streamed = true
	w.writeHead(200, map[string]string{
		"Content-Type":  "text/event-stream",
		"Cache-Control": "no-cache",
		"Connection":    "keep-alive",
	})
}

func (w *responseWriter) SSEEvent(data string) {
	_, _ = w.conn.Write([]byte("data: " + data + "\n\n"))
}

func (w *responseWriter) Close() {
	if w.streamed {
		_, _ = w.conn.Write([]byte("data: [DONE]\n\n"))
	}
	_ = w.conn.Close()
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 204:
		return "No Content"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	default:
		return "Status"
	}
}

func serve(listener net.Listener, handler httpHandler) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleConn(conn, handler)
	}
}

func handleConn(conn net.Conn, handler httpHandler) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Second))
	reader := bufio.NewReaderSize(conn, 64*1024)
	for {
		req, err := readRequest(reader)
		if err != nil {
			return
		}
		req.RemoteAddr = conn.RemoteAddr().String()
		w := &responseWriter{conn: conn}
		handler(req, w)
		if !w.streamed {
			return // 短连接：响应后关闭
		}
	}
}

func readRequest(reader *bufio.Reader) (*httpRequest, error) {
	line, err := readLine(reader)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return nil, fmt.Errorf("非法请求行: %s", line)
	}
	method, target := parts[0], parts[1]
	path, query := splitQuery(target)
	headers := map[string]string{}
	for {
		hl, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		if hl == "" {
			break
		}
		idx := strings.Index(hl, ":")
		if idx > 0 {
			headers[strings.ToLower(strings.TrimSpace(hl[:idx]))] = strings.TrimSpace(hl[idx+1:])
		}
	}
	body := []byte{}
	if cl, ok := headers["content-length"]; ok && cl != "" {
		n, _ := strconv.Atoi(cl)
		if n > 0 && n < 16*1024*1024 {
			buf := make([]byte, n)
			if _, err := ioReadFull(reader, buf); err != nil {
				return nil, err
			}
			body = buf
		}
	}
	return &httpRequest{
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: headers,
		Body:    body,
	}, nil
}

func splitQuery(target string) (string, map[string]string) {
	idx := strings.Index(target, "?")
	if idx < 0 {
		return target, map[string]string{}
	}
	path := target[:idx]
	query := map[string]string{}
	for _, pair := range strings.Split(target[idx+1:], "&") {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			query[urlDecode(kv[0])] = urlDecode(kv[1])
		} else {
			query[urlDecode(kv[0])] = ""
		}
	}
	return path, query
}

func urlDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		if s[i] == '+' {
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// jsonBody 解析 JSON 请求体到目标结构。
func jsonBody(req *httpRequest, v any) error {
	return json.Unmarshal(req.Body, v)
}

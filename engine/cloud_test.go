// cloud_test.go —— 云端 HTTP 客户端测试。
package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestChunkedReader(t *testing.T) {
	data := "5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
	cr := newChunkedReader(strings.NewReader(data))
	out, err := io.ReadAll(cr)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello world" {
		t.Errorf("got %q, want %q", out, "hello world")
	}
}

func TestChunkedReaderMulti(t *testing.T) {
	data := "1\r\na\r\n1\r\nb\r\n1\r\nc\r\n0\r\n\r\n"
	cr := newChunkedReader(strings.NewReader(data))
	out, err := io.ReadAll(cr)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "abc" {
		t.Errorf("got %q, want %q", out, "abc")
	}
}

// keepAliveBody 模拟 HTTP/1.1 keep-alive 上游：正文读完后连接不会关闭，
// 继续读取在真实网络里会一直阻塞到读超时，这里用错误替代以免测试挂死。
type keepAliveBody struct {
	data []byte
}

func (k *keepAliveBody) Read(p []byte) (int, error) {
	if len(k.data) > 0 {
		n := copy(p, k.data)
		k.data = k.data[n:]
		return n, nil
	}
	return 0, errors.New("连接未关闭（keep-alive），继续读取会阻塞到读超时")
}

// TestWrapResponseBodyHonoursContentLength 回归测试：上游保持长连接且只给
// Content-Length 时，响应体必须按长度收尾；否则读取端会阻塞到读超时
// （v0.3.0 之前 wrapResponseBody 只处理 chunked 与 gzip）。
func TestWrapResponseBodyHonoursContentLength(t *testing.T) {
	payload := `{"ok":true}`
	resp := &httpResp{
		Status:  200,
		Headers: map[string]string{"content-length": strconv.Itoa(len(payload))},
		Body:    &readCloser{Reader: &keepAliveBody{data: []byte(payload)}, close: func() error { return nil }},
	}
	if err := wrapResponseBody(resp); err != nil {
		t.Fatalf("wrapResponseBody: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败（Content-Length 未被遵守）: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("got %q, want %q", got, payload)
	}
}

// TestNetClientReadsKeepAliveContentLengthBody 在真实 socket 上验证同一条路径：
// 手写的 netClient 面对 keep-alive + Content-Length 的响应必须立即读完返回。
func TestNetClientReadsKeepAliveContentLengthBody(t *testing.T) {
	payload := `{"id":"chatcmpl-test"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	req, err := buildRequest("POST", srv.URL+"/v1/chat/completions",
		map[string]string{"Content-Type": "application/json"}, []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	client := &netClient{timeout: 5 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("读取响应体失败（很可能阻塞到了读超时）: %v", err)
	}
	if string(body) != payload {
		t.Fatalf("got %q, want %q", body, payload)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("读取耗时 %v，说明没有按 Content-Length 收尾", elapsed)
	}
}

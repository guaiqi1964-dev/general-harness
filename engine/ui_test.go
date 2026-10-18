// /ui 内置浏览器界面的 HTTP 集成测试（真实 TCP + 手写请求，零第三方依赖）。
//
// 背景：安装包不捆绑 Python，gui/gui.py 依赖 Python 3.10+，所以无 Python 的机器
// 必须能靠引擎内置页面完成对话——这个端点是那条路径的唯一入口，必须有测试兜住。
package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// httpRaw 发一个请求并读回完整原始响应（状态码、小写头、正文原文）。
// 与 httpSend 的区别：后者假设响应是 JSON，这里要读 HTML 原文。
func httpRaw(t *testing.T, addr, method, path string) (int, map[string]string, string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", method, path, addr); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("读状态行失败: %v", err)
	}
	var status int
	if _, err := fmt.Sscanf(strings.TrimSpace(line), "HTTP/1.1 %d", &status); err != nil {
		t.Fatalf("解析状态行失败: %q", line)
	}
	headers := map[string]string{}
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("读响应头失败: %v", err)
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "" {
			break
		}
		if i := strings.Index(l, ":"); i > 0 {
			headers[strings.ToLower(strings.TrimSpace(l[:i]))] = strings.TrimSpace(l[i+1:])
		}
	}
	body, _ := io.ReadAll(br)
	return status, headers, string(body)
}

// 内置页面必须可访问、是内嵌 HTML、且引用的都是引擎真实存在的端点。
func TestBuiltinUIPage(t *testing.T) {
	addr, stop := startSearchTestServer(t, searchTestGlobalConfig(defaultSearchConfig()))
	defer stop()

	status, headers, body := httpRaw(t, addr, "GET", "/ui")
	if status != 200 {
		t.Fatalf("GET /ui 状态码 = %d，期望 200", status)
	}
	if ct := headers["content-type"]; !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q，期望 text/html", ct)
	}
	if cl := headers["content-length"]; cl != fmt.Sprintf("%d", len(body)) {
		t.Fatalf("Content-Length = %q，实际正文 %d 字节", cl, len(body))
	}
	if len(body) < 5000 {
		t.Fatalf("页面仅 %d 字节，疑似未嵌入", len(body))
	}
	for _, want := range []string{
		"<title>General Harness</title>",
		"/v1/chat/completions",
		"/v1/search/backends",
		"/v1/usage/stats",
		"sourcesHtml",
		"toggleWeb",
		"renderBubble",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("内置页面缺少 %q", want)
		}
	}
	// gui.py 专有的图表端点不存在于引擎，页面不能引用它。
	if strings.Contains(body, "/usage_chart") {
		t.Fatal("内置页面不应引用 gui.py 专有的 /usage_chart")
	}
	// 与桌面 GUI 同等能力：模型选择、思考折叠、联网开关、用量。
	for _, want := range []string{`id="model"`, "思考过程", `id="web"`, `id="usage"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("内置页面缺少 UI 元素 %q", want)
		}
	}

	// 非 GET 方法应拒绝。
	if st, _, _ := httpRaw(t, addr, "POST", "/ui"); st != 405 {
		t.Fatalf("POST /ui 状态码 = %d，期望 405", st)
	}

	// 根路径公布 ui 入口，便于无 Python 用户发现。
	_, _, rootBody := httpRaw(t, addr, "GET", "/")
	if !strings.Contains(rootBody, `"ui":"/ui"`) {
		t.Fatalf("根路径未公布 ui 入口：%s", rootBody)
	}
}

// /ui 是静态页面，必须豁免限流（否则快速刷新会被 429 挡住）。
func TestBuiltinUISkipsRateLimit(t *testing.T) {
	cfg := searchTestGlobalConfig(defaultSearchConfig())
	cfg.RateLimitPerMinute = 1
	addr, stop := startSearchTestServer(t, cfg)
	defer stop()

	for i := 0; i < 5; i++ {
		if st, _, _ := httpRaw(t, addr, "GET", "/ui"); st != 200 {
			t.Fatalf("第 %d 次 GET /ui = %d，期望 200（/ui 应豁免限流）", i+1, st)
		}
	}
}

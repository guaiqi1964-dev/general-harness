package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const keysTestPluginYAML = `plugin: openai_compatible
base_url: http://127.0.0.1:1/v1
api_keys:
  - name: default
    key: ${KEYS_TEST_MISSING_ENV}
models:
  - deepseek-chat
`

// startKeysTestServer 起一个带 deepseek 厂商的最小引擎（tempDir 作为 ROOT）。
func startKeysTestServer(t *testing.T) (addr, root string, engine *Engine, stop func()) {
	t.Helper()
	root = tempDir(t)
	_ = os.MkdirAll(filepath.Join(root, "plugins", "deepseek"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "models"), 0o755)
	if err := os.WriteFile(filepath.Join(root, "plugins", "deepseek", "config.yaml"),
		[]byte(keysTestPluginYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	engine = newEngine(searchTestGlobalConfig(nil), root)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go serve(ln, engine.dispatch)
	return ln.Addr().String(), root, engine, func() { _ = ln.Close() }
}

func TestKeyOverridesRoundTrip(t *testing.T) {
	root := tempDir(t)
	if got := loadKeyOverrides(root); len(got) != 0 {
		t.Fatalf("没有 keys.yaml 时应返回空 map，实际 %v", got)
	}
	if err := writeKeyOverrides(root, map[string]string{
		"deepseek": "sk-abcdef0123456789",
		"kimi":     "sk-with space",
	}); err != nil {
		t.Fatal(err)
	}
	got := loadKeyOverrides(root)
	if got["deepseek"] != "sk-abcdef0123456789" {
		t.Fatalf("deepseek 读回错误: %q", got["deepseek"])
	}
	if got["kimi"] != "sk-with space" {
		t.Fatalf("含空格的值应带引号写入并原样读回，实际 %q", got["kimi"])
	}
	// 权限：Key 是凭据，文件不应对同机其他用户可读。
	// Windows 上 Go 的 perm 位不映射到 NTFS ACL（由父目录继承），只在类 Unix 平台断言。
	if runtime.GOOS != "windows" {
		st, err := os.Stat(keysFilePath(root))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o077 != 0 {
			t.Fatalf("keys.yaml 权限过宽: %v", st.Mode().Perm())
		}
	}
}

func TestQuoteYAMLValue(t *testing.T) {
	cases := map[string]string{
		"sk-abc":        "sk-abc",
		"sk-with space": `"sk-with space"`,
		"sk-a#b":        `"sk-a#b"`,
	}
	for in, want := range cases {
		if got := quoteYAMLValue(in); got != want {
			t.Errorf("quoteYAMLValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskKey(t *testing.T) {
	if got := maskKey(""); got != "" {
		t.Errorf("空 Key 应返回空串，实际 %q", got)
	}
	if got := maskKey("short"); got != "*****" {
		t.Errorf("短 Key 应全掩码，实际 %q", got)
	}
	long := "sk-1234567890abcdef"
	got := maskKey(long)
	if strings.Contains(got, "1234567890") {
		t.Errorf("掩码后不应包含中段明文: %q", got)
	}
	if !strings.HasPrefix(got, "sk-1") || !strings.HasSuffix(got, "cdef") {
		t.Errorf("掩码应保留首尾 4 字符，实际 %q", got)
	}
}

func TestKeysAPISetAndHotReload(t *testing.T) {
	addr, root, engine, stop := startKeysTestServer(t)
	defer stop()

	code, body := httpSend(t, addr, "GET", "/v1/keys", "", nil)
	if code != 200 {
		t.Fatalf("GET /v1/keys = %d %v", code, body)
	}
	providers, _ := body["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("应列出 1 个厂商，实际 %d", len(providers))
	}
	first, _ := providers[0].(map[string]any)
	if first["name"] != "deepseek" {
		t.Fatalf("厂商名错误: %v", first["name"])
	}
	if first["configured"] != false {
		t.Fatalf("环境变量缺失时应显示未配置，实际 %v", first["configured"])
	}

	code, body = httpSend(t, addr, "POST", "/v1/keys",
		`{"provider":"deepseek","key":"sk-live-0123456789abcdef"}`, nil)
	if code != 200 {
		t.Fatalf("POST /v1/keys = %d %v", code, body)
	}
	if body["ok"] != true || body["masked"] != "sk-l****cdef" {
		t.Fatalf("保存响应异常: %v", body)
	}

	// 热加载：不重启引擎，下一次请求就应该能用到新 Key。
	p := engine.Cloud.Providers["deepseek"]
	if p == nil {
		t.Fatal("厂商未加载")
	}
	engine.refreshKeysIfChanged()
	if got := p.defaultKey().Key; got != "sk-live-0123456789abcdef" {
		t.Fatalf("热加载后 Key 未生效: %q", got)
	}

	// 状态应变为已配置。
	code, body = httpSend(t, addr, "GET", "/v1/keys", "", nil)
	if code != 200 {
		t.Fatalf("GET /v1/keys = %d", code)
	}
	providers, _ = body["providers"].([]any)
	first, _ = providers[0].(map[string]any)
	if first["configured"] != true {
		t.Fatalf("写入后应显示已配置，实际 %v", first["configured"])
	}

	// 删除 keys.yaml 应回退到插件配置（这里环境变量缺失 → 未配置）。
	if err := os.Remove(filepath.Join(root, "keys.yaml")); err != nil {
		t.Fatal(err)
	}
	engine.refreshKeysIfChanged()
	if got := p.defaultKey().Key; got != "" {
		t.Fatalf("删除覆盖层后应回退到插件配置，实际 %q", got)
	}
}

func TestKeysAPIValidation(t *testing.T) {
	addr, _, _, stop := startKeysTestServer(t)
	defer stop()

	cases := []struct {
		name   string
		method string
		body   string
		want   int
	}{
		{"空 provider", "POST", `{"provider":"","key":"sk-x"}`, 400},
		{"空 key", "POST", `{"provider":"deepseek","key":""}`, 400},
		{"未知厂商", "POST", `{"provider":"nope","key":"sk-x"}`, 404},
		{"非法 JSON", "POST", `{`, 400},
		{"方法错误", "PUT", "", 405},
	}
	for _, c := range cases {
		code, body := httpSend(t, addr, c.method, "/v1/keys", c.body, nil)
		if code != c.want {
			t.Errorf("%s: 期望 %d，实际 %d %v", c.name, c.want, code, body)
		}
	}
}

func TestKeysAPIIsLocalOnly(t *testing.T) {
	if !isLoopbackAddr("127.0.0.1") || !isLoopbackAddr("::1") {
		t.Fatal("回环地址判断错误")
	}
	if isLoopbackAddr("192.168.1.10") || isLoopbackAddr("8.8.8.8") || isLoopbackAddr("") {
		t.Fatal("非回环地址不应被放行")
	}
	// 直接验证 handler 的拒绝分支：伪造一个远端地址。
	_, _, engine, stop := startKeysTestServer(t)
	defer stop()
	req := &httpRequest{Method: "GET", Path: "/v1/keys", RemoteAddr: "203.0.113.9:51000"}
	cc := &captureConn{}
	w := &responseWriter{conn: cc}
	engine.handleKeys(req, w)
	line := strings.SplitN(cc.buf.String(), "\r\n", 2)[0]
	if !strings.Contains(line, " 403 ") {
		t.Fatalf("非本机调用应 403，实际响应行 %q", line)
	}
}

// captureConn 实现 net.Conn，只把写出的字节收进缓冲区，用于断言状态码。
type captureConn struct{ buf bytes.Buffer }

func TestKeysAPIGetRefreshesFromDisk(t *testing.T) {
	addr, root, _, stop := startKeysTestServer(t)
	defer stop()

	if code, body := httpSend(t, addr, "POST", "/v1/keys",
		`{"provider":"deepseek","key":"sk-live-0123456789abcdef"}`, nil); code != 200 {
		t.Fatalf("POST /v1/keys = %d %v", code, body)
	}
	code, body := httpSend(t, addr, "GET", "/v1/keys", "", nil)
	if code != 200 {
		t.Fatalf("GET /v1/keys = %d", code)
	}
	first := body["providers"].([]any)[0].(map[string]any)
	if first["configured"] != true {
		t.Fatalf("写入后应显示已配置，实际 %v", first["configured"])
	}

	// 外部删除 keys.yaml 后，即使调用方不主动刷新，GET 也应立刻反映回退。
	if err := os.Remove(filepath.Join(root, "keys.yaml")); err != nil {
		t.Fatal(err)
	}
	code, body = httpSend(t, addr, "GET", "/v1/keys", "", nil)
	if code != 200 {
		t.Fatalf("GET /v1/keys = %d", code)
	}
	first = body["providers"].([]any)[0].(map[string]any)
	if first["configured"] != false {
		t.Fatalf("删除 keys.yaml 后 GET 应立即回退为未配置，实际 %v", first["configured"])
	}
}

func TestPlaceholderKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"", false},
		{"sk-xxxxxx", true},
		{"sk-yyyyyy", true},
		{"sk-XXXX", true},
		{"sk-live-0123456789abcdef", false},
		{"sk-x9", false},
		{"xxxxxx", false},
	}
	for _, c := range cases {
		if got := placeholderKey(c.key); got != c.want {
			t.Errorf("placeholderKey(%q) = %v，期望 %v", c.key, got, c.want)
		}
	}
}

func (c *captureConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *captureConn) Write(p []byte) (int, error)      { return c.buf.Write(p) }
func (c *captureConn) Close() error                     { return nil }
func (c *captureConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *captureConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *captureConn) SetDeadline(time.Time) error      { return nil }
func (c *captureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *captureConn) SetWriteDeadline(time.Time) error { return nil }

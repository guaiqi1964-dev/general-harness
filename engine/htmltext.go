// HTML 正文抽取与出网目标校验（零第三方依赖）。
//
// 本文件服务于 v0.3.0 联网搜索的「正文模式」：
//   - ipAllowed / checkTargetURL：SSRF 防护，抓取前校验目标地址；
//   - fetchPageText：抓取单页（超时、大小、内容类型、重定向上限全部受控）；
//   - extractText：手写 HTML -> 纯文本抽取器（剔除脚本样式、解码实体、块级标签转换行）。
package main

import (
	"bufio"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ---- SSRF 防护 ----

// ipAllowed 判断目标 IP 是否允许访问。allowPrivate=true 时放行内网/回环
// （仅供用户在内网自建服务调试时显式开启）。
func ipAllowed(ip net.IP, allowPrivate bool) bool {
	if ip == nil {
		return false
	}
	if allowPrivate {
		return true
	}
	// IPv4 映射形式（::ffff:127.0.0.1）先归一化为 4 字节再判断。
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if ip.IsPrivate() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0: // 0.0.0.0/8
			return false
		case v4[0] >= 240: // 240.0.0.0/4 保留
			return false
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // 100.64.0.0/10 CGNAT
			return false
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
			return false
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // 198.18.0.0/15 基准测试
			return false
		}
		return true
	}
	// IPv6：拒绝唯一本地地址 fc00::/7。
	if len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return false
	}
	return true
}

// targetAddr 校验通过后的抓取目标：拨号用解析出的 IP（避免二次解析造成
// DNS rebinding），Host 头与 TLS SNI 仍用原始主机名。
type targetAddr struct {
	URL        *url.URL
	DialAddr   string
	HostHeader string
	ServerName string
}

// checkTargetURL 校验并解析目标 URL：仅允许 http/https，且解析出的地址
// 必须通过 ipAllowed 检查。
func checkTargetURL(rawURL string, allowPrivate bool) (*targetAddr, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("URL 解析失败: %v", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("不支持的 URL scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL 缺少主机名")
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("域名解析失败: %v", err)
	}
	var picked net.IP
	for _, ip := range ips {
		if ipAllowed(ip, allowPrivate) {
			picked = ip
			break
		}
	}
	if picked == nil {
		return nil, fmt.Errorf("目标地址被安全策略拒绝（回环/内网/保留地址）: %s", host)
	}
	return &targetAddr{
		URL:        u,
		DialAddr:   net.JoinHostPort(picked.String(), port),
		HostHeader: hostPortForHeader(u, port),
		ServerName: host,
	}, nil
}

// hostPortForHeader 生成 Host 头取值：默认端口省略，非默认端口保留。
func hostPortForHeader(u *url.URL, port string) string {
	host := u.Hostname()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		return host
	}
	return net.JoinHostPort(host, port)
}

// contentTypeAllowed 正文抓取只接受可读文本类型。
func contentTypeAllowed(ctype string) bool {
	ctype = strings.ToLower(strings.TrimSpace(ctype))
	if i := strings.IndexByte(ctype, ';'); i >= 0 {
		ctype = strings.TrimSpace(ctype[:i])
	}
	switch ctype {
	case "text/html", "application/xhtml+xml", "text/plain":
		return true
	}
	return false
}

// ---- 抓取 ----

type fetchOptions struct {
	Timeout      time.Duration
	MaxBytes     int64
	MaxRedirects int
	UserAgent    string
	AllowPrivate bool
}

func (o fetchOptions) normalized() fetchOptions {
	if o.Timeout <= 0 {
		o.Timeout = 8 * time.Second
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 2 << 20 // 2MiB
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = 3
	}
	if o.UserAgent == "" {
		o.UserAgent = "GeneralHarness/0.3"
	}
	return o
}

type fetchResult struct {
	status      int
	contentType string
	location    string
	body        []byte
}

// fetchPageText 抓取单个 URL 并抽出正文纯文本，返回 (最终 URL, 文本, 错误)。
// 每一跳重定向都重新做 SSRF 校验；不发送 Referer、不携带 Cookie；
// 为可控起见正文抓取不走 HTTP 代理（见 PLAN-v0.3.0.md §9）。
func fetchPageText(rawURL string, opt fetchOptions) (string, string, error) {
	opt = opt.normalized()
	current := rawURL
	for hop := 0; hop <= opt.MaxRedirects; hop++ {
		target, err := checkTargetURL(current, opt.AllowPrivate)
		if err != nil {
			return "", "", err
		}
		res, err := fetchOnce(target, opt)
		if err != nil {
			return "", "", err
		}
		if res.location != "" {
			next, err := resolveRedirectURL(target.URL, res.location)
			if err != nil {
				return "", "", err
			}
			current = next
			continue
		}
		if res.status >= 400 {
			return "", "", fmt.Errorf("目标返回 HTTP %d", res.status)
		}
		if !contentTypeAllowed(res.contentType) {
			return "", "", fmt.Errorf("不支持的内容类型: %s", res.contentType)
		}
		return target.URL.String(), extractText(string(res.body)), nil
	}
	return "", "", fmt.Errorf("重定向次数过多")
}

// resolveRedirectURL 以当前 URL 为基准解析 Location。
func resolveRedirectURL(base *url.URL, loc string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(loc))
	if err != nil {
		return "", fmt.Errorf("重定向 Location 解析失败: %v", err)
	}
	if !u.IsAbs() {
		u = base.ResolveReference(u)
	}
	return u.String(), nil
}

// fetchOnce 发起一次 GET（不跟随重定向）。
func fetchOnce(t *targetAddr, opt fetchOptions) (*fetchResult, error) {
	conn, err := net.DialTimeout("tcp", t.DialAddr, opt.Timeout)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(opt.Timeout))
	if t.URL.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: t.ServerName})
		if hErr := tlsConn.Handshake(); hErr != nil {
			return nil, fmt.Errorf("TLS 握手失败: %v", hErr)
		}
		conn = tlsConn
	}
	var req strings.Builder
	req.WriteString("GET " + t.URL.RequestURI() + " HTTP/1.1\r\n")
	req.WriteString("Host: " + t.HostHeader + "\r\n")
	req.WriteString("User-Agent: " + opt.UserAgent + "\r\n")
	req.WriteString("Accept: text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.1\r\n")
	req.WriteString("Accept-Encoding: gzip\r\n")
	req.WriteString("Connection: close\r\n\r\n")
	if _, err := conn.Write([]byte(req.String())); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 32*1024)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimSpace(line), " ")
	status := 0
	if len(parts) >= 2 {
		status, _ = strconv.Atoi(parts[1])
	}
	if status == 0 {
		return nil, fmt.Errorf("非法的 HTTP 响应行: %s", strings.TrimSpace(line))
	}
	headers := map[string]string{}
	for count := 0; ; count++ {
		if count >= 100 {
			return nil, fmt.Errorf("响应头数量过多")
		}
		hl, err := reader.ReadString('\n')
		if err != nil {
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
	if loc := headers["location"]; loc != "" && status >= 300 && status < 400 {
		return &fetchResult{status: status, location: loc}, nil
	}
	// Content-Length 明显超限时直接拒绝，避免无谓读取。
	if cl := headers["content-length"]; cl != "" {
		if n, aerr := strconv.ParseInt(cl, 10, 64); aerr == nil && n > opt.MaxBytes {
			return nil, fmt.Errorf("响应体过大（%d 字节，上限 %d）", n, opt.MaxBytes)
		}
	}
	var bodyReader io.Reader = reader
	if strings.Contains(strings.ToLower(headers["transfer-encoding"]), "chunked") {
		bodyReader = newChunkedReader(bodyReader)
	}
	if strings.EqualFold(headers["content-encoding"], "gzip") {
		gz, gerr := gzip.NewReader(bodyReader)
		if gerr != nil {
			return nil, fmt.Errorf("gzip 解码失败: %v", gerr)
		}
		defer gz.Close()
		bodyReader = gz
	}
	body, err := io.ReadAll(io.LimitReader(bodyReader, opt.MaxBytes))
	if err != nil && len(body) == 0 {
		return nil, err
	}
	return &fetchResult{status: status, contentType: headers["content-type"], body: body}, nil
}

// ---- HTML -> 纯文本 ----

// blockTags 视为块级的标签：遇到它们（或其结束标签）输出换行。
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "td": true, "th": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "article": true, "header": true, "footer": true, "blockquote": true,
	"pre": true, "table": true, "ul": true, "ol": true, "dd": true, "dt": true,
	"figure": true, "figcaption": true, "main": true, "aside": true, "nav": true,
	"form": true, "hr": true, "option": true, "textarea": true,
}

// dropTags 整段删除（含内容）的标签。
var dropTags = []string{"script", "style", "noscript", "svg", "template", "head", "iframe"}

// entities 常见命名实体。
var entities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'", "nbsp": " ",
	"mdash": "—", "ndash": "–", "hellip": "…", "ldquo": "“", "rdquo": "”",
	"lsquo": "‘", "rsquo": "’", "middot": "·", "copy": "©", "reg": "®",
	"trade": "™", "times": "×", "divide": "÷", "deg": "°", "larr": "←",
	"rarr": "→", "bull": "•", "laquo": "«", "raquo": "»", "sect": "§",
}

// extractText 把 HTML 转为可读纯文本。
func extractText(raw string) string {
	if raw == "" {
		return ""
	}
	s := stripComments(raw)
	for _, tag := range dropTags {
		s = stripElement(s, tag)
	}
	if region := pickMainRegion(s); region != "" {
		s = region
	}
	return normalizeWhitespace(decodeEntities(htmlToText(s)))
}

// stripComments 删除 <!-- ... --> 注释。
func stripComments(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "<!--")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		rest := s[i+4:]
		j := strings.Index(rest, "-->")
		if j < 0 {
			return b.String()
		}
		s = rest[j+3:]
	}
}

// stripElement 删除 <tag ...> ... </tag> 整段（大小写不敏感，取首个结束标签）。
func stripElement(s, tag string) string {
	var b strings.Builder
	lower := strings.ToLower(s)
	open := "<" + tag
	closeTag := "</" + tag
	for {
		i := strings.Index(lower, open)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		// 确保是标签边界（"<script" 而非 "<scriptx"）。
		after := i + len(open)
		if after < len(lower) {
			c := lower[after]
			if c != '>' && c != ' ' && c != '\t' && c != '\n' && c != '\r' && c != '/' {
				b.WriteString(s[:after])
				s = s[after:]
				lower = lower[after:]
				continue
			}
		}
		b.WriteString(s[:i])
		rest := lower[i:]
		j := strings.Index(rest, closeTag)
		if j < 0 {
			// 未闭合：丢弃到结尾，避免把脚本内容当正文。
			return b.String()
		}
		k := strings.IndexByte(rest[j:], '>')
		if k < 0 {
			return b.String()
		}
		cut := i + j + k + 1
		s = s[cut:]
		lower = lower[cut:]
	}
}

// pickMainRegion 若存在足够长的 <article>/<main>，只取该段作为正文。
func pickMainRegion(s string) string {
	for _, tag := range []string{"article", "main"} {
		start := indexTag(s, tag, false)
		if start < 0 {
			continue
		}
		end := indexTag(s[start:], tag, true)
		if end < 0 {
			continue
		}
		region := s[start : start+end]
		if len(region) >= 200 {
			return region
		}
	}
	return ""
}

// indexTag 返回 <tag / </tag 的起始下标（大小写不敏感），-1 表示不存在。
func indexTag(s, tag string, closing bool) int {
	needle := "<" + tag
	if closing {
		needle = "</" + tag
	}
	lower := strings.ToLower(s)
	i := strings.Index(lower, needle)
	if i < 0 {
		return -1
	}
	after := i + len(needle)
	if after < len(lower) {
		c := lower[after]
		if c != '>' && c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return -1
		}
	}
	return i
}

// htmlToText 逐字节扫描：标签内忽略，块级标签转换行，其余原样保留。
func htmlToText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	inAttr := false
	closing := false
	var name strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case !inTag && c == '<':
			inTag = true
			inAttr = false
			closing = false
			name.Reset()
		case inTag && c == '>':
			inTag = false
			tag := strings.ToLower(name.String())
			if closing || blockTags[tag] {
				b.WriteByte('\n')
			}
		case inTag:
			if c == '/' && name.Len() == 0 && !inAttr {
				closing = true
				continue
			}
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				inAttr = true
				continue
			}
			if !inAttr {
				name.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// decodeEntities 解码常见命名实体与数字实体。
func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		limit := i + 12
		if limit > len(s) {
			limit = len(s)
		}
		semi := strings.IndexByte(s[i:limit], ';')
		if semi < 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		body := s[i+1 : i+semi]
		if rep, ok := entities[strings.ToLower(body)]; ok {
			b.WriteString(rep)
			i += semi + 1
			continue
		}
		if strings.HasPrefix(body, "#") {
			if r, ok := parseNumericEntity(body[1:]); ok {
				b.WriteRune(r)
				i += semi + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// parseNumericEntity 解析十进制（#123）或十六进制（#x1F）数字实体。
func parseNumericEntity(body string) (rune, bool) {
	base := 10
	if len(body) > 0 && (body[0] == 'x' || body[0] == 'X') {
		base = 16
		body = body[1:]
	}
	if body == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(body, base, 32)
	if err != nil || n <= 0 || n > 0x10FFFF {
		return 0, false
	}
	return rune(n), true
}

// normalizeWhitespace 折叠行内空白与连续空行。
func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned = append(cleaned, strings.Join(strings.Fields(line), " "))
	}
	out := make([]string, 0, len(cleaned))
	prevEmpty := true
	for _, line := range cleaned {
		if line == "" {
			if prevEmpty {
				continue
			}
			prevEmpty = true
		} else {
			prevEmpty = false
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

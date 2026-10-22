// 终端 CLI 模式：把桌面 GUI 的能力搬进终端。
//
// 能做的与 GUI 基本对等：模型切换、多轮对话、流式输出、思考过程折叠、
// 联网检索开关与来源、用量统计、密钥管理、配置查看、中断当前生成。
// 区别只在于交互形式（文本命令 vs 按钮）。
//
// 用法：gh chat（交互）/ echo "问题" | gh chat（管道，一次性输出）
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiCyan    = "\x1b[36m"
	ansiYellow  = "\x1b[33m"
	ansiGreen   = "\x1b[32m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiGray    = "\x1b[90m"
	ansiRed     = "\x1b[31m"
)

// cliCanceled 用户按 Ctrl+C 中断当前生成时，从流式回调返回，让 postStream
// 立刻关闭连接（cloud.go 里 onLine 返回错误就会 Close 并停止读取）。
var cliCanceled = errors.New("用户中断")

// cliSession 一次终端会话的全部状态。
// 输出走 io.Writer、输入走 io.Reader，便于测试时替换成内存缓冲。
type cliSession struct {
	engine    *Engine
	in        *bufio.Reader
	out       io.Writer
	model     string
	sessionID string
	history   []map[string]any
	webSearch bool
	showThink bool
	color     bool
	// plain 管道模式：正文之外的状态/统计信息都不输出，保证 stdout 可直接重定向。
	plain     bool
	privacyOn bool

	// streaming/cancel 是给信号处理协程看的原子标志：
	// streaming=1 表示正在生成，此时 Ctrl+C 只中断当前请求而不是退出程序。
	streaming int32
	cancel    int32
}

// newCLISession 组装会话；out 为 nil 时用 os.Stdout。
func newCLISession(engine *Engine, in io.Reader, out io.Writer) *cliSession {
	if out == nil {
		out = os.Stdout
	}
	return &cliSession{
		engine:    engine,
		in:        bufio.NewReader(in),
		out:       out,
		model:     engine.Config.DefaultModel,
		sessionID: "cli-" + randHex(6),
		color:     colorEnabled(),
	}
}

// colorEnabled 只在真正的终端上着色，重定向到文件或设置 NO_COLOR 时输出纯文本。
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// isTerminal 判断文件是否为交互终端（管道输入时为 false）。
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func (s *cliSession) paint(code, text string) string {
	if !s.color || code == "" {
		return text
	}
	return code + text + ansiReset
}

func (s *cliSession) printf(format string, args ...any) {
	fmt.Fprintf(s.out, format, args...)
}

func (s *cliSession) println(args ...any) {
	fmt.Fprintln(s.out, args...)
}

// dim 输出一行灰色提示。
func (s *cliSession) dim(format string, args ...any) {
	s.println(s.paint(ansiDim, fmt.Sprintf(format, args...)))
}

// ok 输出一行成功提示。
func (s *cliSession) ok(format string, args ...any) {
	s.println(s.paint(ansiGreen, "✓ "+fmt.Sprintf(format, args...)))
}

// fail 输出一行错误提示。
func (s *cliSession) fail(format string, args ...any) {
	s.println(s.paint(ansiRed, "✗ "+fmt.Sprintf(format, args...)))
}

// runCLI 终端入口。交互终端进对话循环，管道输入默认走一次性模式；
// 显式给 --repl 可在管道下强制交互（便于脚本化或测试）。
func runCLI() {
	cfg := loadGlobalConfig(ROOT + "/config.yaml")
	engine := newEngine(cfg, ROOT)
	// 退出前把批量缓冲的用量记录落盘。
	defer engine.Usage.Flush()

	forceRepl := false
	for _, a := range os.Args[1:] {
		if a == "--repl" || a == "-i" {
			forceRepl = true
		}
	}
	if !forceRepl && !isTerminal(os.Stdin) {
		os.Exit(runCLIPipe(engine))
	}

	s := newCLISession(engine, os.Stdin, os.Stdout)
	s.run()
}

// run 交互主循环。
func (s *cliSession) run() {
	s.banner()
	s.installSignalHandler()

	for {
		s.printf("%s ", s.paint(ansiGreen, s.prompt()))
		line, err := s.in.ReadString('\n')
		if err != nil {
			s.println()
			s.bye()
			return
		}
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		// 以 \ 结尾表示续行，方便粘贴多行内容。
		for strings.HasSuffix(text, "\\") {
			s.printf("%s ", s.paint(ansiDim, "..."))
			next, err := s.in.ReadString('\n')
			if err != nil {
				break
			}
			text = strings.TrimSuffix(text, "\\") + "\n" + strings.TrimSpace(next)
		}
		if handled, quit := s.handleCommand(text); handled {
			if quit {
				s.bye()
				return
			}
			continue
		}
		s.chat(text)
	}
}

// installSignalHandler 让 Ctrl+C 在生成中只中断当前请求，空闲时才退出。
func (s *cliSession) installSignalHandler() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		for range ch {
			if atomic.LoadInt32(&s.streaming) == 1 {
				atomic.StoreInt32(&s.cancel, 1)
				continue
			}
			fmt.Fprintln(s.out)
			fmt.Fprintln(s.out, s.paint(ansiDim, "再见！"))
			os.Exit(0)
		}
	}()
}

func (s *cliSession) bye() {
	s.println(s.paint(ansiDim, "再见！"))
}

// prompt 提示符带当前模型与联网状态，等于 GUI 顶部的状态区。
func (s *cliSession) prompt() string {
	web := "关"
	if s.webSearch {
		web = "开"
	}
	return "你 [" + shortModel(s.model) + " | 联网:" + web + "] ›"
}

// shortModel 去掉厂商前缀，提示符太长会挤掉输入行。
func shortModel(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		return model[i+1:]
	}
	return model
}

func (s *cliSession) banner() {
	s.println(s.paint(ansiBold, "═══ General Harness CLI ═══"))
	s.dim("云端厂商 %d · 本地 GGUF %d · 模型 %s",
		len(s.engine.Cloud.Providers), len(s.engine.GGUF.Models), s.model)
	s.dim("输入 /help 看命令，/exit 退出；Ctrl+C 中断当前生成")
	s.println()
}

// handleCommand 处理斜杠命令，返回 (是否已处理, 是否退出)。
func (s *cliSession) handleCommand(text string) (bool, bool) {
	if text == "/exit" || text == "/quit" {
		return true, true
	}
	if !strings.HasPrefix(text, "/") {
		return false, false
	}
	name, arg := splitCommand(text)

	switch name {
	case "/help", "/h", "/?":
		s.cmdHelp()
	case "/models":
		s.cmdModels()
	case "/model":
		s.cmdModel(arg)
	case "/think":
		s.cmdThink(arg)
	case "/search":
		s.cmdSearch(arg)
	case "/web":
		s.cmdWeb(arg)
	case "/key":
		s.cmdKey(arg)
	case "/usage":
		s.cmdUsage(arg)
	case "/config":
		s.cmdConfig()
	case "/history":
		s.cmdHistory()
	case "/regen", "/retry":
		s.cmdRegen()
	case "/clear":
		s.history = nil
		s.dim("历史已清空")
	case "/cls", "/clearscreen":
		s.printf("\x1b[2J\x1b[H")
		s.banner()
	default:
		s.fail("未知命令 %s（/help 查看可用命令）", name)
	}
	return true, false
}

// splitCommand 拆出命令名与参数（命令名大小写不敏感）。
func splitCommand(text string) (string, string) {
	fields := strings.SplitN(strings.TrimSpace(text), " ", 2)
	name := strings.ToLower(fields[0])
	if len(fields) == 1 {
		return name, ""
	}
	return name, strings.TrimSpace(fields[1])
}

func (s *cliSession) cmdHelp() {
	s.println(s.paint(ansiBold, "对话"))
	s.dim("  <文本>            发送消息（\\ 结尾可续行）")
	s.dim("  /regen            重新生成上一条回答（丢弃该回答及其后的轮次）")
	s.dim("  /clear            清空对话历史")
	s.dim("  /cls              清屏")
	s.dim("  /history          查看本次会话轮数")
	s.println(s.paint(ansiBold, "模型"))
	s.dim("  /models           列出云端与本地模型")
	s.dim("  /model <名|序号>  切换模型")
	s.println(s.paint(ansiBold, "联网检索"))
	s.dim("  /search on|off    会话内联网开关（status 看状态）")
	s.dim("  /search list      列出可用的搜索后端")
	s.dim("  /search backend <名>  切换搜索后端")
	s.dim("  /web <检索词>     单次检索并打印编号来源")
	s.println(s.paint(ansiBold, "密钥"))
	s.dim("  /key              查看各厂商 Key 状态")
	s.dim("  /key <厂商> <密钥>  设置（写入 keys.yaml，立即生效）")
	s.dim("  /key <厂商> -     清除该厂商的覆盖，回退到插件配置")
	s.println(s.paint(ansiBold, "用量与配置"))
	s.dim("  /usage [1d|7d|30d|all|conv]  用量统计（默认 1d）")
	s.dim("  /config           查看当前生效配置")
	s.println(s.paint(ansiBold, "其他"))
	s.dim("  /think on|off     展开/折叠思考过程（默认折叠）")
	s.dim("  /exit             退出（Ctrl+C 在生成时只中断当前请求）")
}

// cmdModels 列出模型，带序号、当前标记与视觉能力，等价 GUI 的模型下拉。
func (s *cliSession) cmdModels() {
	list := s.engine.Cloud.modelList()
	s.println(s.paint(ansiBold, "云端模型"))
	for i, m := range list {
		id := toStr(m["id"])
		mark := "  "
		if id == s.model || toStr(m["id"]) == s.model {
			mark = s.paint(ansiGreen, "▸ ")
		}
		vision := ""
		if v, ok := m["vision"].(bool); ok && v {
			vision = s.paint(ansiMagenta, " [视觉]")
		}
		s.printf("  %s%s %s%s\n", mark,
			s.paint(ansiDim, fmt.Sprintf("[%d]", i+1)),
			s.paint(ansiCyan, id), vision)
	}
	if len(s.engine.GGUF.Models) > 0 {
		s.println(s.paint(ansiBold, "本地 GGUF 模型（只出元数据，不做推理）"))
		for _, m := range s.engine.GGUF.list() {
			id := "local/" + toStr(m["name"])
			mark := "  "
			if id == s.model {
				mark = s.paint(ansiGreen, "▸ ")
			}
			s.printf("  %s%s\n", mark, s.paint(ansiYellow, id))
		}
	}
	s.dim("切换: /model <名称或序号>")
}

// cmdModel 切换模型，支持按序号（与 /models 输出对应）。
func (s *cliSession) cmdModel(arg string) {
	if arg == "" {
		s.dim("当前模型: %s", s.model)
		return
	}
	if idx, ok := parseIndex(arg); ok {
		list := s.engine.Cloud.modelList()
		local := s.engine.GGUF.list()
		switch {
		case idx >= 1 && idx <= len(list):
			arg = toStr(list[idx-1]["id"])
		case idx >= 1 && idx <= len(list)+len(local):
			arg = "local/" + toStr(local[idx-len(list)-1]["name"])
		default:
			s.fail("序号 %d 超出范围（/models 查看）", idx)
			return
		}
	}
	if _, _, err := s.engine.Cloud.resolve(arg, s.engine.Config.Aliases); err != nil {
		if s.engine.GGUF.get(localName(arg)) == nil {
			s.fail("%s", err.Error())
			return
		}
	}
	s.model = arg
	s.ok("当前模型: %s", arg)
}

// parseIndex 解析纯数字，非数字返回 false。
func parseIndex(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// cmdThink 切换思考过程展示方式。
func (s *cliSession) cmdThink(arg string) {
	switch arg {
	case "", "status":
		s.dim("思考过程: %s", onOff(s.showThink))
	case "on", "1", "true":
		s.showThink = true
		s.ok("思考过程将完整展开（灰色显示）")
	case "off", "0", "false":
		s.showThink = false
		s.ok("思考过程已折叠（只显示字数）")
	default:
		s.fail("用法: /think [on|off]")
	}
}

// cmdSearch 联网检索开关与后端管理。
func (s *cliSession) cmdSearch(arg string) {
	fields := strings.Fields(arg)
	sub := ""
	if len(fields) > 0 {
		sub = fields[0]
	}
	switch sub {
	case "", "status":
		s.dim("联网检索: %s%s", onOff(s.webSearch), s.searchStatusLine())
		if !s.engine.Search.enabled() {
			s.dim("  引擎侧未启用（config.yaml 的 search.enabled=false），/search on 无法生效")
		}
	case "on":
		if !s.engine.Search.enabled() {
			s.fail("联网检索未启用（config.yaml 中 search.enabled=false）")
			return
		}
		s.webSearch = true
		s.ok("联网检索已开启")
		if !s.privacyOn {
			s.privacyOn = true
			s.dim("  发送给搜索服务的是本地提取的检索词（%s），不含你的问题原文", s.searchQueryMode())
		}
	case "off":
		s.webSearch = false
		s.ok("联网检索已关闭")
	case "list":
		s.listSearchBackends()
	case "backend":
		if len(fields) < 2 {
			s.fail("用法: /search backend <后端名>（/search list 查看）")
			return
		}
		s.switchSearchBackend(fields[1])
	default:
		s.fail("用法: /search [on|off|status|list|backend <名>]")
	}
}

func (s *cliSession) searchStatusLine() string {
	if s.engine.Search.cfg == nil {
		return " · 未配置"
	}
	return " · 后端: " + s.engine.Search.cfg.Backend + " · 检索词模式: " + s.searchQueryMode()
}

func (s *cliSession) searchQueryMode() string {
	if s.engine.Search.cfg == nil || s.engine.Search.cfg.QueryMode == "" {
		return "keywords"
	}
	return s.engine.Search.cfg.QueryMode
}

// listSearchBackends 列出后端与就绪状态（含缺环境变量的原因）。
func (s *cliSession) listSearchBackends() {
	if s.engine.Search.cfg == nil || len(s.engine.Search.cfg.Backends) == 0 {
		s.dim("没有配置搜索后端（config.yaml 的 search.backends）")
		return
	}
	current := s.engine.Search.cfg.Backend
	names := make([]string, 0, len(s.engine.Search.cfg.Backends))
	for name := range s.engine.Search.cfg.Backends {
		names = append(names, name)
	}
	sort.Strings(names)
	s.println(s.paint(ansiBold, "搜索后端"))
	for _, name := range names {
		b := s.engine.Search.cfg.Backends[name]
		mark := "  "
		if name == current {
			mark = s.paint(ansiGreen, "▸ ")
		}
		state := s.paint(ansiGreen, "就绪")
		if ok, reason := b.ready(); !ok {
			state = s.paint(ansiRed, "不可用: "+reason)
		}
		s.printf("  %s%s %s  %s\n", mark, s.paint(ansiCyan, name), s.paint(ansiDim, b.Type), state)
	}
}

// switchSearchBackend 切换后端（只改当前进程内存里的配置，不写回文件）。
func (s *cliSession) switchSearchBackend(name string) {
	if s.engine.Search.cfg == nil {
		s.fail("搜索未配置")
		return
	}
	b, ok := s.engine.Search.cfg.Backends[name]
	if !ok {
		s.fail("未知的搜索后端: %s", name)
		return
	}
	if ready, reason := b.ready(); !ready {
		s.fail("后端 %s 不可用: %s", name, reason)
		return
	}
	s.engine.Search.cfg.Backend = name
	s.ok("搜索后端已切换为 %s（仅本次运行生效）", name)
}

// cmdWeb 单次检索，不进入对话历史。
func (s *cliSession) cmdWeb(query string) {
	if query == "" {
		s.fail("用法: /web <检索词>")
		return
	}
	if !s.engine.Search.enabled() {
		s.fail("联网检索未启用（config.yaml 中 search.enabled=false）")
		return
	}
	s.dim("正在检索…")
	resp, err := s.engine.Search.Search(searchRequest{Query: query})
	if err != nil {
		s.fail("%s", err.Error())
		return
	}
	if len(resp.Results) == 0 {
		s.dim("没有检索到结果")
		return
	}
	s.dim("后端 %s · %dms · %d 条结果", resp.Backend, resp.ElapsedMS, len(resp.Results))
	for _, r := range resp.Results {
		s.printf("%s\n", s.paint(ansiCyan, fmt.Sprintf("[%d] %s", r.Index, r.Title)))
		s.printf("%s\n", s.paint(ansiGray, "    "+r.URL))
		if r.Snippet != "" {
			s.dim("    %s", truncateRunes(r.Snippet, 120))
		}
	}
}

// cmdKey 查看 / 设置 / 清除厂商 Key，等价 GUI 的 🔑 面板。
func (s *cliSession) cmdKey(arg string) {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		s.listKeys()
		return
	}
	if len(parts) == 1 {
		if parts[0] == "list" || parts[0] == "help" {
			s.listKeys()
			return
		}
		// 只给一把 Key：自动判断它是哪家的，并拉回该 Key 可用的模型。
		s.discoverKey(parts[0])
		return
	}
	if len(parts) != 2 {
		s.fail("用法: /key <厂商> <密钥|-清除>，或 /key <密钥> 自动识别厂商")
		return
	}
	provider, key := parts[0], parts[1]
	if _, ok := s.engine.Cloud.Providers[provider]; !ok {
		s.fail("未知厂商: %s", provider)
		return
	}
	root := filepath.Dir(s.engine.KeysPath)
	overrides := loadKeyOverrides(root)
	if key == "-" || key == "clear" || key == "none" {
		if _, ok := overrides[provider]; !ok {
			s.dim("%s 没有覆盖项，当前用的是插件配置里的 Key", provider)
			return
		}
		delete(overrides, provider)
		if err := writeKeyOverrides(root, overrides); err != nil {
			s.fail("写入 keys.yaml 失败: %s", err.Error())
			return
		}
		s.engine.keysMod = time.Time{}
		s.engine.refreshKeysIfChanged()
		s.ok("%s 的 Key 覆盖已清除，回退到插件配置", provider)
		return
	}
	overrides[provider] = key
	if err := writeKeyOverrides(root, overrides); err != nil {
		s.fail("写入 keys.yaml 失败: %s", err.Error())
		return
	}
	s.engine.keysMod = time.Time{}
	s.engine.refreshKeysIfChanged()
	s.ok("%s 的 Key 已保存（%s），立即生效", provider, maskKey(key))
}

// discoverKey 用一把 Key 自动判断厂商，并把该 Key 真实可用的模型拉回本地。
//
// 与 GUI「识别并保存」和 POST /v1/keys/discover 走同一条路径：
// 探测 → 写 keys.yaml → 写 models_cache.json → 并入注册表，因此不必重启。
func (s *cliSession) discoverKey(key string) {
	if len(key) < 8 {
		s.fail("这不像一把密钥。用法: /key <厂商> <密钥>，或 /key <密钥> 自动识别")
		return
	}
	s.dim("正在探测这把 Key 属于哪家厂商…")
	result := discoverByKey(key, providerTargets(s.engine.Cloud))
	if result.Provider == "" {
		s.fail("没有任何厂商接受这把 Key")
		for _, a := range result.Attempts {
			if a.Error != "" {
				s.dim("  %s: %s", a.Provider, a.Error)
			} else if a.Status != 0 {
				s.dim("  %s: HTTP %d", a.Provider, a.Status)
			}
		}
		return
	}
	root := filepath.Dir(s.engine.KeysPath)
	overrides := loadKeyOverrides(root)
	overrides[result.Provider] = key
	if err := writeKeyOverrides(root, overrides); err != nil {
		s.fail("写入 keys.yaml 失败: %s", err.Error())
		return
	}
	s.engine.keysMod = time.Time{}
	s.engine.refreshKeysIfChanged()
	if s.engine.Models == nil {
		s.engine.Models = loadModelsCache(root)
	}
	s.engine.Models.put(result.Provider, result.BaseURL, result.Models)
	if err := s.engine.Models.save(root); err != nil {
		s.dim("写入 models_cache.json 失败: %s", err.Error())
	}
	added := s.engine.Models.applyTo(s.engine.Cloud)
	s.ok("识别为 %s，可用 %d 个模型（新增 %d），Key 已保存（%s）",
		result.Provider, len(result.Models), added, maskKey(key))
	ids := make([]string, 0, len(result.Models))
	for _, m := range result.Models {
		ids = append(ids, m.ID)
	}
	if len(ids) > 0 {
		s.dim("  %s", strings.Join(ids, ", "))
	}
}

func (s *cliSession) listKeys() {
	names := make([]string, 0, len(s.engine.Cloud.Providers))
	for name := range s.engine.Cloud.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	s.println(s.paint(ansiBold, "云端厂商 Key"))
	for _, name := range names {
		p := s.engine.Cloud.Providers[name]
		k := p.defaultKey()
		state := s.paint(ansiRed, "未配置")
		if k.Key != "" {
			label := "已配置 " + maskKey(k.Key)
			if placeholderKey(k.Key) {
				label = "占位符（调用会上游 401）"
			}
			state = s.paint(ansiYellow, label)
			if !placeholderKey(k.Key) {
				state = s.paint(ansiGreen, label)
			}
		}
		s.printf("  %s  %s\n", s.paint(ansiCyan, name), state)
	}
	s.dim("设置: /key <厂商> <密钥> · 清除: /key <厂商> - · 文件: %s", s.engine.KeysPath)
}

// cmdUsage 用量统计，等价 GUI 的 📊 面板。
func (s *cliSession) cmdUsage(arg string) {
	mode := strings.ToLower(strings.TrimSpace(arg))
	if mode == "conv" || mode == "conversations" || mode == "session" {
		s.printConversations()
		return
	}
	rng := mode
	switch rng {
	case "":
		rng = "1d"
	case "1d", "7d", "30d", "all":
	default:
		s.fail("用法: /usage [1d|7d|30d|all|conv]")
		return
	}
	items, err := s.engine.Usage.QueryTimeSeries(rng, nil, nil)
	if err != nil {
		s.fail("%s", err.Error())
		return
	}
	// 引擎会为整个时间范围补零桶，终端里没必要刷一屏 0，只显示有流量的桶。
	active := make([]timeSeriesItem, 0, len(items))
	var total int64
	var peak int64
	for _, it := range items {
		if it.Tokens <= 0 {
			continue
		}
		active = append(active, it)
		total += it.Tokens
		if it.Tokens > peak {
			peak = it.Tokens
		}
	}
	if len(active) == 0 {
		s.dim("统计范围内没有用量记录（%s）", rng)
		return
	}
	s.println(s.paint(ansiBold, fmt.Sprintf("用量统计（%s）· 合计 %d tokens", rng, total)))
	for _, it := range active {
		s.printf("  %s %s %s\n",
			s.paint(ansiDim, padRight(it.Label, 12)),
			s.paint(ansiCyan, padLeft(itoa64(it.Tokens), 8)),
			s.paint(ansiBlue, usageBar(it.Tokens, peak, 30)))
	}
	s.dim("会话明细: /usage conv")
}

func (s *cliSession) printConversations() {
	items, err := s.engine.Usage.QueryRecentConversations(10)
	if err != nil {
		s.fail("%s", err.Error())
		return
	}
	if len(items) == 0 {
		s.dim("还没有会话记录")
		return
	}
	s.println(s.paint(ansiBold, "最近会话"))
	for _, it := range items {
		s.printf("  %s %s %s\n",
			s.paint(ansiCyan, padRight(it.SessionID, 16)),
			s.paint(ansiDim, padRight(it.ModelName, 24)),
			s.paint(ansiYellow, itoa64(it.Tokens)+" tokens"))
	}
}

// usageBar 用方块画条形图，与 GUI 的柱状图等价。
func usageBar(v, peak int64, width int) string {
	if peak <= 0 || width <= 0 {
		return ""
	}
	n := int(v * int64(width) / peak)
	if n == 0 && v > 0 {
		n = 1
	}
	return strings.Repeat("█", n)
}

func padRight(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(runes))
}

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// cmdConfig 打印当前生效配置（等价 GUI 里看不到但排障最常用的那部分）。
func (s *cliSession) cmdConfig() {
	cfg := s.engine.Config
	s.println(s.paint(ansiBold, "服务"))
	s.dim("  监听        %s:%d", cfg.Host, cfg.Port)
	s.dim("  限流        %d 次/分钟", cfg.RateLimitPerMinute)
	s.dim("  CORS        %s", cfg.CORSAllowOrigin)
	auth := "关闭（仅本机建议）"
	if cfg.GatewayAPIKey != "" {
		auth = "开启（Bearer " + maskKey(cfg.GatewayAPIKey) + "）"
	}
	s.dim("  网关鉴权    %s", auth)
	s.println(s.paint(ansiBold, "模型"))
	s.dim("  默认模型    %s", cfg.DefaultModel)
	if len(cfg.Aliases) > 0 {
		names := make([]string, 0, len(cfg.Aliases))
		for name := range cfg.Aliases {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			s.dim("  别名 %-8s→ %s", name, cfg.Aliases[name])
		}
	}
	s.dim("  云端厂商    %d 个", len(s.engine.Cloud.Providers))
	s.dim("  本地 GGUF   %d 个", len(s.engine.GGUF.Models))
	s.println(s.paint(ansiBold, "联网检索"))
	if s.engine.Search.cfg == nil || !s.engine.Search.enabled() {
		s.dim("  状态        未启用（search.enabled=false）")
	} else {
		sc := s.engine.Search.cfg
		s.dim("  状态        启用 · 后端 %s · 模式 %s", sc.Backend, s.searchQueryMode())
		s.dim("  结果上限    %d 条 · 注入预算 %d 字符 · 缓存 %ds",
			sc.MaxResults, sc.InjectBudgetChars, sc.CacheTTLSeconds)
		fetch := "关闭"
		if sc.FetchContent {
			fetch = fmt.Sprintf("开启（最多 %d 页 / 每页 %d 字符）", sc.Fetch.MaxPages, sc.Fetch.MaxCharsPerPage)
		}
		s.dim("  正文抓取    %s", fetch)
	}
	s.println(s.paint(ansiBold, "其他"))
	agent := "关闭"
	if cfg.Agent != nil && cfg.Agent.Enabled {
		agent = "开启（白名单 " + itoa(len(cfg.Agent.AllowCommands)) + " 条命令）"
	}
	s.dim("  Agent 执行  %s", agent)
	s.dim("  配置文件    %s", filepath.Join(ROOT, "config.yaml"))
	s.dim("  密钥文件    %s", s.engine.KeysPath)
	s.dim("  用量文件    %s", filepath.Join(ROOT, "usage_stats.json"))
	s.dim("  会话 ID     %s · 历史 %d 条消息", s.sessionID, len(s.history))
}

// cmdHistory 查看本次会话的轮数。
func (s *cliSession) cmdHistory() {
	if len(s.history) == 0 {
		s.dim("本次会话还没有对话")
		return
	}
	turns := 0
	for _, m := range s.history {
		if toStr(m["role"]) == "user" {
			turns++
		}
	}
	s.dim("本次会话 %d 轮 · %d 条消息 · 会话 ID %s", turns, len(s.history), s.sessionID)
	start := 0
	if len(s.history) > 6 {
		start = len(s.history) - 6
	}
	for _, m := range s.history[start:] {
		role := "你"
		if toStr(m["role"]) == "assistant" {
			role = "助手"
		}
		s.printf("  %s %s\n", s.paint(ansiDim, role),
			truncateRunes(strings.ReplaceAll(toStr(m["content"]), "\n", " "), 50))
	}
}

// chat 发送一条消息并流式接收回复。
// cmdRegen 重新生成上一条回答：丢弃最后一轮问答，用同一个问题重发。
// 与 GUI 的「🔄 重新生成」等价——覆盖式，不做多分支翻页。
func (s *cliSession) cmdRegen() {
	if atomic.LoadInt32(&s.streaming) == 1 {
		s.fail("正在生成中，等这次结束再试")
		return
	}
	lastUser := -1
	for i := len(s.history) - 1; i >= 0; i-- {
		if toStr(s.history[i]["role"]) == "user" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		s.fail("没有可重新生成的提问（对话为空）")
		return
	}
	question := toStr(s.history[lastUser]["content"])
	dropped := len(s.history) - lastUser
	s.history = s.history[:lastUser]
	s.dim("↻ 重新生成：%s（丢弃 %d 条消息）", truncateRunes(strings.ReplaceAll(question, "\n", " "), 40), dropped)
	s.chat(question)
}

func (s *cliSession) chat(text string) {
	s.history = append(s.history, map[string]any{"role": "user", "content": text})
	s.printf("%s ", s.paint(ansiBold, "助手 ›"))
	reply, sources := s.streamReply(s.history)
	if len(sources) > 0 {
		s.printSources(sources)
	}
	if reply != "" {
		s.history = append(s.history, map[string]any{"role": "assistant", "content": reply})
	} else {
		// 没有拿到回复时不要把空回答留在历史里，否则下一轮上下文会失衡。
		s.history = s.history[:len(s.history)-1]
	}
	s.println()
}

// printSources 打印编号来源（等价 GUI 的来源卡片）。
func (s *cliSession) printSources(sources []map[string]any) {
	s.println(s.paint(ansiBold, "来源"))
	for _, src := range sources {
		s.printf("  %s %s\n", s.paint(ansiDim, "["+toStr(src["index"])+"]"),
			s.paint(ansiCyan, toStr(src["title"])))
		s.printf("      %s\n", s.paint(ansiGray, toStr(src["url"])))
	}
}

// streamReply 流式生成，返回（正文, 来源）。
// 渲染策略与 GUI 对齐：检索状态行 → 思考折叠（或展开）→ 正文 → 收尾统计。
func (s *cliSession) streamReply(messages []map[string]any) (string, []map[string]any) {
	// 本地 GGUF 模型只输出固定摘要（与 HTTP 路径一致）。
	if r := s.engine.GGUF.get(localName(s.model)); r != nil {
		reply := localGGUFReply(r)
		s.println(s.paint(ansiCyan, reply))
		return reply, nil
	}
	provider, actual, err := s.engine.Cloud.resolve(s.model, s.engine.Config.Aliases)
	if err != nil {
		s.println()
		s.fail("%s", err.Error())
		s.hintAuth(err)
		return "", nil
	}
	selected, err := provider.resolveKey("")
	if err != nil {
		s.println()
		s.fail("%s", err.Error())
		s.hintAuth(err)
		return "", nil
	}

	var sources []map[string]any
	if s.webSearch {
		if !s.plain {
			s.printf("\r%s", s.paint(ansiDim, "⏳ 正在检索…"))
		}
		var searchErr error
		var ctx string
		ctx, sources, searchErr = s.engine.prepareWebSearch(true, messages, provider, actual, "")
		if searchErr != nil {
			s.println()
			s.fail("%s", searchErr.Error())
			return "", nil
		}
		if !s.plain {
			if len(sources) > 0 {
				s.printf("\r\x1b[K%s\n", s.paint(ansiDim,
					fmt.Sprintf("📡 已检索到 %d 条来源", len(sources))))
			} else {
				s.printf("\r\x1b[K%s\n", s.paint(ansiDim, "📡 未检索到结果，按无来源回答"))
			}
		}
		if ctx != "" {
			messages = withSearchContext(messages, ctx)
		}
	}

	requestID := "req-" + randHex(6)
	atomic.StoreInt32(&s.streaming, 1)
	atomic.StoreInt32(&s.cancel, 0)
	defer atomic.StoreInt32(&s.streaming, 0)

	started := time.Now()
	var full strings.Builder
	var thinkRunes int
	thinkLine := false
	bodyStarted := false
	recorded := false
	var lastThinkCount int
	lastThinkAt := time.Now()

	err = provider.streamChatCompletion(actual, messages, "", nil, nil, nil, nil,
		func(chunk map[string]any) error {
			if atomic.LoadInt32(&s.cancel) == 1 {
				return cliCanceled
			}
			if !recorded {
				if usage, ok := chunk["usage"].(map[string]any); ok && usage != nil {
					recorded = true
					s.engine.Usage.Record(s.sessionID, requestID, selected.Name, actual,
						int(toInt64(usage["prompt_tokens"])),
						int(toInt64(usage["completion_tokens"])),
						int(toInt64(usage["total_tokens"])), 0)
				}
			}
			if thinking := toStr(chunk["thinking"]); thinking != "" {
				thinkRunes += len([]rune(thinking))
				switch {
				case s.showThink:
					s.printf("%s", s.paint(ansiGray, thinking))
				case s.color:
					// 折叠：在终端里原地刷新一行字数。必须节流，否则每个
					// chunk 都写一次会疯狂刷写，也会把 \r 序列灌进日志。
					now := time.Now()
					if thinkRunes-lastThinkCount >= 5 || now.Sub(lastThinkAt) >= 120*time.Millisecond {
						s.printf("\r\x1b[K%s", s.paint(ansiGray,
							fmt.Sprintf("💭 思考中… %d 字", thinkRunes)))
						lastThinkCount, lastThinkAt, thinkLine = thinkRunes, now, true
					}
				}
				// 非终端（重定向/管道）不逐帧输出，正文开始前给一行汇总。
			}
			if content := toStr(chunk["content"]); content != "" {
				if !bodyStarted {
					bodyStarted = true
					if thinkLine {
						s.printf("\r\x1b[K")
						thinkLine = false
					} else if thinkRunes > 0 && !s.showThink && !s.plain && !s.color {
						s.printf("%s\n", s.paint(ansiDim,
							fmt.Sprintf("💭 思考 %d 字（/think on 展开）", thinkRunes)))
					}
				}
				full.WriteString(content)
				s.printf("%s", s.paint(ansiGreen, content))
			}
			return nil
		})

	elapsed := time.Since(started)
	if err != nil {
		if errors.Is(err, cliCanceled) {
			s.printf("\n%s\n", s.paint(ansiYellow,
				fmt.Sprintf("⏹ 已中断（已输出 %d 字，未计入上下文）", len([]rune(full.String())))))
			return full.String(), sources
		}
		if thinkLine {
			s.printf("\r\x1b[K")
		}
		s.println()
		if pe, ok := err.(*PluginError); ok {
			s.fail("%s", pe.Message)
		} else {
			s.fail("%s", err.Error())
		}
		s.hintNetwork(err)
		return full.String(), sources
	}
	if thinkLine {
		s.printf("\r\x1b[K")
	}
	out := full.String()
	if out != "" && !s.plain {
		s.printf("\n%s\n", s.paint(ansiDim,
			fmt.Sprintf("⏱ %.1fs · 输出 %d 字", elapsed.Seconds(), len([]rune(out)))))
	}
	if !recorded {
		// 上游没给 usage 也要记一笔，否则用量统计会漏掉本地/异常结束的请求。
		s.engine.Usage.Record(s.sessionID, requestID, selected.Name, actual, 0, 0, 0, 0)
	}
	return out, sources
}

// hintAuth 鉴权类错误的下一步提示，省得用户自己猜。
func (s *cliSession) hintAuth(err error) {
	msg := err.Error()
	if strings.Contains(msg, "api_key") || strings.Contains(msg, "未配置") {
		s.dim("  提示: 用 /key <厂商> <密钥> 填一把 Key（写入 %s，立即生效）", s.engine.KeysPath)
	}
}

// hintNetwork 网络类错误给出可操作的排查方向。
func (s *cliSession) hintNetwork(err error) {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "连接错误") || strings.Contains(msg, "timeout") || strings.Contains(msg, "dial"):
		s.dim("  提示: 检查网络/代理（HTTPS_PROXY）；或稍后重试")
	case strings.Contains(msg, "401") || strings.Contains(msg, "api key"):
		s.dim("  提示: Key 无效，用 /key <厂商> <密钥> 更新")
	case strings.Contains(msg, "429") || strings.Contains(msg, "限流"):
		s.dim("  提示: 上游限流，稍等再试")
	}
}

// runCLIPipe 管道模式入口：echo "问题" | gh chat，一次性输出。
func runCLIPipe(engine *Engine) int {
	return runPipe(engine, os.Stdin, os.Stdout, os.Stderr)
}

// runPipe 管道模式实现（输入输出可替换，便于测试）。
func runPipe(engine *Engine, in io.Reader, out, errOut io.Writer) int {
	data, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(errOut, "读取标准输入失败:", err)
		return 1
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		fmt.Fprintln(errOut, "标准输入为空；交互模式请直接运行 gh chat")
		return 1
	}
	s := newCLISession(engine, strings.NewReader(""), out)
	s.color = false
	s.plain = true
	s.history = []map[string]any{{"role": "user", "content": text}}
	reply, _ := s.streamReply(s.history)
	if reply == "" {
		return 1
	}
	return 0
}

// onOff 开关的中文展示。
func onOff(b bool) string {
	if b {
		return "开"
	}
	return "关"
}

// runModels 列出模型子命令。
func runModels() {
	cfg := loadGlobalConfig(ROOT + "/config.yaml")
	engine := newEngine(cfg, ROOT)
	s := newCLISession(engine, strings.NewReader(""), os.Stdout)
	s.cmdModels()
}

// runGGUF 解析 GGUF 子命令。
func runGGUF(path string) {
	reader, err := openGGUF(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析失败:", err)
		os.Exit(1)
	}
	st, _ := os.Stat(path)
	size := int64(0)
	if st != nil {
		size = st.Size()
	}
	data, _ := json.MarshalIndent(reader.toDict(size), "", "  ")
	fmt.Println(string(data))
}

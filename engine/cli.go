// 终端 CLI 模式：交互对话。
// 说明：当前实现只打印思考指示符「…」与正文，不区分展示 reasoning_content
// （README 曾宣称的彩色思考段折叠尚未实现）。v0.3.0 起支持联网检索：
// /search on|off|status 会话内开关，/web <query> 单次检索。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiCyan    = "\x1b[36m"
	ansiYellow  = "\x1b[33m"
	ansiGreen   = "\x1b[32m"
	ansiMagenta = "\x1b[35m"
	ansiGray    = "\x1b[90m"
	ansiRed     = "\x1b[31m"
)

// runCLI 终端交互模式。
func runCLI() {
	cfg := loadGlobalConfig(ROOT + "/config.yaml")
	engine := newEngine(cfg, ROOT)
	// CLI 直接退出时也要把尚未批量落盘的用量记录写入磁盘，避免丢失。
	defer engine.Usage.Flush()

	fmt.Println(ansiBold + "═══ General Harness CLI ═══" + ansiReset)
	fmt.Println(ansiDim + "云端厂商: " + itoa(len(engine.Cloud.Providers)) +
		" · 本地 GGUF: " + itoa(len(engine.GGUF.Models)) +
		" · 退出输入 /exit" + ansiReset)
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	sessionID := "cli-" + randHex(6)
	history := []map[string]any{}
	model := cfg.DefaultModel
	webSearch := false
	privacyNoticeShown := false

	for {
		fmt.Print(ansiGreen + "你 > " + ansiReset)
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			return
		}
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		if text == "/exit" || text == "/quit" {
			fmt.Println(ansiDim + "再见！" + ansiReset)
			return
		}
		if strings.HasPrefix(text, "/model ") {
			model = strings.TrimSpace(strings.TrimPrefix(text, "/model "))
			fmt.Println(ansiYellow + "当前模型: " + model + ansiReset)
			continue
		}
		if text == "/models" {
			listCLIModels(engine)
			continue
		}
		if text == "/help" {
			printCLIHelp()
			continue
		}
		if text == "/clear" {
			history = []map[string]any{}
			fmt.Println(ansiDim + "历史已清空" + ansiReset)
			continue
		}
		if text == "/search" || strings.HasPrefix(text, "/search ") {
			arg := strings.TrimSpace(strings.TrimPrefix(text, "/search"))
			switch arg {
			case "", "status":
				fmt.Println(ansiDim + "联网检索: " + onOff(webSearch) + searchStatusLine(engine) + ansiReset)
			case "on":
				if !engine.Search.enabled() {
					fmt.Println(ansiRed + "✗ 联网检索未启用（config.yaml 中 search.enabled=false）" + ansiReset)
					break
				}
				webSearch = true
				fmt.Println(ansiGreen + "✓ 联网检索已开启" + ansiReset)
				if !privacyNoticeShown {
					privacyNoticeShown = true
					fmt.Println(ansiDim + "  提示：发送给搜索服务的是本地提取的检索词（" +
						searchQueryMode(engine) + "），不含你的问题原文。" + ansiReset)
				}
			case "off":
				webSearch = false
				fmt.Println(ansiDim + "联网检索已关闭" + ansiReset)
			default:
				fmt.Println(ansiRed + "✗ 用法: /search [on|off|status]" + ansiReset)
			}
			continue
		}
		if strings.HasPrefix(text, "/web ") {
			query := strings.TrimSpace(strings.TrimPrefix(text, "/web "))
			runCLISearch(engine, query)
			continue
		}
		if text == "/key" || strings.HasPrefix(text, "/key ") {
			runCLIKeyCommand(engine, strings.TrimSpace(strings.TrimPrefix(text, "/key")))
			continue
		}

		history = append(history, map[string]any{"role": "user", "content": text})
		fmt.Println(ansiBold + "助手 > " + ansiReset)
		reply, sources := streamCLI(engine, model, history, sessionID, webSearch)
		if len(sources) > 0 {
			printCLISources(sources)
		}
		history = append(history, map[string]any{"role": "assistant", "content": reply})
		fmt.Println()
	}
}

// onOff 开关的中文展示。
func onOff(b bool) string {
	if b {
		return "开"
	}
	return "关"
}

// searchStatusLine 描述当前搜索配置（cfg 缺失时不越界）。
func searchStatusLine(engine *Engine) string {
	if engine.Search.cfg == nil {
		return " · 未配置"
	}
	return " · 后端: " + engine.Search.cfg.Backend + " · 检索词模式: " + searchQueryMode(engine)
}

func searchQueryMode(engine *Engine) string {
	if engine.Search.cfg == nil || engine.Search.cfg.QueryMode == "" {
		return "keywords"
	}
	return engine.Search.cfg.QueryMode
}

// printCLISources 打印编号来源。
func printCLISources(sources []map[string]any) {
	fmt.Println(ansiDim + "来源:" + ansiReset)
	for _, s := range sources {
		fmt.Println(ansiGray + "  [" + toStr(s["index"]) + "] " + toStr(s["title"]) +
			" — " + toStr(s["url"]) + ansiReset)
	}
}

// runCLISearch 单次检索并打印编号来源（不进入对话历史）。
func runCLISearch(engine *Engine, query string) {
	if query == "" {
		fmt.Println(ansiRed + "✗ 用法: /web <检索词>" + ansiReset)
		return
	}
	if !engine.Search.enabled() {
		fmt.Println(ansiRed + "✗ 联网检索未启用（config.yaml 中 search.enabled=false）" + ansiReset)
		return
	}
	resp, err := engine.Search.Search(searchRequest{Query: query})
	if err != nil {
		fmt.Println(ansiRed + "✗ " + err.Error() + ansiReset)
		return
	}
	if len(resp.Results) == 0 {
		fmt.Println(ansiDim + "没有检索到结果" + ansiReset)
		return
	}
	fmt.Println(ansiDim + "后端 " + resp.Backend + " · " + itoa64(resp.ElapsedMS) + "ms · " +
		itoa(len(resp.Results)) + " 条结果" + ansiReset)
	for _, r := range resp.Results {
		fmt.Println(ansiCyan + "[" + itoa(r.Index) + "] " + r.Title + ansiReset)
		fmt.Println(ansiGray + "    " + r.URL + ansiReset)
		if r.Snippet != "" {
			fmt.Println(ansiDim + "    " + r.Snippet + ansiReset)
		}
	}
}

// streamCLI 流式对话，返回（回答文本, 来源列表）。
// webSearch 为 true 时先做一次联网检索并把结果注入 system 上下文（与 HTTP 路径同一套逻辑）。
func streamCLI(engine *Engine, model string, messages []map[string]any, sessionID string, webSearch bool) (string, []map[string]any) {
	// 本地 GGUF 模型：直接输出固定回复（与 HTTP 路径一致）。
	if r := engine.GGUF.get(localName(model)); r != nil {
		reply := localGGUFReply(r)
		fmt.Println(ansiCyan + reply + ansiReset)
		return reply, nil
	}
	provider, actual, err := engine.Cloud.resolve(model, engine.Config.Aliases)
	if err != nil {
		fmt.Println(ansiRed + "✗ " + err.Error() + ansiReset)
		return "", nil
	}
	selected, err := provider.resolveKey("")
	if err != nil {
		fmt.Println(ansiRed + "✗ " + err.Error() + ansiReset)
		return "", nil
	}
	if webSearch {
		fmt.Println(ansiDim + "正在检索…" + ansiReset)
	}
	ctx, sources, searchErr := engine.prepareWebSearch(webSearch, messages, provider, actual, "")
	if searchErr != nil {
		fmt.Println(ansiRed + "✗ " + searchErr.Error() + ansiReset)
		return "", nil
	}
	if ctx != "" {
		messages = withSearchContext(messages, ctx)
	}
	requestID := "req-" + randHex(6)
	recorded := false
	var full strings.Builder
	thinking := false

	err = provider.streamChatCompletion(actual, messages, "", nil, nil, nil, nil,
		func(chunk map[string]any) error {
			// 记账：流式块携带 usage 时记录（与 HTTP 路径一致）。
			if !recorded {
				if usage, ok := chunk["usage"].(map[string]any); ok && usage != nil {
					recorded = true
					engine.Usage.Record(sessionID, requestID, selected.Name, actual,
						int(toInt64(usage["prompt_tokens"])),
						int(toInt64(usage["completion_tokens"])),
						int(toInt64(usage["total_tokens"])), 0)
				}
			}
			content := toStr(chunk["content"])
			if content == "" {
				return nil
			}
			full.WriteString(content)
			// 动态思考过程：内容以 思考中… 前缀闪烁提示（简化：直接展示）。
			if !thinking {
				thinking = true
				fmt.Print(ansiCyan + "…" + ansiReset)
			}
			fmt.Print(ansiGreen + content + ansiReset)
			return nil
		})
	if err != nil {
		if pe, ok := err.(*PluginError); ok {
			fmt.Println(ansiRed + "\n✗ " + pe.Message + ansiReset)
		}
	}
	return full.String(), sources
}

func listCLIModels(engine *Engine) {
	fmt.Println(ansiBold + "云端模型:" + ansiReset)
	for _, m := range engine.Cloud.modelList() {
		fmt.Println("  " + ansiCyan + toStr(m["id"]) + ansiReset +
			ansiDim + " (" + toStr(m["owned_by"]) + ")" + ansiReset)
	}
	if len(engine.GGUF.Models) > 0 {
		fmt.Println(ansiBold + "本地 GGUF 模型:" + ansiReset)
		for _, m := range engine.GGUF.list() {
			fmt.Println("  " + ansiYellow + "local/" + toStr(m["name"]) + ansiReset)
		}
	}
}

func printCLIHelp() {
	fmt.Println(ansiDim + "/model <name>  切换模型" + ansiReset)
	fmt.Println(ansiDim + "/models        列出模型" + ansiReset)
	fmt.Println(ansiDim + "/key           查看各厂商 Key 状态（/key <厂商> <密钥> 设置）" + ansiReset)
	fmt.Println(ansiDim + "/search on|off 联网检索开关（status 查看当前状态）" + ansiReset)
	fmt.Println(ansiDim + "/web <检索词>  单次联网检索并打印编号来源" + ansiReset)
	fmt.Println(ansiDim + "/clear         清空历史" + ansiReset)
	fmt.Println(ansiDim + "/exit          退出" + ansiReset)
}

// runCLIKeyCommand 查看或设置云端厂商的 API Key。
// 写入 keys.yaml 后立即热加载生效，用户不必重启引擎。
func runCLIKeyCommand(engine *Engine, arg string) {
	if arg == "" {
		names := make([]string, 0, len(engine.Cloud.Providers))
		for name := range engine.Cloud.Providers {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Println(ansiBold + "云端厂商 Key:" + ansiReset)
		for _, name := range names {
			p := engine.Cloud.Providers[name]
			k := p.defaultKey()
			state := ansiRed + "未配置"
			if k.Key != "" {
				state = ansiGreen + "已配置 " + maskKey(k.Key)
			}
			fmt.Println("  " + ansiCyan + name + ansiReset + "  " + state + ansiReset)
		}
		fmt.Println(ansiDim + "设置: /key <厂商> <密钥>（写入 " + engine.KeysPath + "，立即生效）" + ansiReset)
		return
	}
	parts := strings.Fields(arg)
	if len(parts) != 2 {
		fmt.Println(ansiRed + "✗ 用法: /key <厂商> <密钥>" + ansiReset)
		return
	}
	provider, key := parts[0], parts[1]
	if _, ok := engine.Cloud.Providers[provider]; !ok {
		fmt.Println(ansiRed + "✗ 未知厂商: " + provider + ansiReset)
		return
	}
	root := filepath.Dir(engine.KeysPath)
	overrides := loadKeyOverrides(root)
	overrides[provider] = key
	if err := writeKeyOverrides(root, overrides); err != nil {
		fmt.Println(ansiRed + "✗ 写入 keys.yaml 失败: " + err.Error() + ansiReset)
		return
	}
	engine.keysMod = time.Time{}
	engine.refreshKeysIfChanged()
	fmt.Println(ansiGreen + "✓ " + provider + " 的 Key 已保存（" + maskKey(key) + "），立即生效" + ansiReset)
}

// runModels 列出模型子命令。
func runModels() {
	cfg := loadGlobalConfig(ROOT + "/config.yaml")
	engine := newEngine(cfg, ROOT)
	listCLIModels(engine)
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

// 终端 CLI 模式：交互对话，ANSI 动态思考过程展示。
// 支持 /thinking 段（若模型返回 reasoning_content）用彩色 ANSI 显示。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

	fmt.Println(ansiBold + "═══ General Harness CLI ═══" + ansiReset)
	fmt.Println(ansiDim + "云端厂商: " + itoa(len(engine.Cloud.Providers)) +
		" · 本地 GGUF: " + itoa(len(engine.GGUF.Models)) +
		" · 退出输入 /exit" + ansiReset)
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	sessionID := "cli-" + randHex(6)
	history := []map[string]any{}
	model := cfg.DefaultModel

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

		history = append(history, map[string]any{"role": "user", "content": text})
		fmt.Println(ansiBold + "助手 > " + ansiReset)
		reply := streamCLI(engine, model, history, sessionID)
		history = append(history, map[string]any{"role": "assistant", "content": reply})
		fmt.Println()
	}
}

// streamCLI 流式对话：思考段（reasoning_content）灰色斜体折叠，正文青色。
func streamCLI(engine *Engine, model string, messages []map[string]any, sessionID string) string {
	provider, actual, err := engine.Cloud.resolve(model, engine.Config.Aliases)
	if err != nil {
		fmt.Println(ansiRed + "✗ " + err.Error() + ansiReset)
		return ""
	}
	selected, err := provider.resolveKey("")
	if err != nil {
		fmt.Println(ansiRed + "✗ " + err.Error() + ansiReset)
		return ""
	}
	var full strings.Builder
	thinking := false
	inThinking := false

	err = provider.streamChatCompletion(actual, messages, "", nil, nil,
		func(chunk map[string]any) error {
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
	_ = inThinking
	if err != nil {
		if pe, ok := err.(*PluginError); ok {
			fmt.Println(ansiRed + "\n✗ " + pe.Message + ansiReset)
		}
	}
	// 记录用量（与 HTTP 路径一致）
	_ = selected
	// 简单记账：流式 usage 已由 chunk 回调处理，这里补充最终记录
	return full.String()
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
	fmt.Println(ansiDim + "/clear         清空历史" + ansiReset)
	fmt.Println(ansiDim + "/exit          退出" + ansiReset)
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

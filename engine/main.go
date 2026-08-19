// General Harness Go 引擎：核心引擎 + 双模前端（CLI / HTTP API）。
// 单二进制，零第三方依赖（纯 Go 标准库）。
//
// 子命令：
//   gh serve             启动 HTTP API 服务（供 CLI/GUI 连接）
//   gh chat              终端 CLI 模式（交互对话，ANSI 思考展示）
//   gh models            列出可用模型
//   gh gguf <file>       解析 GGUF 文件元数据
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var ROOT string

func init() {
	// 优先使用工作目录（启动脚本 cd 到发行版根目录再运行）；
	// 若工作目录无 config.yaml，回退到可执行文件所在目录（或其上一级，
	// 因为发行版布局为 <root>/bin/gh.exe，config.yaml 在 bin 的上一级）。
	cwd, err := os.Getwd()
	if err == nil {
		if _, statErr := os.Stat(filepath.Join(cwd, "config.yaml")); statErr == nil {
			ROOT = cwd
			return
		}
	}
	exe, err := os.Executable()
	if err != nil {
		ROOT = "."
		return
	}
	dir := filepath.Dir(exe)
	if _, statErr := os.Stat(filepath.Join(dir, "config.yaml")); statErr != nil {
		parent := filepath.Dir(dir)
		if _, statErr2 := os.Stat(filepath.Join(parent, "config.yaml")); statErr2 == nil {
			dir = parent
		}
	}
	ROOT = dir
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		runCLI()
		return
	}
	switch args[0] {
	case "serve", "server", "-s", "--serve":
		runServer(args[1:])
	case "chat", "cli", "-c", "--cli":
		runCLI()
	case "models", "-m", "--models":
		runModels()
	case "gguf":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: gh gguf <model.gguf>")
			os.Exit(2)
		}
		runGGUF(args[1])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", args[0])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`General Harness 引擎（Go 单二进制）
用法:
  gh serve [--host H] [--port P]   启动 HTTP API 服务
  gh chat                           终端 CLI 模式（默认）
  gh models                         列出可用模型
  gh gguf <file.gguf>               解析 GGUF 文件元数据
  gh help                           显示帮助

配置: config.yaml（同目录）+ plugins/*/config.yaml
本地模型: models/*.gguf`)
}

# General Harness — 极简 AI 网关（核心引擎 + 双模前端）

> 一个解压即用的本地 AI 网关：**Go 单二进制引擎**（云端 API 代理 + Ollama
> 协议兼容 + GGUF 解析），配 **终端 CLI** 与 **Webview GUI** 两种界面。
> 核心逻辑与 UI 完全解耦——只跑后端用 CLI，前后端一起用图形界面。

## ✨ 功能亮点

- **🖥️ 双模前端，一次开发多端衔接**
  - **终端 CLI**：ANSI 彩色流式输出，`/model` 切换、`/models` 列表、思考过程动态展示
  - **Webview GUI**：复用系统原生渲染引擎（Windows Edge WebView2），可折叠「💭 思考过程」面板、模型下拉、流式打字机效果
- **☁️ 云端 API 代理**：OpenAI 兼容厂商即插即用（DeepSeek / Kimi / Qwen / OpenAI），多 Key 轮换，自动重试与错误映射
- **🦙 Ollama 协议兼容**：`/api/tags`、`/api/chat`、`/api/generate`——Open WebUI 等 Ollama 生态工具可直接对接
- **📦 GGUF 解析**：读取本地 `.gguf` 模型的架构 / 上下文长度 / 词表 / 参数量元数据
- **📊 用量统计**：时间维度（10min~半年/custom）+ 对话维度（最近 N 次）双看板
- **🔒 安全**：可选网关鉴权、单 IP 限流、CORS 放行本机来源、Key 支持环境变量注入

## 🚀 30 秒快速开始

```bash
# 1) 解压后进入目录

# 2) 终端 CLI 模式（自动拉起引擎）
python start.py

# 3) Webview GUI 模式
python start.py --gui

# 4) 仅引擎服务（供 Open WebUI 等对接）
python start.py --server
```

> 引擎地址默认 `http://127.0.0.1:8000`，可用 `--port` 覆盖。
> 不想用 Python？直接运行 `bin\gh_upx.exe serve`（单文件、零依赖）即可。

### 配置云端 API Key（可选，不配也能玩本地 GGUF）

编辑 `plugins/<厂商>/config.yaml`，任选一种：

```yaml
# 方式 A：直接填
api_key: sk-xxxxx
# 方式 B：环境变量（推荐，避免明文落盘）
api_key: ${DEEPSEEK_API_KEY}
```

### 安装依赖（仅 GUI 需要）

```bash
pip install -r gui/requirements.txt   # pywebview；不装也能用，GUI 会自动回退系统浏览器
```

## 🧩 项目结构

```
general-harness/
├── bin/gh.exe / gh_upx.exe   Go 引擎 + CLI（单二进制，零第三方依赖）
├── engine/                   Go 引擎源码（手写 HTTP 层，无第三方依赖）
├── gui/gui.py                Webview GUI（pywebview → 回退浏览器）
├── models/                   本地 GGUF 模型目录（*.gguf）
├── plugins/                  云端厂商配置（deepseek/kimi/openai/qwen）
├── config.yaml               全局配置
├── start.py                  统一启动器
└── tests/                    测试
```

## 🌐 标准 HTTP API

| 端点 | 说明 |
| --- | --- |
| `GET /health` | 健康检查 |
| `POST /v1/chat/completions` | 统一对话（OpenAI 风格，stream/非 stream，含 thinking） |
| `GET /v1/models` | 模型列表（云端 + 本地 GGUF） |
| `GET /v1/usage/stats` | 用量统计（`time_range` 时间维度 / `limit` 对话维度） |
| `GET /api/tags` | Ollama 兼容：模型列表 |
| `POST /api/chat` | Ollama 兼容：messages 对话 |
| `POST /api/generate` | Ollama 兼容：prompt 生成 |
| `GET /api/gguf/<name>` | 本地 GGUF 模型元数据 |

## 🔨 从源码构建引擎

```bash
cd engine
go build -ldflags "-s -w -buildid=" -trimpath -o ../bin/gh.exe .
# 可选：UPX 极限压缩
upx --lzma -9 -o ../bin/gh_upx.exe ../bin/gh.exe
```

## ✅ 测试

```bash
cd engine && go test ./...
python -m unittest discover -s tests
```

## 📏 体积说明

Go 运行时在 Windows 含网络场景的物理下限约 0.72MB（UPX 极限）；本项目采用
"仅 `net` 包手写 HTTP 层 + 零第三方依赖"路径：**引擎 strip 3.3MB，
UPX 压缩后 1.1MB**，单文件、解压即用。GUI 前端为单文件自包含 HTML（<10KB），
打包体积远小于 10MB 目标。

## 📄 License

All rights reserved unless otherwise noted.

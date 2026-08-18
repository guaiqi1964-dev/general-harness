# General Harness — 极简 AI 网关发行版

> **核心引擎 + 双模前端**：一个 Go 单二进制引擎，统一封装**云端 API 代理**、
> **Ollama 协议兼容**与 **GGUF 文件解析**能力，暴露标准 HTTP API；前端提供
> **纯终端 CLI**（ANSI 动态思考展示）与 **Webview GUI**（可折叠 Thinking 界面）
> 两种交互模式。核心逻辑与 UI 完全解耦——**只启动后端用 CLI，前后端一起用 GUI**。

- **引擎**：`bin/gh_upx.exe`（1.1MB）/ `bin/gh.exe`（3.3MB），单文件、零第三方依赖、解压即用
- **引擎源码**：`engine/`（纯 Go 标准库，手写 HTTP 层，无任何外部模块依赖）
- **GUI**：`gui/gui.py`（Python，pywebview → 系统原生 Edge WebView2；不可用时自动回退系统浏览器）

---

## 📋 目录

- [功能总览](#功能总览)
- [快速开始](#快速开始)
  - [安装方式（安装包 / 绿色压缩包）](#快速开始)
  - [一键启动（.bat 脚本）](#快速开始)
  - [命令行快速开始](#快速开始)
- [三种启动模式](#三种启动模式)
- [终端 CLI 模式（完整命令参考）](#终端-cli-模式)
- [Webview GUI 模式](#webview-gui-模式)
- [标准 HTTP API 参考](#标准-http-api-参考)
  - [统一对话接口 /v1/chat/completions](#v1chatcompletions-统一对话)
  - [模型列表 /v1/models](#v1models-模型列表)
  - [用量统计 /v1/usage/stats](#v1usagestats-用量统计)
  - [Ollama 兼容接口](#ollama-兼容接口)
  - [GGUF 元数据接口](#gguf-元数据接口)
  - [健康检查](#健康检查)
- [配置说明](#配置说明)
  - [config.yaml 全局配置](#configyaml-全局配置)
  - [云端厂商配置 plugins/](#云端厂商配置-plugins)
- [本地 GGUF 模型](#本地-gguf-模型)
- [用量统计与数据文件](#用量统计与数据文件)
- [安全机制](#安全机制)
- [目录结构](#目录结构)
- [从源码构建](#从源码构建)
- [运行测试](#运行测试)
- [体积设计说明](#体积设计说明)
- [常见问题 FAQ](#常见问题-faq)

---

## 功能总览

| 能力 | 说明 |
| --- | --- |
| 🖥️ **双模前端** | 终端 CLI（ANSI 彩色流式输出）+ Webview GUI（原生渲染引擎，可折叠思考面板），核心与 UI 解耦 |
| ☁️ **云端 API 代理** | OpenAI 兼容厂商即插即用：DeepSeek / Kimi / Qwen / OpenAI，支持多 Key 轮换与选择 |
| 🦙 **Ollama 协议兼容** | `/api/tags`、`/api/chat`、`/api/generate`，Open WebUI 等 Ollama 生态工具可直接对接 |
| 📦 **GGUF 解析** | 读取本地 `.gguf` 模型元数据：架构 / 版本 / 张量数 / 上下文长度 / 词表 / 文件类型 |
| 📊 **用量统计** | 时间维度（10min ~ 半年 / 自定义区间）+ 对话维度（最近 N 次），JSON 原子持久化 |
| 🔄 **自动重试** | 429 / 5xx 与网络错误指数退避重试（最多 3 次），上游错误统一映射 |
| 🔒 **安全** | 可选网关鉴权（Bearer Token）、单 IP 滑动窗口限流、CORS 放行、Key 支持环境变量注入 |
| ⚡ **极致轻量** | 引擎单二进制 1.1MB（UPX）、零运行时依赖；手写 HTTP 层 + 手写 YAML 子集解析 |

---

## 快速开始

### 安装方式（二选一）

**方式 A：Windows 安装包（推荐）** — 下载 `General-Harness-Setup-v0.1.100.exe`，双击运行，按向导选择自定义安装位置（默认 `%ProgramFiles%\General Harness`），安装完成后自动创建桌面与开始菜单快捷方式。安装包已内置全部运行所需文件（引擎二进制、配置、GUI、示例模型），并会在安装时检测/安装 Python 依赖。

**方式 B：绿色压缩包（免安装）** — 下载 `General-Harness-v0.1.100.zip`，解压到任意目录即可使用（引擎为单文件零依赖，双击 `start.bat` 或直接运行 `bin\gh_upx.exe` 即可，无需安装）。

### 一键启动（.bat 脚本，Windows）

安装包或压缩包内均自带三个管理脚本，**双击即可**：

```bat
start.bat              :: 一键启动 + 进入终端 CLI
start.bat gui          :: 一键启动 + Webview GUI 图形界面
start.bat server       :: 一键启动 + 仅 HTTP 服务模式
restart.bat            :: 一键重启引擎（自动停止旧进程 → 启动新进程）
restart.bat gui        :: 一键重启 + GUI
stop.bat               :: 一键停止引擎
```

> - 脚本自动选择引擎（优先 `gh_upx.exe`，回退 `gh.exe`）
> - 启动前自动探测端口健康状态：引擎已在运行则复用，不会重复拉起
> - 重启/停止按 `gateway.pid` 精确杀进程，并用 `netstat` 按端口兜底
> - 等引擎就绪（轮询 `/health`，最长 30 秒）后才进入前端
> - GUI 模式自动检测 Python，缺失时给出明确提示

### 命令行快速开始

```bash
# 终端 CLI 模式（推荐先试这个）
python start.py

# Webview GUI 模式
python start.py --gui

# 仅引擎服务（HTTP API，供 Open WebUI / 自写客户端对接）
python start.py --server
```

> 引擎地址默认 `http://127.0.0.1:8000`，可用 `--port <端口>` 覆盖。
> **不想用 Python？** 直接运行 `bin\gh_upx.exe serve` 即可（零依赖，连 Python 都不需要）。

### 最小验证

```bash
# 1) 启动引擎
bin\gh_upx.exe serve --port 8000

# 2) 另一个终端验证健康检查
curl http://127.0.0.1:8000/health
# → {"status":"ok"}

# 3) 查看模型列表（含预置的本地 GGUF 示例）
curl http://127.0.0.1:8000/v1/models
```

---

## 三种启动模式

| 命令 | 模式 | 说明 |
| --- | --- | --- |
| `python start.py` | CLI | 自动拉起引擎 → 进入终端对话，Ctrl+C 退出后自动关闭引擎 |
| `python start.py --gui` | GUI | 自动拉起引擎 → 弹出原生 Webview 窗口 |
| `python start.py --server` | 服务 | 只启动引擎 HTTP API，常驻后台 |
| `bin\gh_upx.exe serve` | 服务 | 同上，不依赖 Python |
| `bin\gh_upx.exe`（无参数） | CLI | 直接进入终端模式（内嵌引擎，不需要单独的服务） |

`start.py` 会先探测 `http://127.0.0.1:<port>/health`：引擎已在运行则**复用**，否则自动启动，退出时自动清理引擎进程。

---

## 终端 CLI 模式

引擎内置终端交互模式（`gh chat` 或直接运行 `bin\gh_upx.exe`）：

```
═══ General Harness CLI ═══
云端厂商: 4 · 本地 GGUF: 1 · 退出输入 /exit

你 > 你好，介绍一下自己
助手 > …（ANSI 绿色流式输出）
```

### 内置命令

| 命令 | 说明 |
| --- | --- |
| `/model <名称>` | 切换模型（如 `/model qwen/qwen-plus`、`/model local/demo-qwen2`） |
| `/models` | 列出全部云端与本地模型（ANSI 彩色） |
| `/clear` | 清空当前会话历史 |
| `/help` | 显示命令帮助 |
| `/exit` / `/quit` | 退出 CLI |

### 引擎子命令（`bin\gh_upx.exe <子命令>`）

| 子命令 | 说明 |
| --- | --- |
| `serve [--host H] [--port P]` | 启动 HTTP API 服务（默认 127.0.0.1:8000） |
| `chat` / `cli` | 终端交互模式 |
| `models` | 打印模型列表后退出 |
| `gguf <file.gguf>` | 解析单个 GGUF 文件并打印 JSON 元数据 |
| `help` | 显示帮助 |

---

## Webview GUI 模式

```bash
python start.py --gui
# 或：python gui/gui.py --url http://127.0.0.1:8000
```

- **渲染引擎**：优先使用 `pywebview`（Windows 复用系统原生 Edge WebView2）；若未安装 pywebview，**自动回退到系统默认浏览器**打开。
- **界面能力**：
  - 模型下拉框（从 `/api/models` 动态加载云端 + 本地模型）
  - 流式打字机输出（SSE）
  - **可折叠「💭 思考过程」面板**（`<details>`，点击展开/收起，模型返回 `reasoning_content` 时显示）
  - Enter 发送 / Shift+Enter 换行、清空对话、状态栏提示
- **架构**：`gui.py` 内置一个本地代理（默认 127.0.0.1:8765），把浏览器的 `/v1/*`、`/api/*` 请求转发到 Go 引擎，避免 CORS 限制；前端为**单文件自包含 HTML**（无任何外部资源，<10KB）。
- **依赖**（可选）：`pip install -r gui/requirements.txt`（pywebview>=5.0）。不安装也可用（回退浏览器）。

---

## 标准 HTTP API 参考

所有端点（除 `/health`、`/`）受**限流**与**可选鉴权**控制；启用 `gateway_api_key` 后需携带 `Authorization: Bearer <key>`。CORS 已放行（`*`），支持 `OPTIONS` 预检。

### /v1/chat/completions 统一对话

OpenAI 风格补全接口，支持流式与非流式；统一响应字段：`id / content / usage / finish_reason / model`，流式块额外携带 `thinking`（来自上游 `reasoning_content`）。

**请求** `POST /v1/chat/completions`

```json
{
  "model": "deepseek/deepseek-chat",
  "messages": [{"role": "user", "content": "你好"}],
  "stream": false,
  "temperature": 0.7,
  "max_tokens": 1024,
  "top_p": 0.9,
  "session_id": "optional-conversation-id",
  "stream_options": {"include_usage": true}
}
```

| 字段 | 说明 |
| --- | --- |
| `model` | 模型名：支持 `厂商/模型`、裸模型名、`config.yaml` 别名、`local/<gguf名>` |
| `messages` | 消息数组（role: system/user/assistant；content 支持文本或多模态 parts） |
| `stream` | `true` 时返回 SSE（每块 `data: {...}`，结束 `data: [DONE]`） |
| `temperature` / `max_tokens` / `top_p` | 采样参数（可选） |
| `session_id` | 会话 ID，用于用量统计关联同一轮对话（可选） |
| `stream_options.include_usage` | 请求流式 usage（透传到上游） |

**请求头**：`X-Gateway-Api-Key: <Key名称或索引>` 可选，用于多 Key 选择。

**响应（非流式）**：

```json
{
  "id": "chatcmpl-xxx",
  "content": "你好！有什么可以帮你？",
  "usage": {"prompt_tokens": 12, "completion_tokens": 24, "total_tokens": 36},
  "finish_reason": "stop",
  "model": "deepseek-chat"
}
```

**响应（流式 SSE）**：

```
data: {"id":"s1","content":"你","thinking":null}
data: {"id":"s1","content":"好","thinking":"（思考中…）"}
data: {"id":"s1","usage":{"total_tokens":36},"finish_reason":"stop"}
data: [DONE]
```

### /v1/models 模型列表

`GET /v1/models` → `{"object":"list","data":[{id, object, owned_by, api_keys, vision}]}`

- 云端模型格式：`厂商/模型`（如 `deepseek/deepseek-chat`）
- 本地 GGUF 模型格式：`local/<模型名>`（如 `local/demo-qwen2`）
- `api_keys`：该厂商可用的 Key 名称与是否已配置
- `vision`：是否声明支持图片输入

### /v1/usage/stats 用量统计

双维度聚合查询，返回 `{"mode": "time"|"conversation", "data": [...]}`。

**维度一：时间聚合** — `?time_range=<粒度>`

| 粒度 | 含义 | 分桶 |
| --- | --- | --- |
| `10min` | 最近 10 分钟 | 1 分钟 |
| `0.5h` | 最近 30 分钟 | 1 分钟 |
| `1h` | 最近 1 小时 | 5 分钟 |
| `2h` | 最近 2 小时 | 10 分钟 |
| `5h` | 最近 5 小时 | 30 分钟 |
| `10h` | 最近 10 小时 | 1 小时 |
| `1d` | 最近 1 天 | 1 小时 |
| `7d` | 最近 7 天 | 6 小时 |
| `30d` | 最近 30 天 | 1 天 |
| `0.5y` | 最近半年 | 1 周 |
| `total` | 全部时间 | 按数据跨度动态 |
| `custom` | 自定义区间 | 需同时提供 `start_ts` 与 `end_ts`（Unix 秒） |

示例：`/v1/usage/stats?time_range=1d`、`/v1/usage/stats?time_range=custom&start_ts=1700000000&end_ts=1700086400`

**维度二：对话聚合** — `?limit=<N|total>`

按会话最后活跃时间倒序，取最近 N 次对话的 Token 总和。`limit` 支持 `1 / 10 / 50 / 100 / 200 / 500 / total` 或任意正整数（1~100000）。

### Ollama 兼容接口

| 端点 | 对应 Ollama | 说明 |
| --- | --- | --- |
| `GET /api/tags` | `GET /api/tags` | 列出全部模型（本地 GGUF + 云端） |
| `POST /api/chat` | `POST /api/chat` | messages 对话；`{"model","messages","stream","options"}`，支持流式 |
| `POST /api/generate` | `POST /api/generate` | prompt 生成；`{"model","prompt","stream","options"}` |

- `options.temperature`、`options.num_predict` 会映射到云端采样参数
- 本地 GGUF 模型返回元数据摘要（本引擎仅解析，不推理）
- 流式输出为 NDJSON（每行一个 JSON 块，`{"message":{"content":...},"done":false}` … `{"done":true}`）

### GGUF 元数据接口

`GET /api/gguf/<模型名>` → 模型元数据 JSON：

```json
{
  "path": "models/demo-qwen2.gguf",
  "name": "demo-qwen2",
  "architecture": "qwen2",
  "version": 3,
  "tensor_count": 0,
  "file_type": 0,
  "context_length": 32768,
  "tokenizer_model": "",
  "vocab_size": 0,
  "file_size": 147
}
```

### 健康检查

`GET /health` → `{"status":"ok"}`（不受限流影响）

---

## 配置说明

### config.yaml 全局配置

```yaml
server:
  host: 127.0.0.1   # 默认仅监听本机；对外共享改 0.0.0.0 并开启 gateway_api_key
  port: 8000

# 网关鉴权（可选）：设置后所有业务端点需携带 Authorization: Bearer <值>
# gateway_api_key: ""

# 单 IP 每分钟最大请求数，0 表示不限流；/health 不受限
rate_limit_per_minute: 60

# 模型别名：把简短别名映射到 厂商/模型
aliases:
  fast: deepseek/deepseek-chat
  smart: qwen/qwen-plus
  long: kimi/moonshot-v1-8k

# 未指定 model 时使用的默认模型
default_model: deepseek/deepseek-chat
```

> 命令行 `serve --host H --port P` 可覆盖 `server` 段的 host/port。

### 云端厂商配置 plugins/

每个厂商一个目录（`plugins/<厂商>/config.yaml`），引擎启动时自动扫描加载：

```yaml
plugin: openai_compatible
base_url: https://api.deepseek.com/v1
# 方式 A：单 Key
api_key: sk-xxxxx
# 方式 B：多 Key（api_keys 优先）
api_keys:
  - name: default
    key: ${DEEPSEEK_API_KEY}     # 支持 ${环境变量名}，避免明文落盘
  - name: backup
    key: sk-yyyyyy
models:
  - deepseek-chat
  - deepseek-reasoner
vision_models: []               # 可选：声明支持图片的模型
```

**内置厂商**：`deepseek` / `kimi` / `openai` / `qwen`（均复用 `openai_compatible` 适配器）。**新增厂商只需三步**：建目录 → 写 config.yaml（plugin/base_url/key/models）→ 重启引擎。

**多 Key 选择**：请求头 `X-Gateway-Api-Key: <名称或索引>` 指定使用哪把 Key，缺省用第一把非空 Key。

---

## 本地 GGUF 模型

1. 把任意 `.gguf` 文件放入 `models/` 目录（重启引擎后自动扫描）
2. 通过以下任一方式访问：
   - `gh gguf models/xxx.gguf`（命令行解析）
   - `GET /api/gguf/<名称>`
   - `GET /v1/models`（显示为 `local/<名称>`）
   - CLI/GUI 中直接选择 `local/<名称>` 对话（返回元数据摘要回复）

> 说明：本引擎提供 GGUF **解析能力**（架构、上下文、词表等元数据），不包含本地推理。如需本地推理请配合 llama.cpp 等推理后端。

---

## 用量统计与数据文件

- 每次成功的云端对话都会记录：`session_id`、`request_id`、`api_key_name`、`model_name`、`prompt_tokens`、`completion_tokens`、`total_tokens`、`timestamp`
- 数据持久化在引擎工作目录的 **`usage_stats.json`**（线程安全、原子写入：先写临时文件再 rename）
- 查询接口：`/v1/usage/stats`（详见上文）
- 运行时还会生成 `gateway.pid`（引擎 PID，供脚本识别）

---

## 安全机制

1. **网关鉴权**：`config.yaml` 设置 `gateway_api_key` 后，`/v1/chat/completions`、`/v1/models`、`/v1/usage/stats`、`/api/*` 全部要求 `Authorization: Bearer <key>`；未设置则放行（默认本机使用）。
2. **限流**：单 IP 滑动窗口限流（`rate_limit_per_minute`），超限返回统一 `429 rate_limit_error`；`/health`、`/` 豁免。
3. **CORS**：允许任意来源（`*`）、GET/POST/OPTIONS，方便本地 Web 工具对接。
4. **Key 保护**：支持 `${ENV_VAR}` 环境变量引用，避免 API Key 明文落盘。
5. **错误不泄露**：上游错误统一映射为 `{error:{message,type,code}}`，不会把内部堆栈透出。

---

## 目录结构

```
General_Harness_Release/
├── bin/
│   ├── gh_upx.exe           # 引擎（UPX 压缩版，1.1MB，推荐）
│   └── gh.exe               # 引擎（strip 版，3.3MB）
├── engine/                  # Go 引擎源码（零第三方依赖）
│   ├── main.go              # 入口与子命令分发
│   ├── server.go            # HTTP 路由与全部处理器
│   ├── http.go              # 手写 HTTP/1.1 服务器（仅 net 包）
│   ├── cloud.go             # 云端代理（重试/错误映射/SSE 解析）
│   ├── ollama.go            # Ollama 协议兼容层
│   ├── gguf.go              # GGUF 二进制解析器
│   ├── usage.go             # 用量统计（JSON 持久化）
│   ├── config.go            # 配置加载与厂商注册
│   ├── yaml.go              # 手写 YAML 子集解析器
│   ├── cli.go               # 终端交互模式
│   ├── util.go              # 工具函数与限流器
│   ├── engine_test.go       # 单元测试
│   ├── integration_test.go  # HTTP 集成测试
│   └── go.mod               # 模块定义（无外部依赖）
├── gui/
│   ├── gui.py               # Webview GUI（pywebview/浏览器回退）
│   └── requirements.txt     # GUI 依赖（pywebview>=5.0，可选）
├── models/
│   └── demo-qwen2.gguf      # 示例 GGUF（演示用）
├── plugins/
│   ├── deepseek/config.yaml
│   ├── kimi/config.yaml
│   ├── openai/config.yaml
│   ├── qwen/config.yaml
│   └── openai_compatible/plugin.py   # 兼容说明（引擎为内嵌实现，此文件供参考）
├── cli/README.md            # CLI 模式说明
├── config.yaml              # 全局配置
├── start.py                 # 统一启动器
├── tests/test_gui.py        # GUI/代理测试
└── README.md                # 本文档
```

---

## 从源码构建

前置：Go 1.21+（Windows）。

```bash
cd engine
go build -ldflags "-s -w -buildid=" -trimpath -o ../bin/gh.exe .

# 可选：UPX 极限压缩（约 3 倍缩小；需安装 UPX）
upx --lzma -9 -o ../bin/gh_upx.exe ../bin/gh.exe
```

引擎源码**零第三方依赖**（纯标准库）：手写 HTTP/1.1 服务器（`http.go`）、手写 YAML 子集解析器（`yaml.go`）、手写 JSON 处理均基于标准库。

---

## 运行测试

```bash
# Go 引擎测试（单元 + HTTP 集成）
cd engine && go test ./...

# Python 组件测试（GUI 代理、前端页面、启动器逻辑）
python -m unittest discover -s tests
```

测试覆盖：YAML 解析、GGUF 解析（含非法魔数）、用量统计（时间/对话/自定义/持久化/并发）、Key 解析、厂商注册、模型路由、限流、鉴权、Ollama 三端点、流式/非流式记账、GUI 代理转发与前端单文件完整性。

---

## 体积设计说明

| 构建 | 体积 |
| --- | --- |
| 引擎 strip（`-s -w -buildid=` + trimpath） | 3.3 MB |
| 引擎 UPX 压缩（`--lzma -9`） | **1.1 MB** |
| GUI 前端单文件 HTML | <10 KB |

设计路径：仅 `net` 包手写 HTTP 层（避开 4MB 的 `net/http`）+ 零第三方依赖 + UPX。Windows 上含网络的 Go 二进制物理下限约 0.72MB（纯 `net` 包 + UPX 极限），本引擎 1.1MB 已非常接近。引擎单文件、零运行时依赖、解压即用。

---

## 常见问题 FAQ

**Q1：没有 Python 能运行吗？**
能。引擎是独立二进制：`bin\gh_upx.exe serve`（服务）或直接运行进入 CLI。Python 只用于 GUI 模式与 `start.py` 启动器。

**Q2：不填 API Key 能玩吗？**
能。不配置 Key 时仍可：列出模型、解析 GGUF、通过 `local/demo-qwen2` 对话（返回模型元数据摘要）。云端真实对话需在 `plugins/*/config.yaml` 配置有效 Key。

**Q3：如何接入 Open WebUI？**
把本引擎地址填为 Ollama 兼容端点（`http://127.0.0.1:8000`），Open WebUI 通过 `/api/tags`、`/api/chat` 即可发现并使用模型。

**Q4：流式输出和普通输出有什么区别？**
流式（`stream: true`）返回 SSE，逐块推送 `content`，结束 `[DONE]`；思考类模型额外返回 `thinking`（`reasoning_content`），GUI 与 CLI 都会动态展示。

**Q5：GGUF 文件为什么对话只返回元数据？**
本发行版的引擎定位是"解析 + 网关"：GGUF 提供元数据解析能力，不含本地推理。需要本地推理请接 llama.cpp 等后端，或通过云端模型对话。

**Q6：修改配置后需要重启吗？**
是的。`config.yaml`、`plugins/*/config.yaml`、`models/*.gguf` 均在引擎启动时加载/扫描，改动后重启引擎生效。

**Q7：usage_stats.json 会无限增长吗？**
目前为追加式存储，未设置自动清理上限。如需长期运行建议定期备份/清理该文件（删除后引擎会自动重建空库）。

**Q8：为什么有两个 gh.exe？**
`gh_upx.exe` 是 UPX 压缩版（更小，运行时自动解压到内存），`gh.exe` 是常规 strip 版（启动略快）。功能完全一致，推荐用 `gh_upx.exe`。

**Q9：端口被占用怎么办？**
`start.py --port <新端口>` 或 `bin\gh_upx.exe serve --port <新端口>`；GUI 可用 `python gui/gui.py --url http://127.0.0.1:<新端口>` 指定。

---

*General Harness — 极简 AI 网关。核心引擎与 UI 完全解耦，一次开发、多端无缝衔接。*

# General Harness — 极简 AI 网关

> **English**: [README.en.md](./README.en.md) | **简体中文**: 本文档

> 一个开箱即用的本地 AI 网关：**Go 单二进制引擎**统一提供云端 API 代理、
> Ollama 协议兼容与 GGUF 文件解析能力，并通过标准 HTTP API 对外服务。
> 前端支持**终端 CLI**（ANSI 动态思考展示）与 **Webview GUI**（可折叠
> Thinking 界面）两种交互模式。核心引擎与界面完全解耦：只启动后端时使用
> CLI，前后端一起运行时使用 GUI。

- **引擎**：`bin/gh_upx.exe`（1.1 MB）/ `bin/gh.exe`（3.3 MB）——单文件、零第三方依赖、解压即用
- **引擎源码**：`engine/`——纯 Go 标准库实现（含手写 HTTP 层），无任何外部模块
- **GUI**：`gui/gui.py`——基于 pywebview 调用系统原生 Edge WebView2，不可用时自动回退系统浏览器

---

## 📋 目录

- [功能总览](#功能总览)
- [安装与快速开始](#安装与快速开始)
  - [方式一：Windows 安装包](#方式一windows-安装包)
  - [方式二：绿色压缩包](#方式二绿色压缩包)
  - [一键启动脚本](#一键启动脚本)
  - [命令行启动](#命令行启动)
- [三种启动模式](#三种启动模式)
- [终端 CLI 模式](#终端-cli-模式)
- [Webview GUI 模式](#webview-gui-模式)
- [标准 HTTP API 参考](#标准-http-api-参考)
  - [统一对话 /v1/chat/completions](#统一对话-v1chatcompletions)
  - [模型列表 /v1/models](#模型列表-v1models)
  - [用量统计 /v1/usage/stats](#用量统计-v1usagestats)
  - [Ollama 兼容接口](#ollama-兼容接口)
  - [GGUF 元数据接口](#gguf-元数据接口)
- [配置说明](#配置说明)
  - [全局配置 config.yaml](#全局配置-configyaml)
  - [云端厂商配置 plugins](#云端厂商配置-plugins)
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
| 🖥️ **双模前端** | 终端 CLI（ANSI 彩色流式输出）与 Webview GUI（原生渲染引擎、可折叠思考面板），核心与界面解耦 |
| ☁️ **云端 API 代理** | 兼容 OpenAI 协议的厂商即插即用：DeepSeek / Kimi / Qwen / OpenAI，支持多 Key 配置与按需选择 |
| 🦙 **Ollama 协议兼容** | 提供 `/api/tags`、`/api/chat`、`/api/generate` 端点，Open WebUI 等 Ollama 生态工具可直接对接 |
| 📦 **GGUF 解析** | 读取本地 `.gguf` 模型元数据：架构、版本、张量数、上下文长度、词表、文件类型 |
| 📊 **用量统计** | 时间维度（10 分钟至半年或自定义区间）与对话维度（最近 N 次）双聚合，JSON 原子持久化 |
| 🔄 **自动重试** | 对 429、5xx 及网络错误进行指数退避重试（最多 3 次），上游错误统一映射 |
| 🔒 **安全机制** | 可选网关鉴权（Bearer Token）、单 IP 滑动窗口限流、CORS 放行、Key 支持环境变量注入 |
| ⚡ **极致轻量** | 引擎单二进制 1.1 MB（UPX 压缩），零运行时依赖；手写 HTTP 层与 YAML 子集解析器 |

---

## 安装与快速开始

### 方式一：Windows 安装包

下载 `General-Harness-Setup-v0.1.100.exe`，双击运行并按向导操作：

- 可选择自定义安装位置（默认 `%ProgramFiles%\General Harness`）；
- 安装包内置全部运行所需文件（引擎二进制、配置、GUI、示例模型）；
- 安装时自动检测并安装 GUI 依赖（pywebview）；
- 安装完成后自动创建桌面与开始菜单快捷方式。

### 方式二：绿色压缩包

下载 `General-Harness-v0.1.100.zip`，解压到任意目录即可使用，无需安装。引擎为单文件零依赖，直接双击 `start.bat` 或运行 `bin\gh_upx.exe` 即可。

### 一键启动脚本

安装包与压缩包内均附带三个管理脚本，双击即可使用：

```bat
start.bat              :: 一键启动，进入终端 CLI
start.bat gui          :: 一键启动，打开 Webview GUI
start.bat server       :: 一键启动，仅运行 HTTP 服务
restart.bat            :: 一键重启引擎（自动停止旧进程并启动新进程）
restart.bat gui        :: 一键重启并打开 GUI
stop.bat               :: 一键停止引擎
```

脚本特性：

- 自动选择引擎（优先 `gh_upx.exe`，缺失时回退 `gh.exe`）；
- 启动前探测端口健康状态：引擎已在运行则直接复用，不会重复启动；
- 重启与停止优先按 `gateway.pid` 精确结束进程，并以 `netstat` 按端口兜底；
- 等待引擎就绪（轮询 `/health`，最长 30 秒）后才进入前端；
- GUI 模式自动检测 Python，缺失时给出明确提示。

### 命令行启动

```bash
# 终端 CLI 模式（推荐先尝试）
python start.py

# Webview GUI 模式
python start.py --gui

# 仅启动引擎服务（供 Open WebUI 等对接）
python start.py --server
```

> 引擎默认监听 `http://127.0.0.1:8000`，可用 `--port <端口>` 覆盖。
> 若不想使用 Python，可直接运行 `bin\gh_upx.exe serve`（零依赖）。

### 最小验证

```bash
# 1) 启动引擎
bin\gh_upx.exe serve --port 8000

# 2) 在另一个终端验证健康检查
curl http://127.0.0.1:8000/health
# → {"status":"ok"}

# 3) 查看模型列表（含预置的本地 GGUF 示例）
curl http://127.0.0.1:8000/v1/models
```

---

## 三种启动模式

| 命令 | 模式 | 说明 |
| --- | --- | --- |
| `python start.py` | CLI | 自动拉起引擎并进入终端对话，Ctrl+C 退出后自动关闭引擎 |
| `python start.py --gui` | GUI | 自动拉起引擎并弹出原生 Webview 窗口 |
| `python start.py --server` | 服务 | 仅启动引擎 HTTP API，常驻后台 |
| `bin\gh_upx.exe serve` | 服务 | 同上，不依赖 Python |
| `bin\gh_upx.exe`（无参数） | CLI | 直接进入终端模式（引擎内嵌，无需单独的服务） |

`start.py` 会先探测 `http://127.0.0.1:<port>/health`：若引擎已在运行则直接复用，否则自动启动；退出时自动清理引擎进程。

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
| `gguf <file.gguf>` | 解析指定 GGUF 文件并打印 JSON 元数据 |
| `help` | 显示帮助 |

---

## Webview GUI 模式

```bash
python start.py --gui
# 或：python gui/gui.py --url http://127.0.0.1:8000
```

- **渲染引擎**：优先使用 pywebview（Windows 上复用系统原生 Edge WebView2）；未安装 pywebview 时自动回退到系统默认浏览器。
- **界面能力**：
  - 模型下拉框（从 `/api/models` 动态加载云端与本地模型）；
  - 流式打字机输出（SSE）；
  - 可折叠「💭 思考过程」面板（使用 `<details>`，点击展开或收起；模型返回 `reasoning_content` 时显示）；
  - Enter 发送、Shift+Enter 换行、清空对话、状态栏提示。
- **架构**：`gui.py` 内置本地代理（默认 127.0.0.1:8765），将浏览器的 `/v1/*`、`/api/*` 请求转发至 Go 引擎，规避 CORS 限制；前端为单文件自包含 HTML（无外部资源，小于 10 KB）。
- **依赖**（可选）：`pip install -r gui/requirements.txt`（pywebview>=5.0）。未安装时仍可使用浏览器回退模式。

---

## 标准 HTTP API 参考

除 `/health`、`/` 外，所有端点均受限流与可选鉴权控制；启用 `gateway_api_key` 后需携带 `Authorization: Bearer <key>`。CORS 已放行（`*`），支持 `OPTIONS` 预检。

### 统一对话 /v1/chat/completions

OpenAI 风格的补全接口，支持流式与非流式。统一响应字段为 `id / content / usage / finish_reason / model`；流式块额外携带 `thinking`（来自上游 `reasoning_content`）。

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
| `model` | 模型名，支持 `厂商/模型`、裸模型名、`config.yaml` 别名、`local/<gguf名>` |
| `messages` | 消息数组（role: system/user/assistant；content 支持文本或多模态 parts） |
| `stream` | 为 `true` 时返回 SSE（每块 `data: {...}`，结束于 `data: [DONE]`） |
| `temperature` / `max_tokens` / `top_p` | 采样参数（可选） |
| `session_id` | 会话 ID，用于用量统计中关联同一轮对话（可选） |
| `stream_options.include_usage` | 请求流式 usage（透传到上游） |

**请求头**：`X-Gateway-Api-Key: <Key名称或索引>`（可选），用于多 Key 选择。

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

### 模型列表 /v1/models

`GET /v1/models` → `{"object":"list","data":[{id, object, owned_by, api_keys, vision}]}`

- 云端模型格式：`厂商/模型`（如 `deepseek/deepseek-chat`）；
- 本地 GGUF 模型格式：`local/<模型名>`（如 `local/demo-qwen2`）；
- `api_keys`：该厂商可用的 Key 名称及其配置状态；
- `vision`：是否声明支持图片输入。

### 用量统计 /v1/usage/stats

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

按会话最后活跃时间倒序，统计最近 N 次对话的 Token 总和。`limit` 支持 `1 / 10 / 50 / 100 / 200 / 500 / total` 或任意正整数（1~100000）。

### Ollama 兼容接口

| 端点 | 对应 Ollama | 说明 |
| --- | --- | --- |
| `GET /api/tags` | `GET /api/tags` | 列出全部模型（本地 GGUF + 云端） |
| `POST /api/chat` | `POST /api/chat` | messages 对话；`{"model","messages","stream","options"}`，支持流式 |
| `POST /api/generate` | `POST /api/generate` | prompt 生成；`{"model","prompt","stream","options"}` |

- `options.temperature`、`options.num_predict` 会映射到云端采样参数；
- 本地 GGUF 模型返回元数据摘要（本引擎仅解析，不提供推理）；
- 流式输出为 NDJSON（每行一个 JSON 块，`{"message":{"content":...},"done":false}` … `{"done":true}`）。

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

### 全局配置 config.yaml

```yaml
server:
  host: 127.0.0.1   # 默认仅监听本机；对外共享改为 0.0.0.0 并开启 gateway_api_key
  port: 8000

# 网关鉴权（可选）：设置后所有业务端点需携带 Authorization: Bearer <值>
# gateway_api_key: ""

# 单 IP 每分钟最大请求数，0 表示不限流；/health 不受限
rate_limit_per_minute: 60

# 模型别名：将简短别名映射到 厂商/模型
aliases:
  fast: deepseek/deepseek-chat
  smart: qwen/qwen-plus
  long: kimi/moonshot-v1-8k

# 未指定 model 时使用的默认模型
default_model: deepseek/deepseek-chat
```

> 命令行 `serve --host H --port P` 可覆盖 `server` 段的 host/port。

### 云端厂商配置 plugins

每个厂商对应一个目录（`plugins/<厂商>/config.yaml`），引擎启动时自动扫描加载：

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

**内置厂商**：`deepseek` / `kimi` / `openai` / `qwen`（均复用 `openai_compatible` 适配器）。**新增厂商只需三步**：

1. 在 `plugins/` 下新建以厂商命名的目录；
2. 在该目录中创建 `config.yaml`（包含 plugin / base_url / key / models）；
3. 重启引擎。

**多 Key 选择**：通过请求头 `X-Gateway-Api-Key: <名称或索引>` 指定要使用的 Key，缺省使用第一把已配置的 Key。

---

## 本地 GGUF 模型

1. 将任意 `.gguf` 文件放入 `models/` 目录（重启引擎后自动扫描）；
2. 通过以下任一方式访问：
   - `gh gguf models/xxx.gguf`（命令行解析）；
   - `GET /api/gguf/<名称>`；
   - `GET /v1/models`（显示为 `local/<名称>`）；
   - 在 CLI / GUI 中直接选择 `local/<名称>` 对话（返回元数据摘要回复）。

> 说明：本引擎提供 GGUF **解析能力**（架构、上下文、词表等元数据），不包含本地推理。如需本地推理，请配合 llama.cpp 等推理后端使用。

---

## 用量统计与数据文件

- 每次成功的云端对话都会记录：`session_id`、`request_id`、`api_key_name`、`model_name`、`prompt_tokens`、`completion_tokens`、`total_tokens`、`timestamp`；
- 数据持久化在引擎工作目录的 `usage_stats.json` 中（线程安全、原子写入：先写临时文件再重命名）；
- 查询接口：`/v1/usage/stats`（详见上文）；
- 运行时还会生成 `gateway.pid`（引擎 PID，供脚本识别）。

---

## 安全机制

1. **网关鉴权**：在 `config.yaml` 中设置 `gateway_api_key` 后，`/v1/chat/completions`、`/v1/models`、`/v1/usage/stats` 及 `/api/*` 全部要求携带 `Authorization: Bearer <key>`；未设置时放行（默认供本机使用）。
2. **限流**：按单 IP 滑动窗口限流（`rate_limit_per_minute`），超限返回统一的 `429 rate_limit_error`；`/health`、`/` 豁免。
3. **CORS**：允许任意来源（`*`）与 GET/POST/OPTIONS，方便本地 Web 工具对接。
4. **Key 保护**：支持 `${ENV_VAR}` 环境变量引用，避免 API Key 明文落盘。
5. **错误不泄露**：上游错误统一映射为 `{error:{message,type,code}}`，不会透出内部堆栈。

---

## 目录结构

```
General_Harness_Release/
├── bin/
│   ├── gh_upx.exe           # 引擎（UPX 压缩版，1.1 MB，推荐）
│   └── gh.exe               # 引擎（strip 版，3.3 MB）
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
│   ├── gui.py               # Webview GUI（pywebview / 浏览器回退）
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
├── start.bat                # 一键启动脚本
├── restart.bat              # 一键重启脚本
├── stop.bat                 # 一键停止脚本
├── tests/test_gui.py        # GUI / 代理测试
└── README.md                # 本文档
```

---

## 从源码构建

前置要求：Go 1.21+（Windows）。

```bash
cd engine
go build -ldflags "-s -w -buildid=" -trimpath -o ../bin/gh.exe .

# 可选：UPX 极限压缩（约缩小 3 倍；需安装 UPX）
upx --lzma -9 -o ../bin/gh_upx.exe ../bin/gh.exe
```

引擎源码零第三方依赖（纯标准库）：手写 HTTP/1.1 服务器（`http.go`）、手写 YAML 子集解析器（`yaml.go`），JSON 处理同样基于标准库。

---

## 运行测试

```bash
# Go 引擎测试（单元 + HTTP 集成）
cd engine && go test ./...

# Python 组件测试（GUI 代理、前端页面、启动器逻辑）
python -m unittest discover -s tests
```

测试覆盖：YAML 解析、GGUF 解析（含非法魔数）、用量统计（时间 / 对话 / 自定义 / 持久化 / 并发）、Key 解析、厂商注册、模型路由、限流、鉴权、Ollama 三端点、流式与非流式记账、GUI 代理转发及前端单文件完整性。

---

## 体积设计说明

| 构建 | 体积 |
| --- | --- |
| 引擎 strip（`-s -w -buildid=` + trimpath） | 3.3 MB |
| 引擎 UPX 压缩（`--lzma -9`） | **1.1 MB** |
| GUI 前端单文件 HTML | <10 KB |

设计思路：仅使用 `net` 包手写 HTTP 层（避开约 4 MB 的 `net/http` 标准库）+ 零第三方依赖 + UPX 压缩。Windows 上含网络的 Go 二进制物理下限约为 0.72 MB（纯 `net` 包 + UPX 极限），本引擎的 1.1 MB 已非常接近。引擎单文件、零运行时依赖，解压即可运行。

---

## 常见问题 FAQ

### 🚀 安装与启动

<details>
<summary><b>没有 Python 能运行吗？</b></summary>

可以。引擎是独立的单文件二进制：

- `bin\gh_upx.exe serve` —— 启动 HTTP 服务；
- 直接运行 `bin\gh_upx.exe` —— 进入终端 CLI。

Python 仅在两处使用：`start.py` 启动器和 Webview GUI 模式。
</details>

<details>
<summary><b>安装包和绿色压缩包有什么区别？</b></summary>

两者包含完全相同的功能，区别在交付形态：

| 安装包（.exe） | 绿色压缩包（.zip） |
| --- | --- |
| 向导式安装，可选择安装位置 | 解压即用，无需安装 |
| 自动创建桌面与开始菜单快捷方式 | 不写注册表、不留系统目录 |
| 安装时自动配置 GUI 依赖 | 适合便携或免安装场景 |

正式使用推荐安装包，临时体验或随身携带推荐绿色包。
</details>

<details>
<summary><b>端口被占用怎么办？</b></summary>

指定新的端口即可：

```bash
python start.py --port 9000                 # 通过启动器
bin\gh_upx.exe serve --port 9000            # 直接启动引擎
python gui/gui.py --url http://127.0.0.1:9000   # GUI 连接新端口
```

> 修改 `config.yaml` 中 `server.port` 后，`start.py` 会自动读取新端口。
</details>

### ⚙️ 配置与模型

<details>
<summary><b>不填 API Key 能用吗？</b></summary>

可以。未配置 Key 时仍支持：列出模型、解析 GGUF、通过 `local/demo-qwen2` 对话（返回模型元数据摘要）。云端真实对话需要在 `plugins/*/config.yaml` 中配置有效 Key。
</details>

<details>
<summary><b>如何新增一个云端厂商或模型？</b></summary>

三步即可，无需改代码：

1. 在 `plugins/` 下新建以厂商命名的目录（如 `plugins/myvendor/`）；
2. 在该目录创建 `config.yaml`，包含 `plugin`、`base_url`、`api_key`、`models` 字段；
3. 重启引擎，新模型即可通过 `myvendor/<模型名>` 使用。

> 也可以直接往现有厂商的 `config.yaml` 的 `models` 列表追加新模型名。
</details>

<details>
<summary><b>修改配置后需要重启吗？</b></summary>

需要。`config.yaml`、`plugins/*/config.yaml`、`models/*.gguf` 均在引擎启动时加载或扫描，修改后重启引擎生效。使用 `restart.bat` 即可一键重启。
</details>

<details>
<summary><b>为什么有两个 gh.exe？</b></summary>

- `gh_upx.exe`：UPX 压缩版（约 1.1 MB），体积更小，运行时自动解压到内存；
- `gh.exe`：常规 strip 版（约 3.3 MB），启动略快。

两者功能完全一致，日常推荐使用 `gh_upx.exe`；所有脚本默认自动选择它，缺失时才回退到 `gh.exe`。
</details>

### 🔌 集成与对接

<details>
<summary><b>如何接入 Open WebUI？</b></summary>

将本引擎地址配置为 Ollama 兼容端点即可：

- 引擎地址：`http://127.0.0.1:8000`；
- Open WebUI 通过 `/api/tags`、`/api/chat` 自动发现并使用本引擎的模型。

也支持 curl 直接调用 Ollama 风格接口，例如：

```bash
curl http://127.0.0.1:8000/api/tags
```
</details>

<details>
<summary><b>流式输出与普通输出有什么区别？</b></summary>

- 普通输出：请求完成后一次性返回完整 JSON；
- 流式输出（`stream: true`）：以 SSE 逐块推送 `content`，以 `data: [DONE]` 结束，首字延迟更低、体验更流畅；
- 思考类模型在流式下额外返回 `thinking`（上游 `reasoning_content`），CLI 与 GUI 都会动态展示思考过程。
</details>

<details>
<summary><b>为什么 GGUF 模型对话只返回元数据？</b></summary>

本引擎的定位是"解析 + 网关"：GGUF 提供元数据解析能力（架构、上下文长度、词表等），但不包含本地推理。如需本地推理，请接入 llama.cpp 等推理后端，或使用云端模型对话。
</details>

<details>
<summary><b>如何让局域网内其他设备访问？</b></summary>

1. 将 `config.yaml` 中 `server.host` 改为 `0.0.0.0`；
2. 强烈建议同时设置 `gateway_api_key`，避免未授权调用消耗额度；
3. 其他设备通过 `http://<本机IP>:8000` 访问。

> 默认仅监听 `127.0.0.1`，这是刻意的安全设计。
</details>

### 🛠 故障排查

<details>
<summary><b>usage_stats.json 会无限增长吗？</b></summary>

当前为追加式存储，未设置自动清理上限。如需长期运行，建议定期备份或清理该文件——直接删除即可，引擎会自动重建空库，不影响其他功能。
</details>

<details>
<summary><b>GUI 窗口打不开怎么办？</b></summary>

按顺序排查：

1. 确认 Python 版本 ≥ 3.10（`python --version`）；
2. 确认引擎已就绪（`curl http://127.0.0.1:8000/health` 返回 `{"status":"ok"}`）；
3. 未安装 pywebview 时会自动回退系统浏览器打开，属正常降级；如需原生窗口可执行 `pip install -r gui/requirements.txt`；
4. 若仍无窗口，直接访问 `http://127.0.0.1:8765/index.html` 使用同一前端。
</details>

<details>
<summary><b>引擎启动后立刻退出怎么办？</b></summary>

通常是端口被占用或配置错误：

1. 查看 `gateway.pid` 对应进程是否已存在；
2. 用 `netstat -ano | findstr :8000` 检查端口占用；
3. 用 `stop.bat` 清理后重新 `start.bat`；仍失败时检查 `config.yaml` 与 `plugins/*/config.yaml` 语法（可用 `gh gguf` 无关，直接检查 YAML 缩进）。
</details>

---

*General Harness — 极简 AI 网关。核心引擎与界面完全解耦，可在不同终端无缝切换使用。*

# General Harness — Minimalist AI Gateway

> A ready-to-use local AI gateway: a **single Go binary engine** that unifies
> **cloud API proxying**, **Ollama protocol compatibility**, and **GGUF file
> parsing**, exposed through a standard HTTP API. Two frontends are provided —
> a **terminal CLI** (ANSI dynamic thinking display) and a **Webview GUI**
> (collapsible Thinking panel). The core engine and the UI are fully decoupled:
> use the CLI when only the backend is running, and the GUI when both run together.

- **Engine**: `bin/gh_upx.exe` (1.6 MB) / `bin/gh.exe` (5.2 MB) — single file, zero third-party dependencies, ready to run after extraction
- **Engine source**: `engine/` — pure Go standard library (including a hand-written HTTP layer), no external modules
- **GUI**: `gui/gui.py` — uses pywebview to call the native Edge WebView2; falls back to the system browser when unavailable

---

## 📋 Table of Contents

- [Feature Overview](#feature-overview)
- [Installation & Quick Start](#installation--quick-start)
  - [Option 1: Windows Installer](#option-1-windows-installer)
  - [Option 2: Portable ZIP](#option-2-portable-zip)
  - [One-Click Scripts](#one-click-scripts)
  - [Command Line Startup](#command-line-startup)
- [Three Startup Modes](#three-startup-modes)
- [Terminal CLI Mode](#terminal-cli-mode)
- [Webview GUI Mode](#webview-gui-mode)
- [HTTP API Reference](#http-api-reference)
  - [Unified Chat /v1/chat/completions](#unified-chat-v1chatcompletions)
  - [Model List /v1/models](#model-list-v1models)
  - [Usage Stats /v1/usage/stats](#usage-stats-v1usagestats)
  - [Ollama-Compatible Endpoints](#ollama-compatible-endpoints)
  - [GGUF Metadata Endpoint](#gguf-metadata-endpoint)
- [Configuration](#configuration)
  - [Global Config config.yaml](#global-config-configyaml)
  - [Cloud Provider Config plugins](#cloud-provider-config-plugins)
- [Local GGUF Models](#local-gguf-models)
- [Usage Stats & Data Files](#usage-stats--data-files)
- [Security](#security)
- [Directory Structure](#directory-structure)
- [Building from Source](#building-from-source)
- [Running Tests](#running-tests)
- [Size Design Notes](#size-design-notes)
- [FAQ](#faq)

---

## Feature Overview

| Capability | Description |
| --- | --- |
| 🖥️ **Dual-Mode Frontend** | Terminal CLI (ANSI color streaming) and Webview GUI (native rendering engine, collapsible thinking panel); core decoupled from UI |
| ☁️ **Cloud API Proxy** | Plug-and-play OpenAI-compatible providers: DeepSeek / Kimi / Qwen / OpenAI; multiple keys with on-demand selection |
| 🦙 **Ollama Protocol Compatible** | Provides `/api/tags`, `/api/chat`, `/api/generate`; Ollama ecosystem tools such as Open WebUI can connect directly |
| 📦 **GGUF Parsing** | Reads local `.gguf` model metadata: architecture, version, tensor count, context length, vocabulary, file type |
| 📊 **Usage Statistics** | Time dimension (10 minutes to half a year, or custom range) and conversation dimension (last N sessions); atomic JSON persistence |
| 🔄 **Automatic Retry** | Exponential backoff retry (up to 3 attempts) for 429, 5xx, and network errors; upstream errors mapped uniformly |
| 🔒 **Security** | Optional gateway auth (Bearer Token), per-IP sliding-window rate limiting, CORS allowed, keys support environment variable injection |
| ⚡ **Extremely Lightweight** | Single 1.6 MB binary (UPX-compressed), zero runtime dependencies; hand-written HTTP layer and YAML subset parser |

---

## Installation & Quick Start

### Option 1: Windows Installer

Download `General-Harness-Setup-v0.1.109.exe`, double-click and follow the wizard:

- Custom installation location (default: `%ProgramFiles%\General Harness`);
- The installer bundles everything needed (engine binary, config, GUI, sample models);
- Automatically detects and installs the GUI dependency (pywebview) during installation;
- Creates desktop and Start Menu shortcuts when finished.

### Option 2: Portable ZIP

Download `General-Harness-v0.1.109.zip` and extract it to any directory — no installation needed. The engine is a single zero-dependency file; double-click `start.bat` or run `bin\gh_upx.exe` directly.

### One-Click Scripts

Both the installer and the ZIP bundle three management scripts. Double-click to use:

```bat
start.bat              :: One-click start, open Webview GUI
start.bat cli          :: One-click start, enter terminal CLI
start.bat server       :: One-click start, HTTP server only
restart.bat            :: One-click restart and open GUI
restart.bat cli        :: One-click restart, enter CLI
stop.bat               :: One-click stop the engine
```

Script features:

- Automatically picks the engine (prefers `gh_upx.exe`, falls back to `gh.exe`);
- Probes port health before starting: reuses an already-running engine instead of starting a duplicate;
- Restart/stop kills the process via `gateway.pid` first, with a `netstat` port-based fallback;
- Waits for the engine to be ready (polling `/health`, up to 30 seconds) before launching the frontend;
- GUI mode checks for Python and gives a clear message if it is missing.

### Command Line Startup

```bash
# Terminal CLI mode (recommended to try first)
python start.py

# Webview GUI mode
python start.py --gui

# Engine server only (for Open WebUI, etc.)
python start.py --server
```

> The engine listens on `http://127.0.0.1:8000` by default; override with `--port <port>`.
> Don't want to use Python? Run `bin\gh_upx.exe serve` directly (zero dependencies).

### Minimal Verification

```bash
# 1) Start the engine
bin\gh_upx.exe serve --port 8000

# 2) Verify health check in another terminal
curl http://127.0.0.1:8000/health
# → {"status":"ok"}

# 3) List models (includes the bundled local GGUF sample)
curl http://127.0.0.1:8000/v1/models
```

---

## Three Startup Modes

| Command | Mode | Description |
| --- | --- | --- |
| `python start.py` | CLI | Auto-starts the engine and enters terminal chat; engine shuts down after Ctrl+C |
| `python start.py --gui` | GUI | Auto-starts the engine and opens a native Webview window |
| `python start.py --server` | Server | Starts only the engine HTTP API, stays in the background |
| `bin\gh_upx.exe serve` | Server | Same as above, no Python needed |
| `bin\gh_upx.exe` (no args) | CLI | Enters terminal mode directly (engine embedded, no separate server) |

`start.py` first probes `http://127.0.0.1:<port>/health`: it reuses an already-running engine, otherwise starts one; it cleans up the engine process on exit.

---

## Terminal CLI Mode

The engine has a built-in terminal interactive mode (`gh chat` or run `bin\gh_upx.exe` directly):

```
═══ General Harness CLI ═══
Cloud providers: 4 · Local GGUF: 1 · Type /exit to quit

You > Hello, introduce yourself
Assistant > …(ANSI green streaming output)
```

### Built-in Commands

| Command | Description |
| --- | --- |
| `/model <name>` | Switch model (e.g. `/model qwen/qwen-plus`, `/model local/demo-qwen2`) |
| `/models` | List all cloud and local models (ANSI colored) |
| `/clear` | Clear the current conversation history |
| `/help` | Show command help |
| `/exit` / `/quit` | Exit the CLI |

### Engine Subcommands (`bin\gh_upx.exe <subcommand>`)

| Subcommand | Description |
| --- | --- |
| `serve [--host H] [--port P]` | Start the HTTP API server (default 127.0.0.1:8000) |
| `chat` / `cli` | Terminal interactive mode |
| `models` | Print the model list and exit |
| `gguf <file.gguf>` | Parse a GGUF file and print JSON metadata |
| `help` | Show help |

---

## Webview GUI Mode

```bash
python start.py --gui
# or: python gui/gui.py --url http://127.0.0.1:8000
```

- **Rendering engine**: prefers pywebview (reuses the native Edge WebView2 on Windows); falls back to the system default browser when pywebview is not installed.
- **UI capabilities**:
  - Model dropdown (loads cloud and local models dynamically from `/api/models`);
  - Streaming typewriter output (SSE);
  - Collapsible "💭 Thinking" panel (using `<details>`, click to expand/collapse; shown when the model returns `reasoning_content`);
  - Enter to send, Shift+Enter for newline, clear conversation, status bar hints.
- **Architecture**: `gui.py` embeds a local proxy (default 127.0.0.1:8765) that forwards the browser's `/v1/*` and `/api/*` requests to the Go engine, avoiding CORS issues; the frontend is a single self-contained HTML file (no external resources, under 10 KB).
- **Dependencies** (optional): `pip install -r gui/requirements.txt` (pywebview>=5.0). The browser fallback still works without it.

---

## HTTP API Reference

All endpoints except `/health` and `/` are subject to rate limiting and optional auth; when `gateway_api_key` is enabled, requests must carry `Authorization: Bearer <key>`. CORS is allowed (`*`), and `OPTIONS` preflight is supported.

### Unified Chat /v1/chat/completions

An OpenAI-style completion endpoint supporting both streaming and non-streaming. Unified response fields: `id / content / usage / finish_reason / model`; streaming chunks additionally carry `thinking` (from the upstream `reasoning_content`).

**Request** `POST /v1/chat/completions`

```json
{
  "model": "deepseek/deepseek-chat",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": false,
  "temperature": 0.7,
  "max_tokens": 1024,
  "top_p": 0.9,
  "session_id": "optional-conversation-id",
  "stream_options": {"include_usage": true}
}
```

| Field | Description |
| --- | --- |
| `model` | Model name: `provider/model`, bare model name, `config.yaml` alias, or `local/<gguf-name>` |
| `messages` | Message array (role: system/user/assistant; content supports text or multimodal parts) |
| `stream` | When `true`, returns SSE (each chunk `data: {...}`, ending with `data: [DONE]`) |
| `temperature` / `max_tokens` / `top_p` | Sampling parameters (optional) |
| `session_id` | Conversation ID, used to correlate rounds in usage stats (optional) |
| `stream_options.include_usage` | Request streaming usage (passed through to upstream) |

**Request header**: `X-Gateway-Api-Key: <key-name-or-index>` (optional), for multi-key selection.

**Response (non-streaming)**:

```json
{
  "id": "chatcmpl-xxx",
  "content": "Hello! How can I help you?",
  "usage": {"prompt_tokens": 12, "completion_tokens": 24, "total_tokens": 36},
  "finish_reason": "stop",
  "model": "deepseek-chat"
}
```

**Response (streaming SSE)**:

```
data: {"id":"s1","content":"Hel","thinking":null}
data: {"id":"s1","content":"lo","thinking":"(thinking...)"}
data: {"id":"s1","usage":{"total_tokens":36},"finish_reason":"stop"}
data: [DONE]
```

### Model List /v1/models

`GET /v1/models` → `{"object":"list","data":[{id, object, owned_by, api_keys, vision}]}`

- Cloud model format: `provider/model` (e.g. `deepseek/deepseek-chat`);
- Local GGUF model format: `local/<model-name>` (e.g. `local/demo-qwen2`);
- `api_keys`: available key names for the provider and whether they are configured;
- `vision`: whether image input is declared as supported.

### Usage Stats /v1/usage/stats

Dual-dimension aggregation, returns `{"mode": "time"|"conversation", "data": [...]}`.

**Dimension 1: Time aggregation** — `?time_range=<granularity>`

| Granularity | Meaning | Bucket |
| --- | --- | --- |
| `10min` | Last 10 minutes | 1 minute |
| `0.5h` | Last 30 minutes | 1 minute |
| `1h` | Last 1 hour | 5 minutes |
| `2h` | Last 2 hours | 10 minutes |
| `5h` | Last 5 hours | 30 minutes |
| `10h` | Last 10 hours | 1 hour |
| `1d` | Last 1 day | 1 hour |
| `7d` | Last 7 days | 6 hours |
| `30d` | Last 30 days | 1 day |
| `0.5y` | Last half year | 1 week |
| `total` | All time | Dynamic based on data span |
| `custom` | Custom range | Requires both `start_ts` and `end_ts` (Unix seconds) |

Examples: `/v1/usage/stats?time_range=1d`, `/v1/usage/stats?time_range=custom&start_ts=1700000000&end_ts=1700086400`

**Dimension 2: Conversation aggregation** — `?limit=<N|total>`

Groups by session (ordered by last activity, newest first) and sums the tokens of the most recent N conversations. `limit` accepts `1 / 10 / 50 / 100 / 200 / 500 / total` or any positive integer (1–100000).

### Ollama-Compatible Endpoints

| Endpoint | Ollama equivalent | Description |
| --- | --- | --- |
| `GET /api/tags` | `GET /api/tags` | List all models (local GGUF + cloud) |
| `POST /api/chat` | `POST /api/chat` | messages chat; `{"model","messages","stream","options"}`, streaming supported |
| `POST /api/generate` | `POST /api/generate` | prompt generation; `{"model","prompt","stream","options"}` |

- `options.temperature` and `options.num_predict` map to cloud sampling parameters;
- Local GGUF models return a metadata summary (this engine only parses, it does not infer);
- Streaming output is NDJSON (one JSON object per line: `{"message":{"content":...},"done":false}` … `{"done":true}`).

### GGUF Metadata Endpoint

`GET /api/gguf/<model-name>` → model metadata JSON:

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

### Health Check

`GET /health` → `{"status":"ok"}` (not rate-limited)

---

## Configuration

### Global Config config.yaml

```yaml
server:
  host: 127.0.0.1   # Localhost only by default; use 0.0.0.0 + gateway_api_key for LAN sharing
  port: 8000

# Gateway auth (optional): when set, all business endpoints require Authorization: Bearer <value>
# gateway_api_key: ""

# Max requests per IP per minute; 0 disables rate limiting; /health is exempt
rate_limit_per_minute: 60

# Model aliases: map short names to provider/model
aliases:
  fast: deepseek/deepseek-chat
  smart: qwen/qwen-plus
  long: kimi/moonshot-v1-8k

# Default model used when no model is specified
default_model: deepseek/deepseek-chat
```

> The command line `serve --host H --port P` overrides the `server` section.

### Cloud Provider Config plugins

Each provider has its own directory (`plugins/<provider>/config.yaml`), auto-scanned at engine startup:

```yaml
plugin: openai_compatible
base_url: https://api.deepseek.com/v1
# Option A: single key
api_key: sk-xxxxx
# Option B: multiple keys (api_keys takes precedence)
api_keys:
  - name: default
    key: ${DEEPSEEK_API_KEY}     # ${ENV_VAR} supported, avoids plaintext keys on disk
  - name: backup
    key: sk-yyyyyy
models:
  - deepseek-chat
  - deepseek-reasoner
vision_models: []               # optional: models that support images
```

**Built-in providers**: `deepseek` / `kimi` / `openai` / `qwen` (all reuse the `openai_compatible` adapter). **Adding a provider takes three steps**:

1. Create a directory named after the provider under `plugins/`;
2. Create a `config.yaml` inside it (with `plugin` / `base_url` / `key` / `models`);
3. Restart the engine.

**Multi-key selection**: use the `X-Gateway-Api-Key: <name-or-index>` request header to pick a key; the first configured key is used by default.

---

## Local GGUF Models

1. Put any `.gguf` file into the `models/` directory (auto-scanned after restart);
2. Access it in any of these ways:
   - `gh gguf models/xxx.gguf` (command-line parse);
   - `GET /api/gguf/<name>`;
   - `GET /v1/models` (shown as `local/<name>`);
   - Select `local/<name>` in CLI / GUI to chat (returns a metadata summary).

> Note: this engine provides GGUF **parsing** (architecture, context, vocabulary metadata, etc.) but no local inference. For local inference, pair it with llama.cpp or another inference backend.

---

## Usage Stats & Data Files

- Every successful cloud conversation records: `session_id`, `request_id`, `api_key_name`, `model_name`, `prompt_tokens`, `completion_tokens`, `total_tokens`, `timestamp`;
- Data is persisted to `usage_stats.json` in the engine's working directory (thread-safe, atomic writes: temp file then rename);
- Query endpoint: `/v1/usage/stats` (see above);
- A `gateway.pid` file is also generated at runtime (engine PID, used by the scripts).

---

## Security

1. **Gateway auth**: after setting `gateway_api_key` in `config.yaml`, `/v1/chat/completions`, `/v1/models`, `/v1/usage/stats`, and all `/api/*` require `Authorization: Bearer <key>`; when unset, access is open (intended for local use).
2. **Rate limiting**: per-IP sliding-window limiting (`rate_limit_per_minute`); exceeding it returns a unified `429 rate_limit_error`; `/health` and `/` are exempt.
3. **CORS**: any origin (`*`) with GET/POST/OPTIONS allowed, convenient for local web tools.
4. **Key protection**: `${ENV_VAR}` references are supported to avoid plaintext API keys on disk.
5. **No error leakage**: upstream errors are mapped to `{error:{message,type,code}}`; internal stack traces are never exposed.

---

## Directory Structure

```
General_Harness_Release/
├── bin/
│   ├── gh_upx.exe           # Engine (UPX-compressed, 1.6 MB, recommended)
│   └── gh.exe               # Engine (stripped, 5.2 MB)
├── engine/                  # Go engine source (zero third-party deps)
│   ├── main.go              # Entry point & subcommand dispatch
│   ├── server.go            # HTTP routing & all handlers
│   ├── http.go              # Hand-written HTTP/1.1 server (net package only)
│   ├── cloud.go             # Cloud proxy (retry/error mapping/SSE parsing)
│   ├── ollama.go            # Ollama protocol compatibility layer
│   ├── gguf.go              # GGUF binary parser
│   ├── usage.go             # Usage stats (JSON persistence)
│   ├── config.go            # Config loading & provider registry
│   ├── yaml.go              # Hand-written YAML subset parser
│   ├── cli.go               # Terminal interactive mode
│   ├── util.go              # Utilities & rate limiter
│   ├── engine_test.go       # Unit tests
│   ├── integration_test.go  # HTTP integration tests
│   └── go.mod               # Module definition (no external deps)
├── gui/
│   ├── gui.py               # Webview GUI (pywebview / browser fallback)
│   └── requirements.txt     # GUI dependency (pywebview>=5.0, optional)
├── models/
│   └── demo-qwen2.gguf      # Sample GGUF (for demo)
├── plugins/
│   ├── deepseek/config.yaml
│   ├── kimi/config.yaml
│   ├── openai/config.yaml
│   ├── qwen/config.yaml
│   └── openai_compatible/plugin.py   # Compatibility note (engine is embedded; this file is for reference)
├── cli/README.md            # CLI mode notes
├── config.yaml              # Global config
├── start.py                 # Unified launcher
├── start.bat                # One-click start script
├── restart.bat              # One-click restart script
├── stop.bat                 # One-click stop script
├── tests/test_gui.py        # GUI / proxy tests
└── README.md                # This document
```

---

## Building from Source

Prerequisites: Go 1.21+ (Windows).

```bash
cd engine
go build -ldflags "-s -w -buildid=" -trimpath -o ../bin/gh.exe .

# Optional: UPX extreme compression (~3x smaller; requires UPX)
upx --lzma -9 -o ../bin/gh_upx.exe ../bin/gh.exe
```

The engine source has zero third-party dependencies (pure standard library): a hand-written HTTP/1.1 server (`http.go`) and a hand-written YAML subset parser (`yaml.go`); JSON handling also uses the standard library.

---

## Running Tests

```bash
# Go engine tests (unit + HTTP integration)
cd engine && go test ./...

# Python component tests (GUI proxy, frontend page, launcher logic)
python -m unittest discover -s tests
```

Test coverage: YAML parsing, GGUF parsing (including invalid magic), usage stats (time / conversation / custom / persistence / concurrency), key parsing, provider registration, model routing, rate limiting, auth, all three Ollama endpoints, streaming & non-streaming accounting, GUI proxy forwarding, and single-file frontend integrity.

---

## Size Design Notes

| Build | Size |
| --- | --- |
| Engine stripped (`-s -w -buildid=` + trimpath) | 5.2 MB |
| Engine UPX-compressed (`--lzma -9`) | **1.6 MB** |
| GUI frontend single-file HTML | <10 KB |

Design approach: hand-written HTTP layer using only the `net` package (avoiding the ~4 MB `net/http` standard library) + zero third-party dependencies + UPX compression. On Windows, the physical floor for a network-capable Go binary is about 0.72 MB (pure `net` package + UPX extreme); this engine's 1.6 MB remains comfortably lightweight. The engine is a single file with zero runtime dependencies — extract and run.

---

## FAQ

### 🚀 Installation & Startup

<details>
<summary><b>Can I run it without Python?</b></summary>

Yes. The engine is a standalone single-file binary:

- `bin\gh_upx.exe serve` — starts the HTTP service;
- Run `bin\gh_upx.exe` directly — enters the terminal CLI.

Python is only used in two places: the `start.py` launcher and the Webview GUI mode.
</details>

<details>
<summary><b>What's the difference between the installer and the portable ZIP?</b></summary>

Both contain identical functionality; they differ in delivery format:

| Installer (.exe) | Portable ZIP (.zip) |
| --- | --- |
| Wizard-based install with selectable location | Extract and run, no installation |
| Creates desktop & Start Menu shortcuts | No registry writes, no system directories |
| Configures GUI dependencies during install | Ideal for portable / no-install scenarios |

For regular use, the installer is recommended; for a quick trial or on-the-go use, pick the portable ZIP.
</details>

<details>
<summary><b>What if the port is already in use?</b></summary>

Just pick a new port:

```bash
python start.py --port 9000                 # via the launcher
bin\gh_upx.exe serve --port 9000            # start the engine directly
python gui/gui.py --url http://127.0.0.1:9000   # point the GUI at the new port
```

> After changing `server.port` in `config.yaml`, `start.py` picks it up automatically.
</details>

### ⚙️ Configuration & Models

<details>
<summary><b>Can I use it without an API key?</b></summary>

Yes. Without keys you can still: list models, parse GGUF, and chat with `local/demo-qwen2` (returns a metadata summary). Real cloud conversations require a valid key in `plugins/*/config.yaml`.
</details>

<details>
<summary><b>How do I add a cloud provider or model?</b></summary>

Three steps, no code changes:

1. Create a directory named after the provider under `plugins/` (e.g. `plugins/myvendor/`);
2. Create a `config.yaml` in it with `plugin`, `base_url`, `api_key`, and `models`;
3. Restart the engine; the new model is available as `myvendor/<model-name>`.

> You can also simply append new model names to the `models` list of an existing provider's `config.yaml`.
</details>

<details>
<summary><b>Do I need to restart after changing config?</b></summary>

Yes. `config.yaml`, `plugins/*/config.yaml`, and `models/*.gguf` are loaded or scanned at engine startup; restart the engine for changes to take effect. Use `restart.bat` for a one-click restart.
</details>

<details>
<summary><b>Why are there two gh.exe files?</b></summary>

- `gh_upx.exe`: UPX-compressed (~1.6 MB), smaller, decompresses to memory at runtime;
- `gh.exe`: regular stripped build (~5.2 MB), slightly faster startup.

They are functionally identical; use `gh_upx.exe` day-to-day. All scripts auto-pick it and only fall back to `gh.exe` when it is missing.
</details>

### 🔌 Integration

<details>
<summary><b>How do I connect Open WebUI?</b></summary>

Configure the engine address as an Ollama-compatible endpoint:

- Engine address: `http://127.0.0.1:8000`;
- Open WebUI auto-discovers and uses the engine's models via `/api/tags` and `/api/chat`.

You can also call the Ollama-style endpoints directly with curl:

```bash
curl http://127.0.0.1:8000/api/tags
```
</details>

<details>
<summary><b>What's the difference between streaming and normal output?</b></summary>

- Normal: returns the complete JSON once the request finishes;
- Streaming (`stream: true`): pushes `content` chunk by chunk over SSE, ending with `data: [DONE]` — lower time-to-first-token, smoother experience;
- Thinking models additionally return `thinking` (upstream `reasoning_content`) in streaming mode; both the CLI and GUI show the thinking process dynamically.
</details>

<details>
<summary><b>Why does chatting with a GGUF model only return metadata?</b></summary>

This engine is positioned as "parse + gateway": GGUF provides metadata parsing (architecture, context length, vocabulary, etc.) but no local inference. For local inference, connect a backend like llama.cpp, or chat with a cloud model.
</details>

<details>
<summary><b>How do I let other devices on the LAN access it?</b></summary>

1. Change `server.host` in `config.yaml` to `0.0.0.0`;
2. Strongly recommended: set `gateway_api_key` at the same time to prevent unauthorized calls from consuming quota;
3. Other devices access it via `http://<your-IP>:8000`.

> Listening on `127.0.0.1` only is the default — this is an intentional security design.
</details>

### 🛠 Troubleshooting

<details>
<summary><b>Will usage_stats.json grow forever?</b></summary>

It is currently append-only with no automatic cleanup cap. For long-running use, back up or clear the file periodically — just delete it; the engine recreates an empty store automatically and nothing else is affected.
</details>

<details>
<summary><b>The GUI window won't open — what should I do?</b></summary>

Check in order:

1. Confirm Python ≥ 3.10 (`python --version`);
2. Confirm the engine is ready (`curl http://127.0.0.1:8000/health` returns `{"status":"ok"}`);
3. Without pywebview it automatically falls back to the system browser (normal degradation); for a native window run `pip install -r gui/requirements.txt`;
4. If still no window, open `http://127.0.0.1:8765/index.html` directly — the same frontend.
</details>

<details>
<summary><b>The engine exits immediately after starting — what should I do?</b></summary>

Usually a port conflict or a config error:

1. Check whether the process in `gateway.pid` already exists;
2. Inspect the port with `netstat -ano | findstr :8000`;
3. Run `stop.bat`, then `start.bat` again; if it still fails, check the YAML syntax of `config.yaml` and `plugins/*/config.yaml` (indentation).
</details>

---

*General Harness — a minimalist AI gateway. The core engine is fully decoupled from the UI, letting you switch seamlessly between different frontends.*

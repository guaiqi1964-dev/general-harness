# General Harness — Minimalist AI Gateway (v0.3.0)

> A ready-to-use local AI gateway: a **single Go binary engine** that unifies
> **cloud API proxying**, **Ollama protocol compatibility**, **GGUF file
> parsing**, **web search**, and **guarded Agent command execution**, exposed
> through a standard HTTP API. Two frontends are provided — a **terminal CLI**
> and a **Webview GUI** (collapsible Thinking panel). The core engine and the UI
> are fully decoupled: use the CLI when only the backend is running, and the GUI
> when both run together.

- **Engine**: `bin/gh.exe` (6,482,432 bytes ≈ 6.18 MB) — single file, zero third-party dependencies, ready to run after extraction. Since v0.3.5 the release no longer ships a UPX-compressed build: Windows Defender flags it as `Trojan:Win32/Wacatac.B!ml` and quarantines it on extraction.
- **Engine source**: `engine/` — pure Go standard library (including a hand-written HTTP layer), no external modules
- **GUI**: `gui/gui.py` — uses pywebview to call the native Edge WebView2; the engine also serves an equivalent built-in page at `GET /ui` (v0.3.1, key panel added in v0.3.2), which `start.bat` opens automatically when Python or pywebview is missing
- **Version**: `0.3.5` (new in this release: copy any question or answer, delete a single turn, and regenerate an answer — see [Fixed in v0.3.5](#fixed-in-v035); earlier fixes: [v0.3.4](#fixed-in-v034) / [v0.3.3](#fixed-in-v033) / [v0.3.2](#fixed-in-v032) / [v0.3.1](#fixed-in-v031))

---

## 📋 Table of Contents

- [Feature Overview](#feature-overview)
- [Installation & Quick Start](#installation--quick-start)
  - [Option 1: Windows Installer](#option-1-windows-installer)
  - [Option 2: Build from Source](#option-2-build-from-source)
  - [One-Click Scripts](#one-click-scripts)
  - [Command Line Startup](#command-line-startup)
  - [Minimal Verification](#minimal-verification)
- [Three Startup Modes](#three-startup-modes)
- [Terminal CLI Mode](#terminal-cli-mode)
- [Webview GUI Mode](#webview-gui-mode)
- [HTTP API Reference](#http-api-reference)
  - [Unified Chat /v1/chat/completions](#unified-chat-v1chatcompletions)
  - [Agent Command Execution](#agent-command-execution)
  - [Web Search](#web-search)
  - [Model List /v1/models](#model-list-v1models)
  - [Usage Stats /v1/usage/stats](#usage-stats-v1usagestats)
  - [Ollama-Compatible Endpoints](#ollama-compatible-endpoints)
  - [GGUF Metadata Endpoint](#gguf-metadata-endpoint)
- [Configuration](#configuration)
  - [Global Config config.yaml](#global-config-configyaml)
  - [Web Search Config search](#web-search-config-search)
  - [Cloud Provider Config plugins](#cloud-provider-config-plugins)
- [Local GGUF Models](#local-gguf-models)
- [Usage Stats & Data Files](#usage-stats--data-files)
- [Security](#security)
- [Directory Structure](#directory-structure)
- [Building from Source](#building-from-source)
- [Running Tests](#running-tests)
- [Size Design Notes](#size-design-notes)
- [API Keys & Credentials](#api-keys--credentials)
- [Fixed in v0.3.5](#fixed-in-v035)
- [Fixed in v0.3.4](#fixed-in-v034)
- [Fixed in v0.3.3](#fixed-in-v033)
- [Fixed in v0.3.2](#fixed-in-v032)
- [Fixed in v0.3.1](#fixed-in-v031)
- [Fixed in v0.3.0](#fixed-in-v030)
- [FAQ](#faq)

---

## Feature Overview

| Capability | Description |
| --- | --- |
| 🖥️ **Dual-Mode Frontend** | Terminal CLI (ANSI color streaming) and Webview GUI (native rendering engine, collapsible thinking panel); core decoupled from UI |
| ☁️ **Cloud API Proxy** | Plug-and-play OpenAI-compatible providers: DeepSeek / Kimi / Qwen / OpenAI; multiple keys with on-demand selection |
| 🌐 **Web Search (new in v0.3.0)** | `POST /v1/search`, `GET /v1/search/backends`, and a `web_search` extension field on chat; **disabled by default**; configurable `http_json` backends (e.g. self-hosted SearXNG); no new third-party dependencies |
| 🤖 **Agent Command Execution** | `POST /v1/agent/run` and `POST /v1/agent/loop` execute host commands under an allow-list plus a dangerous-shell block-list; disabled by default |
| 🦙 **Ollama Protocol Compatible** | Provides `/api/tags`, `/api/chat`, `/api/generate`, `/api/models`; Ollama ecosystem tools such as Open WebUI can connect directly |
| 📦 **GGUF Parsing** | Reads local `.gguf` model metadata: architecture, version, tensor count, context length, vocabulary, file type |
| 📊 **Usage Statistics** | Time dimension (10 minutes to half a year, or custom range) and conversation dimension (last N sessions); atomic JSON persistence, 10000-record in-memory cap |
| 🔄 **Automatic Retry** | Exponential backoff retry (up to 3 attempts) for 429, 5xx, and network errors; upstream errors mapped uniformly |
| 🔒 **Security** | Optional gateway auth (Bearer Token), per-IP sliding-window rate limiting, CORS allowed, keys support environment variable injection, SSRF-guarded page fetching |
| ⚡ **Extremely Lightweight** | Single 6.18 MB binary (stripped), zero runtime dependencies; hand-written HTTP layer and YAML subset parser |
| 🔑 **Key Management** (v0.3.1) | Fill in provider keys from the GUI 🔑 panel, the CLI `/key` command, or `GET/POST /v1/keys`; writes go to `keys.yaml` and are hot-reloaded, deleting it reverts to plugin config |

---

## Installation & Quick Start

### Option 1: Windows Installer

Download `General-Harness-Setup-0.3.5.exe` from the [Releases page](https://github.com/guaiqi1964-dev/general-harness/releases/latest), double-click and follow the wizard:

- `DefaultDirName={autopf}\General Harness` combined with `PrivilegesRequired=lowest` means the default install target is **the current user's program directory**, not a machine-wide one;
- The installer bundles everything needed (engine binary, config, GUI, sample models);
- Automatically detects and installs the GUI dependency (pywebview) during installation;
- Creates Start Menu shortcuts, plus a desktop shortcut **checked by default** since v0.3.1 (the `desktopicon` task no longer carries `Flags: unchecked`);
- **Brand icon**: the setup program itself, every desktop / Start Menu shortcut, and the uninstall entry all use `assets/general-harness.ico` (9 sizes, 16–256, 32bpp), which is installed to `{app}\general-harness.ico`.

### Option 2: Build from Source

There is currently **no portable (ZIP) packaging script** in the repository — the setup script `installer.iss`, the launcher scripts, and `go build` are the supported paths. As of v0.3.0 the [Releases page](https://github.com/guaiqi1964-dev/general-harness/releases/latest) additionally ships a ZIP of the release directory built from the same tree; a ZIP you build yourself is not bound to that file name. To run without the installer:

```bash
cd engine
go build -ldflags "-s -w -buildid=" -trimpath -o ../bin/gh.exe .
bin\gh.exe serve
```

### One-Click Scripts

The installer and a source checkout both contain three management scripts. Double-click to use:

```bat
start.bat              :: One-click start, open Webview GUI
start.bat cli          :: One-click start, enter terminal CLI
start.bat server       :: One-click start, HTTP server only
restart.bat            :: One-click restart and open GUI
restart.bat cli        :: One-click restart, enter CLI
stop.bat               :: One-click stop the engine
```

Script features:

- `start.bat` and `restart.bat` prefer `bin\gh.exe` and fall back to `bin\gh.exe`;
- Probes port health before starting: reuses an already-running engine instead of starting a duplicate;
- Restart/stop kills the process via `gateway.pid` first, with a `netstat` port-based fallback;
- Waits for the engine to be ready (polling `/health`, up to 30 seconds) before launching the frontend;
- With Python, GUI mode opens a native Webview window; without Python (or without pywebview) `start.bat` now opens the engine's built-in `/ui` page instead of stopping with an error (v0.3.1).

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
> Don't want to use Python? Run `bin\gh.exe serve` directly (zero dependencies).

### Minimal Verification

```bash
# 1) Start the engine
bin\gh.exe serve --port 8000

# 2) Verify health check in another terminal
curl http://127.0.0.1:8000/health
# → {"status":"ok"}

# 3) Identify the service and its version
curl http://127.0.0.1:8000/
# → {"service":"General Harness","ui":"/ui","version":"0.3.5"}

# 4) List models (includes the bundled local GGUF sample)
curl http://127.0.0.1:8000/v1/models
```

---

## Three Startup Modes

| Command | Mode | Description |
| --- | --- | --- |
| `python start.py` | CLI | Auto-starts the engine and enters terminal chat; engine shuts down after Ctrl+C |
| `python start.py --gui` | GUI | Auto-starts the engine and opens a native Webview window |
| `python start.py --server` | Server | Starts only the engine HTTP API, stays in the background |
| `bin\gh.exe serve` | Server | Same as above, no Python needed |
| `bin\gh.exe` (no args) | CLI | Enters terminal mode directly (engine embedded, no separate server) |

`start.py` first probes `http://127.0.0.1:<port>/health`: it reuses an already-running engine, otherwise starts one; it cleans up the engine process on exit.

---

## Terminal CLI Mode

The engine has a built-in terminal interactive mode (`gh chat` or run `bin\gh.exe` directly):

```
═══ General Harness CLI ═══
Cloud providers: 4 · Local GGUF: 1 · Model deepseek/deepseek-chat
Type /help for commands, /exit to quit; Ctrl+C interrupts the current answer

You [deepseek-chat | web:off] › Hello, introduce yourself
Assistant ▸ …(streamed in green; timing and char count printed at the end)
```

The prompt shows the current model and the web-search switch. `reasoning_content` (thinking) is **collapsed into a single line** — `💭 思考中… N 字`, refreshed in place — expandable with `/think on` (dim grey) or hidden with `/think off`. When stdout is redirected the thinking text is replaced by a one-line summary instead of escape sequences.

### Built-in Commands

| Command | Description |
| --- | --- |
| `/model <name>` | Switch model (e.g. `/model qwen/qwen-plus`, `/model local/demo-qwen2`) |
| `/model <index>` | Switch by the index shown in `/models` |
| `/models` | List all cloud and local models; `▸` marks the current one, `[视觉]` marks image models |
| `/think on\|off` | Expand / collapse the thinking process |
| `/search on\|off\|status` | Turn session web search on/off, or show the current state |
| `/search list` | List configured search backends with readiness and reason |
| `/search backend <name>` | Switch the search backend for this run |
| `/web <query>` | Run a single search and print numbered sources (not added to the conversation) |
| `/key <provider> <key>` | Store a provider key in `keys.yaml`, hot-reloaded, effective immediately |
| `/key <provider> -` | Delete the override and fall back to the plugin config |
| `/usage [1d\|7d\|30d\|all]` | Usage over time as a block bar chart (only buckets with traffic are shown) |
| `/usage conv` | Recent conversations |
| `/config` | Show the effective configuration and the file paths in use |
| `/history` | Message count in the current session |
| `/clear` | Clear the current conversation history |
| `/help` | Show command help |
| `/exit` / `/quit` | Exit the CLI |

`/search on` is refused when `search.enabled=false`. The first time it turns on, it prints the privacy notice: what is sent to the search service is the locally extracted query terms (or the `rewrite_model`-generated terms), never the raw question.

**Ctrl+C** never kills the program mid-answer: while a reply streams it only cancels that generation (the partial text stays, `⏹ 已中断` is printed); when idle it exits. **Pipe mode**: `echo "question" | gh chat` prints only the answer body to stdout — status lines and statistics are suppressed so output can be piped/scripted (`--repl` forces interactive mode under redirection).

`/search on` is refused when `search.enabled=false`. The first time it turns on, it prints the privacy notice: what is sent to the search service is the locally extracted query terms (or the `rewrite_model`-generated terms), never the raw question.

### Engine Subcommands (`bin\gh.exe <subcommand>`)

| Subcommand | Description |
| --- | --- |
| `serve [--host H] [--port P] [--allow-insecure]` | Start the HTTP API server (default 127.0.0.1:8000) |
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
  - **"🌐 Web" toggle** in the header (see [Web Search](#web-search));
  - Enter to send, Shift+Enter for newline, clear conversation, status bar hints.
- **Architecture**: `gui.py` embeds a local proxy (default 127.0.0.1:8765) that forwards the browser's `/v1/*` and `/api/*` requests to the Go engine, avoiding CORS issues; the frontend is a **single embedded file with no external assets** (`INDEX_HTML`, 15317 bytes as measured from `gui/gui.py`).
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
  "stream_options": {"include_usage": true},
  "web_search": true
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
| `web_search` | v0.3.0 extension (optional): boolean or object; omitted ⇒ behavior identical to previous versions |

**Request header**: `X-Gateway-Api-Key: <key-name-or-index>` (optional), for multi-key selection.

**Response (non-streaming)**:

```json
{
  "id": "chatcmpl-xxx",
  "content": "Hello! How can I help you?",
  "usage": {"prompt_tokens": 12, "completion_tokens": 24, "total_tokens": 36},
  "finish_reason": "stop",
  "model": "deepseek-chat",
  "sources": [{"index": 1, "title": "Example", "url": "https://example.com/a", "snippet": "…"}]
}
```

The top-level `sources` array is present only when `web_search` was requested; without a search request the response is byte-for-byte the pre-0.3.0 shape (no `sources` key, no phase frame). When a search *was* requested but produced no results, the backend was unavailable, or no query could be built, `sources` is an **empty array** `[]` rather than being absent (the answer is still returned — that is the graceful-degradation path). Chat `sources` entries carry exactly `index`, `title`, `url` and `snippet`; the richer `POST /v1/search` result objects additionally carry `source` (upstream engine name, falling back to the URL host), `published_at` and `content`, the last two only when available.

**Response (streaming SSE)**:

```
data: {"id":"s1","content":"Hel","thinking":null}
data: {"id":"s1","content":"lo","thinking":"(thinking...)"}
data: {"id":"s1","usage":{"total_tokens":36},"finish_reason":"stop"}
data: [DONE]
```

When `web_search` was requested, exactly one extra frame is emitted **before** the content chunks:

```
data: {"id":"req-xxxxxx","phase":"search","sources":[{"index":1,"title":"…","url":"…","snippet":"…"}],"finish_reason":null}
```

Without `web_search` no `phase:"search"` frame is sent, so the frame sequence stays identical to previous versions.

---

### Agent Command Execution

High-risk and **disabled by default**. When `agent.enabled: true`, the model (or any authenticated caller) can execute host commands under the constraints below.

| Endpoint | Request | Response |
| --- | --- | --- |
| `POST /v1/agent/run` | `{"command": "ping", "args": ["127.0.0.1"], "timeout_seconds": 30}` | `{"command", "args", "exit_code", "stdout", "stderr", "timed_out", "truncated", "error"?}` |
| `POST /v1/agent/loop` | `{"goal": "…", "model": "deepseek/deepseek-chat", "max_steps": 10}` | `{"answer": "…", "steps": [{"step", "command", "args", "exit_code", "stdout", …}]}` |

**Configuration** (`agent:` block in `config.yaml`):

```yaml
agent:
  enabled: false
  allow_commands:      # executable names; case, path and extension are ignored
    - ping
    - ipconfig
    - tasklist
    - netstat
    - systeminfo
    - echo
    - findstr
  timeout_seconds: 30      # per-command timeout (default 30 s)
  max_output_bytes: 65536  # stdout/stderr cap per stream (default 64 KiB)
  # work_dir: ""           # working directory (empty = engine working directory)
```

Constraints:

- **Command allow-list**: only executables whose base name matches `allow_commands` (case-insensitive, path and extension stripped) may run. An empty list allows nothing.
- **Dangerous-shell block-list**: `cmd`, `powershell`, `pwsh`, `bash`, `sh`, `zsh`, `cscript`, `wscript`, `mshta`, `rundll32`, `regsvr32`, `wmic`, `certutil` are **always rejected**, even if someone adds them to `allow_commands` — allowing a general-purpose shell would make the allow-list meaningless.
- **Timeout and output caps**: a per-request `timeout_seconds` overrides the configured value and is capped at 3600 s; stdout/stderr are each capped by `max_output_bytes` and the response sets `truncated: true` when either was cut.
- Environment variables with sensitive names (suffixes `KEY`/`TOKEN`/`SECRET`/`PASSWORD`/`CREDENTIAL`) are stripped from the child process environment.
- Rejections map to `403 permission_error` (agent disabled, command not allow-listed, blocked shell), `400 invalid_request_error` (missing `command`/`goal`), or `500 internal_error` (execution failure). The loop additionally returns `403` when the agent is disabled, so repeated calls cannot silently burn cloud quota.
- **`max_steps`**: defaults to 10 when omitted; values above 20 are also clamped to 10 (only 1–20 are honored), and the loop stops with `达到最大步数 %d，未完成任务` when the budget is exhausted.
- **Non-loopback listening requires `gateway_api_key`**: if `server.host` is not a loopback address (`127.0.0.1`/`localhost`/`::1`) and `gateway_api_key` is empty, the engine **refuses to start** unless the engine start flag `--allow-insecure` is passed. This guards the high-risk Agent endpoints too.

---

### Web Search

#### Overview

Web search is **disabled by default**. When `search.enabled: true`, the engine provides:

| Surface | Description |
| --- | --- |
| `POST /v1/search` | One-shot search, returns the normalized result list |
| `GET /v1/search/backends` | Backend inventory with readiness diagnostics |
| `web_search` field on `/v1/chat/completions` | Retrieves, injects the results as untrusted context, and returns `sources` |

Backends are plain `http_json` adapters (self-hosted SearXNG, Tavily, etc.). This feature adds **no new third-party dependencies** — it reuses the hand-written HTTP client in `engine/cloud.go`.

#### Privacy red line

> **The user's raw question is never sent to the search service.**

- `query_mode: keywords` (default): the engine extracts search terms **locally** by rule — punctuation/whitespace splitting, removal of English and Chinese stop words and instruction words, then dedup and truncation (at most 8 terms, rune-bound capped). Entities and English/numeric terms are kept.
- `query_mode: rewrite`: the engine asks the `rewrite_model` (resolved on your own configured provider, `max_tokens=64`) to produce the query; the prompt is "rewrite the question into search keywords, output only the keywords". If the rewrite model cannot be resolved or fails, it falls back to local keyword extraction.
- **There is no raw mode.** No configuration makes the engine forward the question verbatim.
- Note that the extracted terms **still reveal the topic** — the red line prevents sending the full question text, not the disclosure of subject matter.
- stderr logs only backend name / elapsed ms / result count / cache hit (`搜索 <backend> 耗时 <n>ms 结果 <n> 条 缓存 hit|miss`); the query text is never logged.

#### Configuration

```yaml
search:
  enabled: false            # master switch; routes are not registered while false
  backend: searxng          # default backend name (a key under backends)
  query_mode: keywords      # keywords (local extraction, default) | rewrite (own model)
  # rewrite_model: deepseek/deepseek-chat   # used only when query_mode: rewrite
  max_results: 5            # results per search (per-request override allowed; hard cap 20)
  timeout_seconds: 10       # per-search timeout
  inject_budget_chars: 4000 # character budget for the injected context
  cache_ttl_seconds: 300    # result cache TTL; 0 disables caching
  max_concurrent: 8         # concurrent searches (excess returns 429)
  fetch_content: false      # false = snippets only; true = also fetch page text
  fetch:
    max_pages: 3            # max pages fetched in content mode
    max_chars_per_page: 4000
    timeout_seconds: 8
    user_agent: GeneralHarness/0.3
    allow_private_hosts: false  # SSRF switch: keep false to reject loopback/private hosts
  backends:
    searxng:
      type: http_json
      base_url: http://127.0.0.1:8888
      path: /search
      method: GET
      query_param: q
      params:
        format: json
      result_path: results
      map:
        title: title
        url: url
        snippet: content
        source: engine
```

Backend keys: `type` (only `http_json` is accepted; defaults to `http_json`), `base_url` (required), `path`, `method` (default `GET`), `query_param`, `params`, `headers`, `body`, `result_path` (dot path to the result array; when omitted the engine probes `results`/`data`/`items`/`organic`/`web` and then any array value), and `map` (logical field → upstream field; logical keys are `title`, `url`, `snippet`, `source`, `published_at`, each with built-in fallbacks).

`${ENV_VAR}` references are expanded inside **all** string values, including inline uses such as `Authorization: Bearer ${TAVILY_API_KEY}`. A backend whose referenced environment variable is unset reports `ready: false` with reason `环境变量未设置: <NAME>`.

> ⚠️ **Parser limitation (current behaviour)**: the hand-written subset parser (`engine/yaml.go`) does **not** understand inline flow syntax (`{}` / `[]`). Writing `params: {format: json}` or `map: {title: title, ...}` makes the value a plain string that is then **silently ignored** (`yamlStringMap` only accepts mapping nodes), and `allow_commands: [ping, ...]` is ignored the same way, leaving the allow-list empty so every command is rejected. Always write block mappings / block sequences — as in the examples above and in the shipped `config.yaml`.

**Self-hosted SearXNG example** (the one shipped in `config.yaml`, pointing at `http://127.0.0.1:8888`): SearXNG must have the **`json` output format enabled in its `settings.yml`** — otherwise the endpoint returns HTML and the engine reports a backend/result-parsing failure.

#### API contract

**`POST /v1/search`**

| Direction | Field | Notes |
| --- | --- | --- |
| Request | `query` | Required, non-empty after trimming |
| Request | `max_results` | Optional; falls back to `search.max_results`, then 5; **hard cap 20** |
| Request | `backend` | Optional; falls back to `search.backend` |
| Request | `fetch_content` | Optional boolean; overrides `search.fetch_content` |
| Response | `query` | The query that was actually used |
| Response | `backend` | Backend name that served the request |
| Response | `elapsed_ms` | Elapsed time in milliseconds (0 for a cache hit) |
| Response | `cached` | `true` when served from the TTL cache |
| Response | `results[]` | Each: `index`, `title`, `url`, `snippet`, `source`, `published_at` (omitted when empty), `content` (omitted when empty) |

Status codes: `400` empty query or unknown backend · `401` gateway auth failure · `429` concurrency gate full · `503` backend unavailable / connect or read failure / no backends configured · **`404` for both search routes while `search.enabled=false` (the routes are simply not registered, matching the pre-0.3.0 unknown-path behavior)** · `405` wrong method.

**`GET /v1/search/backends`**

```json
{
  "enabled": true,
  "query_mode": "keywords",
  "fetch_content": false,
  "data": [{"name": "searxng", "type": "http_json", "ready": true, "reason": ""}]
}
```

`ready` is `false` with a human-readable `reason` when a backend is misconfigured — for example a missing `base_url` (`缺少 base_url`), an unsupported `type`, or an unset environment variable referenced by its headers/params/body. This is the endpoint the GUI probes before enabling its toggle.

#### Chat extension field

`web_search` accepts a boolean or an object:

| Form | Meaning |
| --- | --- |
| `true` / `false` | Enable/disable retrieval for this request with configured defaults |
| `{"enabled": bool, "max_results": int, "backend": string, "query_mode": "keywords"\|"rewrite", "query": string, "fetch_content": bool}` | Per-request overrides; `enabled` defaults to `true` in object form, and an explicit `query` skips query construction entirely |

Behavior:

- Non-streaming responses gain a top-level `sources: [{index, title, url, snippet}]`.
- Streaming sends **one** frame `data: {"id":…,"phase":"search","sources":[…],"finish_reason":null}` before the content chunks; when no search was requested, no such frame is emitted.
- A failing search backend **degrades gracefully**: the answer is produced without sources (`sources` is an empty array), and the error is written to stderr only.
- Requesting `web_search` while `search.enabled=false` returns `403 permission_error` — the request is rejected rather than silently answered without retrieval.
- Retrieved context is injected as the first `system` message, wrapped with an explicit label that the pages are external, untrusted material that must never be treated as instructions and should be cited as `[n]`. The injected text is capped by `inject_budget_chars`.
- **Local GGUF models (`local/xxx`) do not get retrieved context injected** — they return the fixed metadata summary without touching the search path.

#### Security & privacy

- **SSRF protection**: only `http`/`https` targets are accepted; loopback, private, link-local, multicast and cloud-metadata addresses are rejected by default. `fetch.allow_private_hosts: true` is the explicit switch that lifts this. **Every redirect hop is re-validated** (up to 3 redirects by default).
- Page fetching sends `Host` and `User-Agent` only — **no Cookie and no Referer** — and deliberately **bypasses HTTP proxies** for controllability.
- HTML extraction drops `script`, `style`, `noscript`, `svg`, `template`, `head` and `iframe` before producing text, and result URLs are normalized (scheme/host lowercased, fragment and tracking parameters such as `utm_*`/`fbclid`/`gclid`/`ref` removed); duplicate URLs are de-duplicated.
- Fetched pages are labeled as **untrusted data** in the injected system context.
- Content fetching is controlled by `fetch_content`: with the default `false` **only snippets are injected**, and no page is ever fetched.

#### Frontend usage

- **GUI**: the header "🌐 Web" toggle stores its state in `localStorage` and is **off by default**. Before enabling it probes `GET /v1/search/backends` and refuses to turn on when the engine reports 404 (search disabled), a non-OK status, or **no ready backend**. While the toggle is on, the status line shows "searching…" from the moment the request is sent (unconditionally, whether or not the search ends up returning results); once the stream's search phase frame arrives it switches to "found N sources, generating…", and clickable source cards (numbered, opening in a new window with `rel="noopener noreferrer"`) are rendered under the answer.
- **When the toggle is off, the request body is field-for-field identical to previous versions** — the `web_search` field is simply not added.
- **CLI**: `/search on|off|status` toggles session retrieval and reports the backend plus query mode; `/web <query>` runs one search without entering the conversation; `/help` lists both commands. Numbered sources are printed under the answer as `[n] title — URL`.

#### Cache & concurrency

- **TTL cache**: default TTL 300 s (`cache_ttl_seconds: 0` disables it), capacity 256 entries; the key is `backend|max_results|fetch|query`. When full, expired entries are evicted and the cache is reset if it is still full.
- **Concurrency gate**: at most `max_concurrent` searches (default 8) run at once; exceeding it returns `429 rate_limit_error` immediately instead of queueing.

---

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
| `custom` | Custom range | `start_ts` and `end_ts` are optional (Unix seconds): defaults are `start_ts=0`, `end_ts=now`; if they are swapped (`end <= start`) the engine swaps them back instead of erroring |

Examples: `/v1/usage/stats?time_range=1d`, `/v1/usage/stats?time_range=custom&start_ts=1700000000&end_ts=1700086400`

Empty buckets inside the requested range are **backfilled** with zero-valued entries, so the returned series has a continuous time axis.

**Dimension 2: Conversation aggregation** — `?limit=<N|total>`

Groups by session (ordered by last activity, newest first) and sums the tokens of the most recent N conversations. `limit` accepts `1 / 10 / 50 / 100 / 200 / 500 / total` or any positive integer (1–100000).

### Ollama-Compatible Endpoints

| Endpoint | Ollama equivalent | Description |
| --- | --- | --- |
| `GET /api/tags` | `GET /api/tags` | List all models (local GGUF + cloud) |
| `POST /api/chat` | `POST /api/chat` | messages chat; `{"model","messages","stream","options"}`, streaming supported |
| `POST /api/generate` | `POST /api/generate` | prompt generation; `{"model","prompt","stream","options"}` |
| `GET /api/models` | `GET /api/tags` (alias) | Same model list as `/v1/models` |
| `GET /api/gguf/<name>` | — | GGUF metadata for a local model (see below) |

- `options.temperature` and `options.num_predict` map to cloud sampling parameters;
- Local GGUF models return a metadata summary (this engine only parses, it does not infer);
- **Streaming output is SSE** (`data: {...}` frames, one JSON object per frame: `{"message":{"role":"assistant","content":"…"},"done":false}` … `{"done":true}`) — it is *not* NDJSON, and cloud streams always terminate with a `done:true` frame so Ollama clients stop waiting.
- **Model naming**: `/api/*` always uses the names returned by `GET /api/tags` — for a local GGUF model that is the **bare name** (e.g. `demo-qwen2`), for cloud models `provider/model`. The `local/<name>` form only works on `/v1/*` (including CLI/GUI); sending it to `/api/chat` yields 404 `未知厂商: local`. Any other name yields 404 `未知模型: xxx`.

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

### Root Endpoint

`GET /` → `{"service":"General Harness","ui":"/ui","version":"0.3.5"}` (not rate-limited)

`GET /ui` → the built-in browser UI (HTML, 13,307 bytes embedded via `go:embed`; not rate-limited).
It matches the desktop GUI feature-for-feature and needs no Python.

The `"ui":"/ui"` field is new in v0.3.1 — `start.bat` opens that URL when Python (or pywebview) is missing.

The old `"docs":"/docs"` field was misleading — the engine never implemented a `/docs` route — and has been removed in v0.3.0.

---

## Configuration

### Global Config config.yaml

```yaml
server:
  host: 127.0.0.1   # Localhost only by default; use 0.0.0.0 + gateway_api_key for LAN sharing
  port: 8000

# Gateway auth (optional): when set, all business endpoints require Authorization: Bearer <value>
# gateway_api_key: ""

# CORS allowed origin (optional): defaults to * ; use a concrete origin when exposed
# cors_allow_origin: "*"

# Max requests per IP per minute; 0 disables rate limiting; /health and / are exempt
rate_limit_per_minute: 60

# Model aliases: map short names to provider/model
aliases:
  fast: deepseek/deepseek-chat
  smart: qwen/qwen-plus
  long: kimi/moonshot-v1-8k

# Default model used when no model is specified
default_model: deepseek/deepseek-chat

# Agent command execution (optional, high risk, disabled by default)
agent:
  enabled: false
  allow_commands:          # block sequence — inline [a, b] is NOT parsed (see the YAML subset note)
    - ping
    - ipconfig
    - tasklist
    - netstat
    - systeminfo
    - echo
    - findstr
  timeout_seconds: 30
  max_output_bytes: 65536

# Web search (optional, disabled by default) — see the search section below
search:
  enabled: false
  backend: searxng
  query_mode: keywords
```

> The command line `serve --host H --port P` overrides the `server` section. Listening on a non-loopback address without `gateway_api_key` makes the engine refuse to start unless `--allow-insecure` is passed.

### Web Search Config search

The full `search:` block, its defaults, backend keys, `${ENV_VAR}` expansion and the SearXNG example are documented in [Web Search → Configuration](#configuration). Every key listed there comes from `config.yaml`; a missing key keeps its default.

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
# vision_models:              # optional: models that support images (block sequence)
#   - qwen-vl-plus
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
- **At most 10000 records are kept in memory**; once the cap is reached, the **oldest records are silently dropped** (there is no archive or warning);
- Data is persisted to `usage_stats.json` in the engine's working directory (thread-safe, atomic writes: temp file then fsync then rename);
- **Writes are batched every 50 new records**, so a hard kill (power loss, task-manager kill) can lose up to **49 recent records**; a graceful shutdown flushes the remainder;
- Query endpoint: `/v1/usage/stats` (see above);
- A `gateway.pid` file is also generated at runtime (engine PID, used by the scripts).

---

## Security

1. **Gateway auth**: after setting `gateway_api_key` in `config.yaml`, `/v1/chat/completions`, `/v1/search`, `/v1/search/backends`, `/v1/agent/*`, `/v1/models`, `/v1/usage/stats`, and all `/api/*` require `Authorization: Bearer <key>`; when unset, access is open (intended for local use).
2. **Refuse to start when exposed without auth**: a non-loopback `server.host` with no `gateway_api_key` aborts startup unless `--allow-insecure` is explicitly passed.
3. **Rate limiting**: per-IP sliding-window limiting (`rate_limit_per_minute`); exceeding it returns a unified `429 rate_limit_error`; `/health` and `/` are exempt.
4. **CORS**: configurable origin (default `*`) with GET/POST/OPTIONS allowed, convenient for local web tools.
5. **Key protection**: `${ENV_VAR}` references are supported to avoid plaintext API keys on disk (provider keys and search backend headers/params/body alike).
6. **Search SSRF protection**: only `http`/`https`; loopback/private/link-local/multicast/cloud-metadata addresses rejected unless `fetch.allow_private_hosts: true`; every redirect hop re-validated; no Cookie/Referer sent.
7. **Search privacy**: the raw question is never sent to the search service (see [Privacy red line](#privacy-red-line)); fetched pages are labeled untrusted data.
8. **Agent containment**: executable allow-list plus a hard dangerous-shell block-list, per-command timeout, output caps, and sensitive environment variables stripped from the child process.
9. **No error leakage**: upstream errors are mapped to `{error:{message,type,code}}`; internal stack traces are never exposed.

---

## Directory Structure

```
General_Harness_Release/
├── .gitignore               # Ignores __pycache__, *.pyc, gateway.pid, usage_stats.json, logs, gh-engine.exe, release_assets
├── bin/
│   └── gh.exe               # Engine (stripped, 6,482,432 bytes)
├── engine/                  # Go engine source (zero third-party deps)
│   ├── main.go              # Entry point & subcommand dispatch
│   ├── server.go            # HTTP routing & all handlers
│   ├── http.go              # Hand-written HTTP/1.1 server (net package only)
│   ├── cloud.go             # Cloud proxy (retry/error mapping/SSE parsing, hand-written client)
│   ├── cloud_test.go        # Hand-written chunked-response tests
│   ├── ollama.go            # Ollama protocol compatibility layer
│   ├── ollama_test.go       # Ollama streaming final-done tests
│   ├── gguf.go              # GGUF binary parser
│   ├── agent.go             # Agent command executor (allow-list + shell block-list)
│   ├── agent_test.go        # Agent allow-list / block-list / timeout / truncation tests
│   ├── search.go            # Web search: config, backends, cache, concurrency gate, keywords
│   ├── search_test.go       # Search config / keywords / cache / retrieve / errors tests
│   ├── search_api.go        # POST /v1/search, GET /v1/search/backends
│   ├── search_api_test.go   # Search endpoint tests (disabled 404 / enabled / auth)
│   ├── search_chat.go       # web_search integration into chat (query build, injection, sources)
│   ├── search_chat_test.go  # Chat-side search tests (unchanged / inject / rewrite / degrade / 403)
│   ├── htmltext.go          # HTML → text extraction, SSRF validation, page fetching
│   ├── ui.go                # Built-in browser UI handler (/ui)
│   ├── keys.go              # Key management (/v1/keys, keys.yaml overlay + hot reload)
│   ├── ui.html              # Built-in UI source (embedded via go:embed)
│   ├── htmltext_test.go     # Extraction / entity / SSRF / redirect / truncation tests
│   ├── ui_test.go           # Built-in UI endpoint tests
│   ├── usage.go             # Usage stats (JSON persistence, 10000-record cap)
│   ├── config.go            # Config loading & provider registry
│   ├── yaml.go              # Hand-written YAML subset parser
│   ├── cli.go               # Terminal interactive mode (/search, /web)
│   ├── util.go              # Utilities & rate limiter
│   ├── engine_test.go       # Unit tests
│   ├── streaming_test.go    # Streaming usage-accounting tests
│   ├── integration_test.go  # HTTP integration tests
│   └── go.mod               # Module definition (no external deps)
├── gui/
│   ├── gui.py               # Webview GUI (pywebview / browser fallback, embedded proxy)
│   └── requirements.txt     # GUI dependency (pywebview>=5.0, optional)
├── models/
│   └── demo-qwen2.gguf      # Sample GGUF (for demo)
├── plugins/
│   ├── deepseek/config.yaml
│   ├── kimi/config.yaml
│   ├── openai/config.yaml
│   └── qwen/config.yaml
├── assets/
│   └── general-harness.ico  # Brand icon (9 sizes, 16–256): setup program, shortcuts, uninstall entry
├── cli/README.md            # CLI mode notes
├── config.yaml              # Global config (server / agent / search / plugins)
├── installer.iss            # Inno Setup 6 script (desktop shortcut created by default since v0.3.1)
├── start.py                 # Unified launcher (always uses bin/gh.exe)
├── start.bat                # One-click start script (prefers gh.exe)
├── restart.bat              # One-click restart script (prefers gh.exe)
├── stop.bat                 # One-click stop script
├── tests/test_gui.py        # GUI / proxy tests
├── installer.iss            # Inno Setup 6 packaging script (the only packaging script)
├── PLAN-v0.2.0.md           # Historical design notes (agent capability)
├── PLAN-v0.3.0.md           # v0.3.0 design notes (web search)
├── README.md                # Chinese documentation
└── README.en.md             # This document
```

---

## Building from Source

Prerequisites: Go 1.21+ (Windows).

```bash
cd engine
go build -ldflags "-s -w -buildid=" -trimpath -o ../bin/gh.exe .

# Optional: UPX extreme compression (~3x smaller; requires UPX)
upx --lzma -9 -o ../bin/gh.exe ../bin/gh.exe
```

The engine source has zero third-party dependencies (pure standard library): a hand-written HTTP/1.1 server (`http.go`), a hand-written HTTP client with TLS, redirect, chunked and gzip support (`cloud.go`), and a hand-written YAML subset parser (`yaml.go`); JSON handling also uses the standard library. The installer package is produced separately with Inno Setup (`ISCC.exe installer.iss`); there is no ZIP packaging script.

---

## Running Tests

```bash
# Go engine tests (unit + HTTP integration, 101 test functions)
cd engine && go test ./... -count=1

# Python component tests (GUI proxy, frontend page, launcher logic)
python -m unittest discover -s tests
```

Measured state of the suite: **101 Go test functions, all green, none skipped** (`go test ./... -count=1` → `ok gh-engine`) — the 33 pre-existing engine / integration / agent tests plus 38 new tests covering web search, HTML extraction, SSRF, chat integration, the built-in `/ui`, and the keep-alive/Content-Length fix (`htmltext_test.go` 12, `search_test.go` 11, `search_chat_test.go` 8, `search_api_test.go` 3, `ui_test.go` 2, plus 2 new cases in `cloud_test.go`), plus 8 more for key management (`keys_test.go` 6: override round-trip, YAML quoting, masking, `/v1/keys` write + hot reload, validation, loopback-only; and 2 assertions added to `ui_test.go`). **7 Python tests, all green** (`Ran 7 tests … OK`).

What is actually covered: YAML parsing, GGUF parsing (including invalid magic), usage stats (time series / conversation aggregation / custom range / persistence), key parsing and multi-key selection, provider registration, model routing, rate limiting (including 429), auth, the Ollama endpoints, streaming & non-streaming accounting, SSE parsing, the hand-written chunked reader plus keep-alive + `Content-Length` framing (with regression tests that fail if the fix is removed), the root endpoint reporting service/version/ui, agent allow-list/block-list/timeout/truncation, search config parsing and keywords, URL normalization and de-duplication, cache and fetch-content behavior, search error mapping, `search.backends` readiness, injected-context building, chat `web_search` (unchanged when absent, context+sources, explicit query, rewrite mode, streaming phase frame, graceful degradation, 403 when disabled), HTML extraction, entity decoding, SSRF rejection, redirect handling and truncation, GUI proxy forwarding, and single-file frontend integrity.

**Not covered** (stated honestly): the 429 branch for a saturated `max_concurrent` gate, concurrent multi-goroutine writes to usage stats, and live end-to-end tests against real cloud providers or real search engines — the retrieval chain is exercised against local HTTP test servers, provider calls use injected `ChatHandler`/`StreamHandler` stubs (the real-socket keep-alive + `Content-Length` path does have dedicated regression tests in `cloud_test.go`). Capabilities such as native pywebview window rendering are outside the automated suite.

---

## Size Design Notes

| Build | Size (measured) |
| --- | --- |
| Engine stripped (`-s -w -buildid=` + trimpath) — `bin/gh.exe` | 6,482,432 bytes (≈ 6.18 MB) |
| GUI frontend | Single embedded file, no external assets (`INDEX_HTML`, 14,324 characters / 15,317 bytes) |

Design approach: hand-written HTTP layer using only the `net` package (avoiding the ~4 MB `net/http` standard library) + zero third-party dependencies + UPX compression. On Windows, the physical floor for a network-capable Go binary is about 0.72 MB (pure `net` package + UPX extreme); the UPX build here is about 6.18 MB, which remains comfortably lightweight. The engine is a single file with zero runtime dependencies — extract and run.

---

## API Keys & Credentials

Before v0.3.1 the only ways to configure a provider key were editing `plugins/<provider>/config.yaml` or setting
an environment variable (`key: ${DEEPSEEK_API_KEY}`), followed by an engine restart. There are now three
equivalent channels - **all of them write `keys.yaml` and take effect immediately (hot reload, no restart)**.

### 1. GUI: the 🔑 Keys panel

Click **🔑 密钥 / Keys** in the toolbar. The panel lists every loaded provider and its state:

| State | Meaning |
| --- | --- |
| Not configured | Neither `plugins/<provider>/config.yaml` nor the environment provides a usable key; calls return 401 |
| Configured `sk-a****1234` | A usable key exists; only the first and last 4 characters are echoed |
| Placeholder (requests will fail) | The plugin still holds a sample placeholder such as `sk-xxxxxx` / `sk-yyyyyy` - non-empty, so it counts as "configured", but any real request will 401. Replace it with a real key |

Paste the key into the row and press Save; the state refreshes immediately and the next chat request uses it.

### 2. CLI: the `/key` command

```text
/key list                         # list providers and their state
/key set deepseek sk-xxxxxxxx     # write (effective immediately)
/key help
```

### 3. HTTP: `GET/POST /v1/keys`

```bash
# read state (never returns the full key, only a mask)
curl http://127.0.0.1:8000/v1/keys

# write a provider key
curl -X POST http://127.0.0.1:8000/v1/keys \
  -H "Content-Type: application/json" \
  -d '{"provider":"deepseek","key":"sk-xxxxxxxx"}'
```

Response: `{"ok":true,"provider":"deepseek","masked":"sk-x****xxxx","applied":true}`.
An unknown `provider` returns 404; empty `provider`/`key` returns 400.

### File and precedence

- Location: `keys.yaml` in the engine working directory (next to `config.yaml` and `gateway.pid`).
- Precedence: `keys.yaml` > `key`/`api_keys` in `plugins/<provider>/config.yaml` > expanded `${ENV_VAR}`.
- Hot reload: before every `/v1/keys`, chat and model-list request the engine compares `keys.yaml` mtime+size and reloads when it changed.
- Revert: **deleting `keys.yaml` restores the plugin/environment configuration** without touching any config file.
- Permissions: written with `0600` (current user only) and listed in `.gitignore`, so it is never committed by accident.
- Security: `/v1/keys` only accepts **loopback** callers (403 otherwise) and is exempt from `gateway_api_key` - it is the credential bootstrap endpoint. Never expose the engine to the public internet.

> `keys.yaml` stores keys in **plaintext** (same as `plugins/*/config.yaml`). It is more convenient than environment variables but should not be shared; use `${ENV_VAR}` when you need stronger separation.

---

## Fixed in v0.3.5

> Why: the v0.3.4 GUI was read-only — you could not select or copy an answer, you could not
> delete a single turn out of a long conversation, and an unsatisfying answer forced you to
> retype the whole question.

| Problem | Fix |
| --- | --- |
| Answer text could not be selected or copied (`pywebview`'s `text_select` defaults to off, so WebView2 injects `user-select:none`) | `create_window` in `gui/gui.py` now passes `text_select=True`; every bubble also gets a 📋 Copy button (falls back to a hidden `textarea` + `execCommand` when `navigator.clipboard` is unavailable) |
| No way to delete one specific turn — only a full "clear" | Each turn carries a 🗑 Delete button; the request context is rebuilt by `buildHistory()` from `turns`, so the conversation still lines up after a deletion |
| Regenerating meant retyping the question, with the old answer still in context | Assistant bubbles get 🔄 Regenerate: drop this turn and everything after it, then resend the same question (matches mainstream chat apps; no parallel branches) |
| UI state could drift from the request context | The front end now keeps a single source of truth, `turns` (`{q, a, qEl, aEl}`); `messages` is computed at send time |
| The UPX-compressed engine in the installer tripped Windows Defender (`Trojan:Win32/Wacatac.B!ml`) and was quarantined on extraction | Releases no longer ship `bin/gh_upx.exe`, only the uncompressed `bin/gh.exe` (6,482,432 bytes). Scripts now prefer `gh.exe` and fall back to `gh_upx.exe` for old install directories. lzma2 / DEFLATE compress the plain Go binary better, so the installer *shrank* from 5,067,208 to **4,342,864 bytes** and the ZIP from 5,075,965 to **3,010,339 bytes** |

> Note: `turns` lives in memory only and is cleared on page reload — same as v0.3.4 (no local session persistence).

---

## Fixed in v0.3.4

> Why: double-clicking the desktop icon popped two or three console windows, and you had to wait
> several seconds before the window you actually wanted appeared.

| Problem | Fix |
| --- | --- |
| GUI mode left 2-3 console windows behind: the `start.bat` window itself, the engine's `cmd /k` window and the GUI's `cmd /k` window | The GUI branch now hands over to `pythonw start.py --gui` and exits at once (measured: the script window lives 0.6 s); the engine starts with `CREATE_NO_WINDOW` and the GUI with `pythonw`, so only the pywebview window remains on screen |
| Once the engine was silent, its log disappeared and failures were unreadable | Engine stdout/stderr is appended to `{app}\engine.log`, and startup failures are reported in a system message box |
| Double-clicking again while the GUI was already running opened a second window | `start.py` probes `127.0.0.1:8765` first and returns immediately when it already answers, so nothing starts twice |
| On a machine without pywebview a double-click did nothing at all (pythonw has no console, so the traceback vanished) | Falls back to the engine's built-in page `GET /ui`, opens the browser and explains the reason in a message box; the engine keeps running for that page |
| When a `.bat` file was saved with LF endings or UTF-8 Chinese comments, cmd failed to parse it and the script exited silently | `start.bat` / `restart.bat` are kept CRLF + pure ASCII, and the constraint is documented in the scripts themselves |

---

## Fixed in v0.3.3

> Why: the CLI was usable but not comfortable — thinking printed as a bare "…", no usage panel,
> no way to inspect the live config, no way to stop a runaway answer. This release moves the
> desktop GUI's capabilities into the terminal.

| Problem | Fix |
| --- | --- |
| `reasoning_content` was discarded; the only trace was a "…" | Collapsed by default into `💭 思考中… N 字`, refreshed in place (throttled to 120 ms / 5 chars); `/think on` expands it in dim grey; under redirection a one-line summary replaces the escape sequences |
| Ctrl+C killed the whole program mid-answer | Signal handler is state-aware: while streaming it only sets a cancel flag, the stream callback returns an error so `postStream` closes the connection, partial text is kept and `⏹ 已中断` is printed; when idle it exits |
| No usage panel — stats required the GUI or curl | `/usage [1d\|7d\|30d\|all]` draws a block bar chart per bucket (only buckets with traffic); `/usage conv` lists recent conversations |
| Effective config was invisible without reading YAML | `/config` prints listen address, rate limit, auth, models, aliases, search state and backends, agent, the config/keys/usage file paths and the session id |
| Keys could be set but never removed | `/key <provider> -` deletes the override, hot-reloads and falls back to the plugin config |
| Search backend was read-only | `/search list` shows every backend with its readiness reason; `/search backend <name>` switches for this run |
| No way to script the CLI | `echo "question" \| gh chat` prints only the answer body to stdout; `--repl` forces interactive mode under redirection |
| `/models` did not show the current model or vision support | `▸` marks the current model, `[视觉]` marks image models, `/model <index>` switches by index |
| The prompt carried no state | `You [deepseek-chat \| web:off] ›` shows model and web-search state at a glance |
| Errors were a bare "internal error" | 401 → suggests `/key`; 429 / connection failures → give a direction to check; timing and char count are printed at the end |

---

## Fixed in v0.3.2

> Why: a user installed v0.3.1 in a **Python-free Windows Sandbox** — the built-in page opened and chat worked,
> but there was **nowhere to paste an API key** (the 🔑 panel in `gui.py` only exists in the desktop GUI).

| Problem | Fix |
| --- | --- |
| The built-in page `/ui` had no key entry, so Python-free users were stuck with placeholder keys or hand-editing YAML | `engine/ui.html` gained a 🔑 key panel that reads/writes `keys.yaml` through `GET/POST /v1/keys`, matching the desktop GUI and CLI `/key` |
| No feedback after saving | On success the panel echoes a mask like `sk-p****abcd` and states "takes effect immediately"; the engine hot-reloads, no restart needed |
| The panel could silently break | `engine/ui_test.go` now asserts the page contains `id="keys"`, `id="keysPanel"`, `id="keysList"`, `loadKeys`, `toggleKeys` and `/v1/keys` |
| Version lagged behind capability | `engineVersion` bumped to `0.3.2`; installer and READMEs follow |

---

## Fixed in v0.3.1

> Why: measured on a clean Windows machine with **no Python** - the installer succeeded, but double-clicking
> the desktop icon ended in an English error plus `pause`, while `installer.iss` claimed a browser-mode
> fallback that did not actually exist.

| Problem | Fix |
| --- | --- |
| On a machine without Python the app installs but is **unusable**: `start.bat` bails out with `pause & exit /b 1` | The engine now serves a built-in browser UI at `GET /ui` (embedded via `go:embed`, no external files); `start.bat` opens `http://127.0.0.1:8000/ui` when Python is missing, and now exits 0 |
| Python present but pywebview missing (blocked pip / no permission): `python gui\gui.py` raises ImportError and the window flashes away | `start.bat` additionally probes `python -c "import webview"` and falls back to `/ui` on failure |
| The root endpoint returned only the version, leaving Python-less users nowhere to go | `GET /` now includes a `"ui":"/ui"` field |
| `/ui` is a static page, so repeated refreshes consumed the rate-limit budget | `/ui` is exempt from rate limiting, like `/health` and `/` |
| Users could not find how to start the app after installing (desktop shortcut task was unchecked) | The `desktopicon` task in `installer.iss` no longer carries `Flags: unchecked`, so a desktop shortcut is created by default |
| Configuring an API key meant editing YAML, setting environment variables and restarting the engine | Added a GUI 🔑 key panel, a CLI `/key` command and `GET/POST /v1/keys`; writes go to `keys.yaml` and are hot-reloaded, no restart needed |
| On Chinese Windows the installer wizard body rendered as tofu boxes while the title bar was fine | `installer.iss` now sets `DefaultDialogFontName=Microsoft YaHei UI`: Inno's default dialog font (Segoe UI) has no CJK glyphs and slim images without FontLink do not fall back |
| The setup program, shortcuts and uninstall entry all used the generic blank system icon | Added `assets/general-harness.ico` (9 sizes, 16–256); `installer.iss` sets `SetupIconFile`, every `[Icons]` entry and `UninstallDisplayIcon` point at it, and the ico ships to `{app}\general-harness.ico` |

The built-in page matches the desktop GUI feature-for-feature (model picker, streaming, thinking fold,
web-search toggle with source cards, key panel, usage panel); it only omits the time-series chart that `gui.py`
renders as SVG in Python.
## Fixed in v0.3.0

- **Keep-alive + `Content-Length` responses no longer time out** (`engine/cloud.go`): an upstream that holds the connection open (HTTP/1.1 keep-alive) previously blocked until the read timeout, because the body was only read to EOF. The body is now length-bounded (`Content-Length`) or chunk-decoded, so it returns as soon as the boundary is reached.
- **The root endpoint no longer advertises a non-existent `/docs`**: `GET /` now returns `{"service":"General Harness","version":"0.3.0"}` instead of the misleading `{"docs":"/docs"}` field (the engine never implemented that route).
- **The GUI proxy declares HTTP/1.1**: `ProxyHandler` previously defaulted to HTTP/1.0 while forwarding chunked SSE, which is not allowed under HTTP/1.0; `protocol_version = "HTTP/1.1"` is now set.

---

## FAQ

### 🚀 Installation & Startup

<details>
<summary><b>Can I run it without Python?</b></summary>

Yes. The engine is a standalone single-file binary:

- `bin\gh.exe serve` — starts the HTTP service;
- Run `bin\gh.exe` directly — enters the terminal CLI.

Python is only used in two places: the `start.py` launcher and the Webview GUI mode.
</details>

<details>
<summary><b>Is there a portable ZIP?</b></summary>

Not currently. The repository ships an Inno Setup script (`installer.iss`) and the launcher scripts, but **no ZIP packaging script**. To run without the installer, copy the extracted files or build the engine with `go build` (see [Option 2: Build from Source](#option-2-build-from-source)); the engine itself is a single zero-dependency file.
</details>

<details>
<summary><b>What if the port is already in use?</b></summary>

Just pick a new port:

```bash
python start.py --port 9000                 # via the launcher
bin\gh.exe serve --port 9000            # start the engine directly
python gui/gui.py --url http://127.0.0.1:9000   # point the GUI at the new port
```

> Note the port-handling asymmetry: `start.py` reads `server.port` from `config.yaml` (via PyYAML, falling back silently to 8000 when PyYAML is missing), while `start.bat`, `restart.bat` and `stop.bat` **hard-code port 8000 and ignore `server.port`**. Pass `--port` or edit the scripts if you change the configured port.
</details>

<details>
<summary><b>Does `start.bat` call `start.py`?</b></summary>

No. `start.bat` invokes the engine binary directly (`serve --port 8000`, `chat`, or `python gui\gui.py`); it never calls `start.py`. The two launchers are independent paths to the same engine.
</details>

### ⚙️ Configuration & Models

<details>
<summary><b>Can I use it without an API key?</b></summary>

Yes. Without keys you can still: list models, parse GGUF, and chat with `local/demo-qwen2` (returns a metadata summary). Real cloud conversations require a valid key in `plugins/*/config.yaml`. Note that a search backend keyed by an unset environment variable is reported as `ready: false` by `GET /v1/search/backends`.
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
<summary><b>How do I turn on web search?</b></summary>

1. Set `search.enabled: true` in `config.yaml`;
2. Make a backend reachable — the bundled example assumes a self-hosted SearXNG at `http://127.0.0.1:8888` **with the `json` output format enabled in SearXNG's `settings.yml`**;
3. Restart the engine, then verify with `curl http://127.0.0.1:8000/v1/search/backends` (look for `"ready": true`);
4. Turn it on per request with `"web_search": true`, in the GUI with the "🌐 Web" toggle, or in the CLI with `/search on`.

Remember the privacy rule: the raw question is never sent — only locally extracted keywords (or terms produced by your own `rewrite_model`).
</details>

<details>
<summary><b>Do I need to restart after changing config?</b></summary>

Yes. `config.yaml`, `plugins/*/config.yaml`, and `models/*.gguf` are loaded or scanned at engine startup; restart the engine for changes to take effect. Use `restart.bat` for a one-click restart.
</details>

<details>
<summary><b>Why are there two gh.exe files?</b></summary>

The repo ships a single engine binary `bin/gh.exe` (stripped, 6,482,432 bytes ≈ 6.18 MB).

> Earlier releases also shipped a UPX-compressed `bin/gh_upx.exe` (~2 MB). Since v0.3.5 it is gone: the UPX + Go combination trips Windows Defender's ML heuristic (`Trojan:Win32/Wacatac.B!ml`, 5 detections observed), so users got a quarantined file right after extraction.
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

> The streaming format is SSE (`data: {...}` frames ending with `"done":true`), not NDJSON.
</details>

<details>
<summary><b>What's the difference between streaming and normal output?</b></summary>

- Normal: returns the complete JSON once the request finishes;
- Streaming (`stream: true`): pushes `content` chunk by chunk over SSE, ending with `data: [DONE]` — lower time-to-first-token, smoother experience;
- With `web_search`, one extra `phase:"search"` frame carrying `sources` precedes the content chunks;
- Thinking models additionally return `thinking` (upstream `reasoning_content`) in streaming mode; the **GUI** renders it in a collapsible panel, and the **CLI** folds it into one `💭 思考中… N 字` line (`/think on` expands it as dim grey text; under redirection only a one-line summary is printed) before the answer body.
</details>

<details>
<summary><b>Why does chatting with a GGUF model only return metadata?</b></summary>

This engine is positioned as "parse + gateway": GGUF provides metadata parsing (architecture, context length, vocabulary, etc.) but no local inference. For local inference, connect a backend like llama.cpp, or chat with a cloud model. Local `local/<name>` models also skip web-search context injection.
</details>

<details>
<summary><b>How do I let other devices on the LAN access it?</b></summary>

1. Change `server.host` in `config.yaml` to `0.0.0.0`;
2. Strongly recommended: set `gateway_api_key` at the same time — without it the engine **refuses to start** unless you pass `--allow-insecure`;
3. Other devices access it via `http://<your-IP>:8000`.

> Listening on `127.0.0.1` only is the default — this is an intentional security design.
</details>

### 🛠 Troubleshooting

<details>
<summary><b>Will usage_stats.json grow forever?</b></summary>

No, but the bound is in memory: the store keeps **at most 10000 records** and silently drops the oldest beyond that, then writes the trimmed list to disk on the next batch flush (every 50 new records). So the file is bounded by the in-memory cap, and a hard kill can lose up to the 49 most recent records. To start over, just delete the file; the engine recreates an empty store automatically and nothing else is affected.
</details>

<details>
<summary><b>Web search returns nothing or errors — what should I check?</b></summary>

Check in order:

1. `search.enabled: true` and the engine restarted — while disabled, both search routes return `404` by design;
2. `GET /v1/search/backends`: a `ready: false` entry carries the reason (missing `base_url`, unsupported `type`, or an unset `${ENV_VAR}`);
3. For SearXNG: the `json` format must be enabled in its `settings.yml`, and `base_url` must match where it actually listens;
4. `429` means the concurrency gate (`max_concurrent`) is saturated — retry later;
5. `503` means the backend could not be reached or its response could not be read.
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
3. If you set a non-loopback `server.host` without `gateway_api_key`, the engine refuses to start by design — set the key or pass `--allow-insecure` only on a trusted network;
4. Run `stop.bat`, then `start.bat` again; if it still fails, check the YAML syntax of `config.yaml` and `plugins/*/config.yaml` (indentation).
</details>

## Known Limitations

- **Windows only**: the repo ships only the Windows engine binary (`bin/gh.exe`); macOS/Linux users must build from source (`cd engine && go build`).
- **Never commit real API keys**: in `plugins/*/config.yaml`, prefer `${ENV_VAR}` notation; ensure real keys are not committed to version control.
- **YAML subset parser**: the hand-written YAML parser does not support flow style (`[]`/`{}`) or multi-line strings — only the shapes this project's config requires.
- **Binaries in repo**: `bin/` and `models/*.gguf` are committed for out-of-box use; use GitHub Releases to slim the repo if needed.
- **CLI thinking display is text-only**: `reasoning_content` is folded into a single `💭 思考中… N 字` line and expanded with `/think on` in dim grey — there is no syntax coloring inside the thinking text, and under stdout redirection only a one-line summary is printed.
- **Launcher port handling is inconsistent**: `start.py` reads `server.port` (via PyYAML, silently falling back to 8000 when PyYAML is missing), while `start.bat` / `restart.bat` / `stop.bat` hard-code port 8000 and ignore `server.port`. `start.py` always uses `bin\gh.exe` (no `gh.exe` preference and no fallback); `start.bat` never calls `start.py`.
- **Usage records are bounded and batched**: at most 10000 records are kept in memory (oldest silently dropped) and flushed every 50 entries, so a hard kill can lose up to 49 recent records.
- **No portable packaging**: `installer.iss` (Inno Setup) is the only packaging script; the desktop shortcut is created by default since v0.3.1 (previously unchecked by default and `PrivilegesRequired=lowest` means the default install goes to the current user's program directory.

---

*General Harness — a minimalist AI gateway. The core engine is fully decoupled from the UI, letting you switch seamlessly between different frontends.*

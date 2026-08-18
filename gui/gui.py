"""General Harness Webview GUI（Python）。

复用系统原生渲染引擎（Windows: Edge WebView2 via pywebview；
pywebview 不可用时回退到系统默认浏览器）。通过 HTTP 调用 Go 引擎 API，
核心逻辑与 UI 完全解耦。

用法:
    python gui.py [--url http://127.0.0.1:8000]
"""
from __future__ import annotations

import argparse
import json
import sys
import threading
import webbrowser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.request import Request, urlopen
from urllib.error import HTTPError

DEFAULT_ENGINE = "http://127.0.0.1:8000"

# 内嵌单文件前端（可折叠 Thinking 可视化界面，无外部资源）。
INDEX_HTML = r"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>General Harness</title>
<style>
:root { --bg:#1e2228; --panel:#262b33; --border:#3a3f45; --fg:#d8dee6;
        --muted:#8f959e; --accent:#5E81AC; --think:#8a6d3b; }
* { box-sizing:border-box; margin:0; padding:0; }
body { background:var(--bg); color:var(--fg); font:14px/1.6 "Segoe UI",sans-serif;
       display:flex; flex-direction:column; height:100vh; }
header { padding:10px 16px; border-bottom:1px solid var(--border);
         display:flex; gap:10px; align-items:center; flex-wrap:wrap; }
header h1 { font-size:16px; margin-right:auto; }
select, input, button { background:var(--panel); color:var(--fg);
        border:1px solid var(--border); border-radius:6px; padding:5px 10px; font-size:13px; }
button { cursor:pointer; } button:hover { border-color:var(--accent); }
button:disabled { opacity:.5; cursor:default; }
#chat { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:12px; }
.msg { max-width:82%; padding:10px 14px; border-radius:10px; white-space:pre-wrap; word-break:break-word; }
.user { align-self:flex-end; background:#234a6e; }
.assistant { align-self:flex-start; background:var(--panel); border:1px solid var(--border); }
details.think { margin-top:8px; border:1px dashed var(--think); border-radius:6px;
        padding:6px 10px; font-size:13px; color:#c9b98a; }
details.think summary { cursor:pointer; color:var(--think); }
details.think[open] summary { margin-bottom:6px; }
details.think div { white-space:pre-wrap; color:#a99e8a; }
#inputbar { display:flex; gap:10px; padding:12px 16px; border-top:1px solid var(--border); }
#input { flex:1; background:var(--panel); color:var(--fg); border:1px solid var(--border);
         border-radius:6px; padding:8px 12px; font-size:14px; resize:none; }
#status { padding:4px 16px; color:var(--muted); font-size:12px; border-top:1px solid var(--border); }
.hint { color:var(--muted); font-size:12px; }
</style>
</head>
<body>
<header>
  <h1>General Harness</h1>
  <label class="hint">模型</label>
  <select id="model"></select>
  <button id="refresh">刷新</button>
  <button id="clear">清空</button>
</header>
<div id="chat"></div>
<div id="status">就绪</div>
<div id="inputbar">
  <textarea id="input" rows="2" placeholder="输入消息，Enter 发送，Shift+Enter 换行"></textarea>
  <button id="send">发送</button>
</div>
<script>
const $ = s => document.querySelector(s);
const chat = $('#chat'), modelSel = $('#model'), input = $('#input');
const statusEl = $('#status'), sendBtn = $('#send');
let history = [];
const esc = s => s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');

async function api(path, opts) {
  const r = await fetch('/api'+path, Object.assign({headers:{'Content-Type':'application/json'}}, opts||{}));
  if (!r.ok) { let m = await r.text(); try { m = JSON.parse(m).error?.message || m; } catch(e){} throw new Error(m); }
  return r.json();
}
async function loadModels() {
  try {
    const d = await api('/models');
    modelSel.innerHTML = d.data.map(m => '<option>'+esc(m.id)+'</option>').join('');
    statusEl.textContent = '已加载 ' + d.data.length + ' 个模型';
  } catch(e) { statusEl.textContent = '模型加载失败: '+e.message; }
}
function addMsg(role, text, thinking) {
  const div = document.createElement('div');
  div.className = 'msg ' + role;
  if (thinking) {
    div.innerHTML = '<details class="think" open><summary>💭 思考过程</summary><div>'+esc(thinking)+'</div></details><div>'+esc(text)+'</div>';
  } else {
    div.textContent = text;
  }
  chat.appendChild(div);
  chat.scrollTop = chat.scrollHeight;
  return div;
}
function setBusy(b) { sendBtn.disabled = b; input.disabled = b; }
async function send() {
  const text = input.value.trim();
  if (!text || sendBtn.disabled) return;
  input.value = ''; setBusy(true); statusEl.textContent = '生成中…';
  addMsg('user', text);
  const bubble = addMsg('assistant', '');
  try {
    const r = await fetch('/v1/chat/completions', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({model: modelSel.value, messages: history.concat([{role:'user',content:text}]), stream: true, stream_options: {include_usage: true}})
    });
    if (!r.ok || !r.body) throw new Error('请求失败');
    const reader = r.body.getReader();
    const dec = new TextDecoder();
    let buf = '', full = '', think = '', inThink = false;
    bubble.textContent = '';
    while (true) {
      const {done, value} = await reader.read();
      if (done) break;
      buf += dec.decode(value, {stream:true});
      let idx;
      while ((idx = buf.indexOf('\n\n')) >= 0) {
        const line = buf.slice(0, idx); buf = buf.slice(idx+2);
        if (!line.startsWith('data:')) continue;
        const data = line.slice(5).trim();
        if (data === '[DONE]') continue;
        let ev; try { ev = JSON.parse(data); } catch(e) { continue; }
        if (ev.error) throw new Error(ev.error.message || '网关错误');
        if (ev.thinking !== undefined) { inThink = true; think += ev.thinking; }
        if (ev.content) { full += ev.content; inThink = false; }
        if (ev.content || think) {
          bubble.textContent = full;
          if (think) bubble.innerHTML = '<details class="think" open><summary>💭 思考过程</summary><div>'+esc(think)+'</div></details><div>'+esc(full)+'</div>';
          chat.scrollTop = chat.scrollHeight;
        }
      }
    }
    history = history.concat([{role:'user',content:text},{role:'assistant',content:full}]);
    statusEl.textContent = '完成';
  } catch(e) {
    bubble.textContent = '✗ ' + e.message;
    statusEl.textContent = '错误';
  }
  setBusy(false);
}
$('#send').onclick = send;
$('#clear').onclick = () => { history = []; chat.innerHTML = ''; };
$('#refresh').onclick = loadModels;
input.addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
});
loadModels();
</script>
</body>
</html>"""

# 代理：把 /api/* 转发到 Go 引擎（避免 CORS）。
class ProxyHandler(BaseHTTPRequestHandler):
    # 引擎地址在 run_proxy 中注入 server 实例，避免类变量跨实例污染。
    engine_url = DEFAULT_ENGINE

    def log_message(self, *a):  # 静默访问日志
        pass

    def _engine(self) -> str:
        server = getattr(self, "server", None)
        if server is not None and hasattr(server, "engine_url"):
            return server.engine_url
        return self.engine_url

    def _proxy(self, method: str) -> None:
        path = self.path
        if path == "/" or path == "/index.html":
            body = INDEX_HTML.encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        length = int(self.headers.get("Content-Length") or 0)
        data = self.rfile.read(length) if length else None
        url = self._engine() + path
        req = Request(url, data=data, method=method)
        for key in ("Content-Type", "Authorization", "X-Gateway-Api-Key"):
            if self.headers.get(key):
                req.add_header(key, self.headers[key])
        try:
            with urlopen(req, timeout=300) as resp:
                body = resp.read()
                self.send_response(resp.status)
                ct = resp.headers.get("Content-Type", "application/json")
                self.send_header("Content-Type", ct)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
        except HTTPError as e:
            body = e.read()
            self.send_response(e.code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except Exception as e:
            body = json.dumps({"error": {"message": "无法连接引擎: %s" % e, "type": "internal_error", "code": 500}}).encode()
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    do_GET = lambda self: self._proxy("GET")   # noqa: E731
    do_POST = lambda self: self._proxy("POST")  # noqa: E731


def run_proxy(engine_url: str, port: int) -> ThreadingHTTPServer:
    ProxyHandler.engine_url = engine_url
    server = ThreadingHTTPServer(("127.0.0.1", port), ProxyHandler)
    server.engine_url = engine_url  # 实例级配置，多实例互不干扰
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def main() -> int:
    ap = argparse.ArgumentParser(description="General Harness GUI")
    ap.add_argument("--url", default=DEFAULT_ENGINE, help="Go 引擎地址")
    ap.add_argument("--port", type=int, default=8765, help="本地前端端口")
    args = ap.parse_args()

    proxy = run_proxy(args.url, args.port)
    url = f"http://127.0.0.1:{args.port}/index.html"
    print(f"前端已启动: {url}（引擎: {args.url}）")

    try:
        import webview  # type: ignore

        def open_window() -> None:
            webview.create_window("General Harness", url, width=1000, height=700)
            webview.start()

        threading.Thread(target=open_window, daemon=True).start()
    except Exception:
        webbrowser.open(url)
        print("pywebview 不可用，已用系统浏览器打开")

    try:
        threading.Event().wait()
    except KeyboardInterrupt:
        pass
    proxy.shutdown()
    return 0


if __name__ == "__main__":
    sys.exit(main())

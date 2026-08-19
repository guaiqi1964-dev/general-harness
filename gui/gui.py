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
from urllib.parse import urlparse, parse_qs, quote

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
button.active { background:var(--accent); color:#fff; border-color:var(--accent); }
#usagePanel { margin:0 16px 12px; background:var(--panel); border:1px solid var(--border); border-radius:8px; padding:10px 14px; max-height:40vh; overflow-y:auto; }
#usagePanel .usage-head { display:flex; justify-content:space-between; align-items:center; margin-bottom:8px; font-weight:bold; }
#usagePanel .usage-head button { padding:2px 8px; }
.usage-row { display:flex; gap:12px; padding:4px 0; border-bottom:1px solid var(--border); font-size:13px; }
.usage-row span:nth-child(1) { flex:1; word-break:break-all; color:var(--muted); }
.usage-row span:nth-child(3) { color:var(--accent); white-space:nowrap; }
.usage-total { margin-top:8px; font-weight:bold; color:var(--accent); }
.hint { color:var(--muted); font-size:12px; }
.usage-range { display:flex; gap:8px; align-items:center; margin-bottom:8px; }
.usage-range label { color:var(--muted); font-size:12px; white-space:nowrap; }
#usageChart { width:100%; height:auto; display:block; margin-bottom:10px; border:1px solid var(--border); border-radius:6px; background:var(--bg); }
.usage-subhead { margin:8px 0 4px; font-weight:bold; color:var(--muted); font-size:12px; }
</style>
</head>
<body>
<header>
  <h1>General Harness</h1>
  <label class="hint">模型</label>
  <select id="model"></select>
  <button id="deep">🧠 深度思考</button>
  <button id="usage">📊 用量</button>
  <button id="refresh">刷新</button>
  <button id="clear">清空</button>
</header>
<div id="usagePanel" hidden>
  <div class="usage-head"><span>📊 Token 用量统计</span><button id="usageClose">✕</button></div>
  <div class="usage-range">
    <label>分时范围</label>
    <select id="usageRange">
      <option value="1h">最近 1 小时</option>
      <option value="1d">最近 1 天</option>
      <option value="7d">最近 7 天</option>
      <option value="30d">最近 30 天</option>
      <option value="total">全部</option>
    </select>
  </div>
  <img id="usageChart" alt="分时统计图">
  <div class="usage-subhead">历史对话</div>
  <div id="usageBody"></div>
</div>
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
const deepBtn = $('#deep'), usageBtn = $('#usage'), usagePanel = $('#usagePanel'), usageBody = $('#usageBody'), usageRange = $('#usageRange'), usageChart = $('#usageChart');
let history = [];
let deepThinking = false;
let sessionId = 'sess-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
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
function toggleDeep() {
  deepThinking = !deepThinking;
  deepBtn.classList.toggle('active', deepThinking);
  if (deepThinking) {
    const opt = [...modelSel.options].find(o => o.value.includes('reasoner'));
    if (opt) { modelSel.value = opt.value; statusEl.textContent = '深度思考已开启：' + opt.value; }
    else statusEl.textContent = '未找到推理模型，请在模型列表手动选择';
  } else {
    const opt = [...modelSel.options].find(o => /chat|plus|turbo/.test(o.value));
    if (opt) { modelSel.value = opt.value; statusEl.textContent = '深度思考已关闭：' + opt.value; }
    else statusEl.textContent = '深度思考已关闭';
  }
}
function loadChart() {
  usageChart.src = '/usage_chart?time_range=' + encodeURIComponent(usageRange.value);
}
async function loadUsage() {
  loadChart();
  try {
    const r = await fetch('/v1/usage/stats?limit=20');
    if (!r.ok) throw new Error('请求失败');
    const d = await r.json();
    const rows = (d.data || []).map(item => {
      const t = item.last_ts ? new Date(item.last_ts * 1000).toLocaleString() : '';
      return '<div class="usage-row"><span>' + esc(item.label || item.session_id || '') + '</span><span>' + esc(item.model_name || '') + '</span><span>' + item.tokens + ' tokens</span><span>' + t + '</span></div>';
    }).join('');
    const total = (d.data || []).reduce((s, x) => s + (x.tokens || 0), 0);
    usageBody.innerHTML = rows || '<div class="hint">暂无用量记录</div>';
    usageBody.innerHTML += '<div class="usage-total">总计：' + total + ' tokens</div>';
    usagePanel.hidden = false;
  } catch(e) {
    statusEl.textContent = '用量加载失败：' + e.message;
  }
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
      body: JSON.stringify({model: modelSel.value, messages: history.concat([{role:'user',content:text}]), stream: true, stream_options: {include_usage: true}, session_id: sessionId})
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
        // 仅在 thinking 为非空字符串时累加（引擎对无思考的块输出 null，
        // 直接 += 会把 null 转成字符串 "null" 污染思考面板）。
        if (typeof ev.thinking === 'string' && ev.thinking.length > 0) {
          inThink = true; think += ev.thinking;
        }
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
$('#clear').onclick = () => { history = []; chat.innerHTML = ''; sessionId = 'sess-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8); };
$('#refresh').onclick = loadModels;
$('#deep').onclick = toggleDeep;
$('#usage').onclick = loadUsage;
$('#usageClose').onclick = () => { usagePanel.hidden = true; };
$('#usageRange').onchange = loadChart;
input.addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
});
loadModels();
</script>
</body>
</html>"""


# ---- 分时统计图（纯 Python 生成 SVG，零第三方依赖） ----

def _esc_xml(s: str) -> str:
    return (s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
            .replace('"', "&quot;").replace("'", "&apos;"))


def _svg_placeholder(msg: str) -> str:
    return ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 660 160" '
            'style="width:100%;height:auto">'
            f'<text x="330" y="80" fill="#8f959e" font-size="13" '
            f'text-anchor="middle">{_esc_xml(msg)}</text></svg>')


def build_usage_svg(items) -> str:
    """把 /v1/usage/stats?time_range=... 的分桶数据画成竖向柱状图 SVG。"""
    if not items:
        return _svg_placeholder("暂无用量记录")

    W, H = 660, 240
    left, right, top, bottom = 46, 14, 18, 38
    plot_w = W - left - right
    plot_h = H - top - bottom
    n = len(items)

    max_tok = 0
    for it in items:
        try:
            v = int(it.get("tokens", 0))
        except (TypeError, ValueError):
            v = 0
        if v > max_tok:
            max_tok = v
    scale = max_tok if max_tok > 0 else 1

    s = [f'<rect x="0" y="0" width="{W}" height="{H}" fill="none"/>']
    ticks = ([(0.0, "0"), (0.5, str(max_tok // 2)), (1.0, str(max_tok))]
             if max_tok > 0 else [(0.0, "0")])
    for frac, lbl in ticks:
        y = top + plot_h * (1.0 - frac)
        s.append(f'<line x1="{left}" y1="{y:.1f}" x2="{W - right}" y2="{y:.1f}" stroke="#3a3f45" stroke-width="1"/>')
        s.append(f'<text x="{left - 6}" y="{y + 4:.1f}" fill="#8f959e" font-size="10" text-anchor="end">{_esc_xml(lbl)}</text>')

    slot = plot_w / n
    bar_w = max(4.0, slot * 0.62)
    step = max(1, (n + 11) // 12)  # 最多约 12 个 X 轴标签
    for i, it in enumerate(items):
        try:
            tok = int(it.get("tokens", 0))
        except (TypeError, ValueError):
            tok = 0
        h = plot_h * (tok / scale)
        x = left + slot * i + (slot - bar_w) / 2.0
        y = top + plot_h - h
        bh = h if h > 0 else 1.0
        s.append(f'<rect x="{x:.1f}" y="{y:.1f}" width="{bar_w:.1f}" height="{bh:.1f}" rx="2" fill="#5E81AC"/>')
        cx = x + bar_w / 2.0
        if tok > 0:
            s.append(f'<text x="{cx:.1f}" y="{y - 4:.1f}" fill="#d8dee6" font-size="10" text-anchor="middle">{tok}</text>')
        if i % step == 0 or i == n - 1:
            lbl = str(it.get("label", "") or "")
            s.append(f'<text transform="translate({cx:.1f},{H - 14}) rotate(-35)" text-anchor="end" x="0" y="0" fill="#8f959e" font-size="10">{_esc_xml(lbl)}</text>')
    return ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" '
            'style="width:100%%;height:auto">%s</svg>' % (W, H, "".join(s)))


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
        if path.startswith("/usage_chart"):
            self._usage_chart(path)
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
                self.send_response(resp.status)
                ct = resp.headers.get("Content-Type", "application/json")
                self.send_header("Content-Type", ct)
                if ct == "text/event-stream":
                    # SSE 流式：逐块转发，不整体缓冲
                    self.send_header("Transfer-Encoding", "chunked")
                    self.end_headers()
                    while True:
                        chunk = resp.read(4096)
                        if not chunk:
                            break
                        self.wfile.write(("%x\r\n" % len(chunk)).encode() + chunk + b"\r\n")
                        self.wfile.flush()
                    self.wfile.write(b"0\r\n\r\n")
                    self.wfile.flush()
                else:
                    body = resp.read()
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

    def _render_usage_svg(self, time_range: str) -> str:
        url = "%s/v1/usage/stats?time_range=%s" % (self._engine(), quote(time_range))
        try:
            with urlopen(url, timeout=30) as resp:
                data = json.loads(resp.read().decode("utf-8"))
            return build_usage_svg(data.get("data", []))
        except HTTPError as e:
            return _svg_placeholder("引擎返回错误: %d" % e.code)
        except Exception as e:  # noqa: BLE001
            return _svg_placeholder("无法加载数据: %s" % e)

    def _usage_chart(self, path: str) -> None:
        qs = parse_qs(urlparse(path).query)
        time_range = (qs.get("time_range") or ["1h"])[0]
        svg = self._render_usage_svg(time_range)
        body = svg.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "image/svg+xml; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
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

    # 代理服务器在后台线程运行，webview 必须在主线程（pywebview 要求）。
    proxy = run_proxy(args.url, args.port)
    url = f"http://127.0.0.1:{args.port}/index.html"
    print(f"前端已启动: {url}（引擎: {args.url}）")

    try:
        import webview  # type: ignore

        webview.create_window("General Harness", url, width=1000, height=700)
        webview.start()  # 主线程运行，阻塞直到窗口关闭
    except Exception:
        webbrowser.open(url)
        print("pywebview 不可用，已用系统浏览器打开")
        try:
            threading.Event().wait()
        except KeyboardInterrupt:
            pass
    finally:
        proxy.shutdown()
        proxy.server_close()  # 释放监听 socket，避免资源泄漏
    return 0


if __name__ == "__main__":
    sys.exit(main())

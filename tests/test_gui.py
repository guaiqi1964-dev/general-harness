"""发行版 Python 组件测试：GUI 代理、前端页面、启动器逻辑。"""

import json
import os
import sys
import threading
import time
import unittest
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


class TestGUIProxy(unittest.TestCase):
    """GUI 本地代理：前端页面 + 引擎 API 转发。"""

    @classmethod
    def setUpClass(cls):
        from gui import gui as gg
        cls.gg = gg
        # 起一个假的"引擎"（本地 HTTP 服务模拟，避免依赖真实引擎）
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

        class FakeEngine(BaseHTTPRequestHandler):
            def log_message(self, *a):
                pass

            def do_GET(self):
                if self.path == "/health":
                    body = b'{"status":"ok"}'
                elif self.path in ("/v1/models", "/api/models"):
                    body = json.dumps({"object": "list", "data": [
                        {"id": "fake/model", "object": "model", "owned_by": "fake",
                         "api_keys": [], "vision": False}]}).encode()
                else:
                    body = b'{"ok":true}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def do_POST(self):
                body = b'{"id":"r1","content":"pong"}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        cls.fake = ThreadingHTTPServer(("127.0.0.1", 0), FakeEngine)
        threading.Thread(target=cls.fake.serve_forever, daemon=True).start()
        cls.fake_port = cls.fake.server_address[1]
        cls.proxy = gg.run_proxy(f"http://127.0.0.1:{cls.fake_port}", 0)
        cls.proxy_port = cls.proxy.server_address[1]
        # 等待代理线程就绪
        deadline = time.time() + 5
        while time.time() < deadline:
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{cls.proxy_port}/health", timeout=1):
                    break
            except Exception:
                time.sleep(0.1)

    @classmethod
    def tearDownClass(cls):
        cls.proxy.shutdown()
        cls.proxy.server_close()
        cls.fake.shutdown()
        cls.fake.server_close()

    def _get(self, path):
        with urllib.request.urlopen(f"http://127.0.0.1:{self.proxy_port}{path}", timeout=5) as r:
            return r.status, r.read()

    def test_index_page(self):
        status, body = self._get("/index.html")
        self.assertEqual(status, 200)
        html = body.decode("utf-8")
        self.assertIn("思考过程", html)  # 可折叠思考界面
        self.assertIn("details class=\"think\"", html)

    def test_proxy_models(self):
        status, body = self._get("/api/models")
        self.assertEqual(status, 200)
        data = json.loads(body)
        self.assertEqual(data["data"][0]["id"], "fake/model")

    def test_proxy_health(self):
        status, body = self._get("/health")
        self.assertEqual(status, 200)
        self.assertEqual(json.loads(body)["status"], "ok")

    def test_proxy_post(self):
        req = urllib.request.Request(
            f"http://127.0.0.1:{self.proxy_port}/v1/chat/completions",
            data=b'{"model":"x","messages":[]}', headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=5) as r:
            self.assertEqual(r.status, 200)
            self.assertEqual(json.loads(r.read())["content"], "pong")

    def test_engine_down_returns_502(self):
        gg = self.gg
        bad = gg.run_proxy("http://127.0.0.1:1", 0)  # 不可达端口
        try:
            with self.assertRaises(urllib.error.HTTPError) as ctx:
                urllib.request.urlopen(f"http://127.0.0.1:{bad.server_address[1]}/health", timeout=5)
            self.assertEqual(ctx.exception.code, 502)
            ctx.exception.close()  # 释放 HTTPError 持有的连接，避免资源泄漏
        finally:
            bad.shutdown()
            bad.server_close()


class TestIndexContent(unittest.TestCase):
    """前端单文件内容完整性。"""

    def test_thinking_fold(self):
        from gui import gui as gg
        self.assertIn("<details class=\"think\"", gg.INDEX_HTML)
        self.assertIn("思考过程", gg.INDEX_HTML)
        self.assertIn("v1/chat/completions", gg.INDEX_HTML)
        self.assertIn("stream_options", gg.INDEX_HTML)

    def test_single_file_no_external(self):
        from gui import gui as gg
        # 无外部 CSS/JS 引用（完全自包含单文件）
        self.assertNotIn("http://", gg.INDEX_HTML.replace("http://127.0.0.1", ""))


if __name__ == "__main__":
    unittest.main()

"""General Harness 发行版启动器（纯 CLI 版）。

用法（在发行版根目录）：
    python start.py                # 启动 Go 引擎 + 终端 CLI 模式
    python start.py --server       # 仅启动 Go 引擎（HTTP API 服务）
    python start.py --port 8000    # 指定引擎端口

架构：核心引擎为 Go 单二进制（bin/gh_upx.exe），CLI 内嵌于引擎，
整个程序不需要 Python 也能运行——`bin\\gh_upx.exe` 双击或直接执行即进入 CLI。
本启动器只是给"习惯用 python 起服务"的人一个包装，且它是可选的。
"""
from __future__ import annotations

import os
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def pick_bin() -> Path:
    """优先用 UPX 压缩版（体积最小），缺失时回退未压缩版。"""
    names = ("gh_upx.exe", "gh.exe") if os.name == "nt" else ("gh_upx", "gh")
    for name in names:
        p = ROOT / "bin" / name
        if p.exists():
            return p
    return ROOT / "bin" / names[0]


BIN = pick_bin()


def load_port() -> int:
    try:
        import yaml
        cfg = yaml.safe_load((ROOT / "config.yaml").read_text(encoding="utf-8")) or {}
        return int((cfg.get("server") or {}).get("port") or 8000)
    except Exception:
        return 8000


def engine_healthy(base: str, timeout: float = 1.0) -> bool:
    try:
        with urllib.request.urlopen(f"{base}/health", timeout=timeout) as r:
            return r.status == 200
    except Exception:
        return False


def stop(proc: subprocess.Popen) -> None:
    try:
        proc.terminate()
        proc.wait(timeout=5)
    except Exception:
        try:
            proc.kill()
        except Exception:
            pass


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    mode = "cli"          # 默认：引擎 + CLI
    port = load_port()
    if "--server" in args:
        mode = "server"
        args.remove("--server")
    if "--gui" in args:
        # 纯 CLI 分支已移除 GUI；这里明确提示而不是静默忽略。
        print("[发行版] 这个版本不含 GUI（纯 CLI 分支）。直接运行 bin\\gh_upx.exe 或 python start.py 即可进入终端。")
        args.remove("--gui")
    for i, a in enumerate(args):
        if a == "--port" and i + 1 < len(args):
            port = int(args[i + 1])

    base = f"http://127.0.0.1:{port}"
    engine_proc: subprocess.Popen | None = None

    # ---- 1) 引擎：未运行则启动 ----
    if engine_healthy(base):
        print(f"[发行版] 检测到引擎已在运行（{base}），复用。")
    else:
        if not BIN.exists():
            print(f"[发行版] 未找到引擎二进制: {BIN}，请先构建（见 README）。")
            return 1
        print(f"[发行版] 正在启动 Go 引擎（{base}）...")
        engine_proc = subprocess.Popen([str(BIN), "serve", "--port", str(port)],
                                       cwd=str(ROOT))
        deadline = time.time() + 30
        while time.time() < deadline:
            if engine_healthy(base):
                print("[发行版] 引擎就绪。")
                break
            if engine_proc.poll() is not None:
                print("[发行版] 引擎进程已退出，请检查日志。")
                return 1
            time.sleep(0.5)
        else:
            print("[发行版] 等待引擎就绪超时。")
            stop(engine_proc)
            return 1

    # ---- 2) 按模式启动前端 ----
    try:
        if mode == "cli":
            print("[发行版] 进入终端 CLI 模式（Ctrl+C 中断当前回复，再按一次退出）。")
            subprocess.run([str(BIN), "chat"], cwd=str(ROOT))
        else:  # server
            print(f"[发行版] 引擎服务运行中: {base}（Ctrl+C 退出）")
            while True:
                time.sleep(3600)
    except KeyboardInterrupt:
        pass

    # ---- 3) 收尾 ----
    if engine_proc is not None and engine_proc.poll() is None:
        stop(engine_proc)
        print("[发行版] 引擎已关闭。")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)

"""General Harness 发行版启动器。

用法（在发行版根目录）：
    python start.py                # 启动 Go 引擎 + 终端 CLI 模式
    python start.py --gui          # 启动 Go 引擎 + Webview GUI 模式
    python start.py --server       # 仅启动 Go 引擎（HTTP API 服务）
    python start.py --port 8000    # 指定引擎端口

架构：核心引擎为 Go 单二进制（bin/gh.exe），CLI 内嵌于引擎；
GUI 为 Python（pywebview/浏览器），通过 HTTP 调用引擎 API。

静默启动：GUI 模式建议用 pythonw 运行（`pythonw start.py --gui`）。
引擎子进程以 CREATE_NO_WINDOW 启动、日志写入 engine.log，GUI 子进程
用 pythonw 启动，因此桌面上只会出现一个 pywebview 窗口，不会弹出
任何命令行窗口。没有控制台时，错误用系统消息框提示。
"""
from __future__ import annotations

import os
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent
BIN = ROOT / "bin" / ("gh.exe" if os.name == "nt" else "gh")
ENGINE_LOG = ROOT / "engine.log"
# 让引擎子进程不创建控制台窗口（Windows）。其它平台为 0（无影响）。
CREATE_NO_WINDOW = 0x08000000 if os.name == "nt" else 0


def log(msg: str) -> None:
    """pythonw 下 sys.stdout / sys.stderr 可能为 None，写入前必须先判断。"""
    for stream in (sys.stdout, sys.stderr):
        if stream is not None:
            try:
                print(msg, file=stream, flush=True)
            except Exception:
                pass
            return


def show_error(msg: str) -> None:
    """写入日志；无控制台（pythonw）时改用系统消息框，避免错误被静默吞掉。"""
    log(msg)
    if os.name == "nt" and sys.stdout is None:
        try:
            import ctypes

            ctypes.windll.user32.MessageBoxW(0, msg, "General Harness", 0x10)
        except Exception:
            pass


def pythonw_exe() -> str:
    """优先返回同目录的 pythonw.exe，避免 GUI 子进程再开一个控制台。"""
    exe = Path(sys.executable)
    if os.name == "nt" and exe.name.lower() == "python.exe":
        candidate = exe.with_name("pythonw.exe")
        if candidate.exists():
            return str(candidate)
    return str(exe)


def gui_proxy_up(port: int = 8765) -> bool:
    """GUI 自身起的本地代理（默认 8765）是否已在响应。"""
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/", timeout=1.5) as r:
            return r.status == 200
    except Exception:
        return False


def has_pywebview() -> bool:
    try:
        import webview  # noqa: F401
        return True
    except Exception:
        return False


def open_browser(url: str) -> None:
    try:
        import webbrowser

        webbrowser.open(url)
    except Exception:
        pass


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


def spawn_engine(port: int) -> subprocess.Popen:
    """以无控制台窗口的方式启动引擎，stdout/stderr 追加到 engine.log。"""
    append = "ab"
    fh = open(ENGINE_LOG, append, buffering=0)
    try:
        return subprocess.Popen(
            [str(BIN), "serve", "--port", str(port)],
            cwd=str(ROOT),
            stdin=subprocess.DEVNULL,
            stdout=fh,
            stderr=subprocess.STDOUT,
            creationflags=CREATE_NO_WINDOW,
        )
    finally:
        # 子进程持有自己的句柄副本，父进程可以立即关闭
        fh.close()


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    mode = "cli"          # 默认：引擎 + CLI
    port = load_port()
    if "--gui" in args:
        mode = "gui"
        args.remove("--gui")
    if "--server" in args:
        mode = "server"
        args.remove("--server")
    for i, a in enumerate(args):
        if a == "--port" and i + 1 < len(args):
            port = int(args[i + 1])

    base = f"http://127.0.0.1:{port}"
    engine_proc: subprocess.Popen | None = None

    # ---- 1) 引擎：未运行则启动（无窗口） ----
    if engine_healthy(base):
        log(f"[发行版] 检测到引擎已在运行（{base}），复用。")
    else:
        if not BIN.exists():
            show_error(f"未找到引擎二进制：{BIN}\n请先构建或重新安装（见 README）。")
            return 1
        log(f"[发行版] 正在启动 Go 引擎（{base}），日志：{ENGINE_LOG}")
        engine_proc = spawn_engine(port)
        deadline = time.time() + 30
        while time.time() < deadline:
            if engine_healthy(base):
                log("[发行版] 引擎就绪。")
                break
            if engine_proc.poll() is not None:
                show_error(f"引擎进程已退出，请查看日志：{ENGINE_LOG}")
                return 1
            time.sleep(0.5)
        else:
            log("[发行版] 等待引擎就绪超时。")
            stop(engine_proc)
            show_error(f"引擎启动超时，请查看日志：{ENGINE_LOG}")
            return 1

    # ---- 2) 按模式启动前端 ----
    try:
        if mode == "cli":
            log("[发行版] 进入终端 CLI 模式（Ctrl+C 退出）。")
            subprocess.run([str(BIN), "chat"], cwd=str(ROOT))
        elif mode == "gui":
            gui_script = ROOT / "gui" / "gui.py"
            # GUI 已在运行：不要重复开窗，也别把我们刚拉起的引擎杀掉
            if gui_proxy_up():
                engine_proc = None
                log("[发行版] GUI 已在运行（http://127.0.0.1:8765），不重复启动。")
                return 0
            # 没有 pywebview（或缺少 GUI 脚本）：回退到引擎内置页面
            if not gui_script.exists() or not has_pywebview():
                engine_proc = None          # 引擎留给浏览器页面使用
                show_error("未检测到 pywebview（或缺少 gui/gui.py）。\n"
                           f"已改用引擎内置页面：{base}/ui")
                open_browser(f"{base}/ui")
                return 0
            log("[发行版] 启动 Webview GUI 模式（pythonw，无控制台窗口）...")
            rc = subprocess.run([pythonw_exe(), str(gui_script), "--url", base],
                                cwd=str(ROOT)).returncode
            if rc != 0:
                log(f"[发行版] GUI 退出码 {rc}（若窗口未出现，请改用 {base}/ui）")
        else:  # server
            log(f"[发行版] 引擎服务运行中: {base}（Ctrl+C 退出）")
            while True:
                time.sleep(3600)
    except KeyboardInterrupt:
        pass

    # ---- 3) 收尾 ----
    if engine_proc is not None and engine_proc.poll() is None:
        stop(engine_proc)
        log("[发行版] 引擎已关闭。")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)

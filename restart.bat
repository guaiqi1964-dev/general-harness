@echo off
setlocal enabledelayedexpansion
title General Harness - Restart
cd /d "%~dp0"
set "ENGINE=%~dp0bin\gh_upx.exe"
set PORT=8000
if not exist "%ENGINE%" set "ENGINE=%~dp0bin\gh.exe"

rem ---- Parse mode arg: default gui (GUI); optional cli / server ----
set MODE=gui
if /i "%~1"=="cli"    set MODE=cli
if /i "%~1"=="server" set MODE=server

rem ---- Load user-level env vars from registry (guarantees engine gets them) ----
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v DEEPSEEK_API_KEY 2^>nul') do set "DEEPSEEK_API_KEY=%%a"
)
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTPS_PROXY 2^>nul') do set "HTTPS_PROXY=%%a"
)
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTP_PROXY 2^>nul') do set "HTTP_PROXY=%%a"
)

echo =============================================
echo   General Harness - One-click Restart
echo   Port: %PORT%    Mode: %MODE%
echo   API Key: %DEEPSEEK_API_KEY:~0,6%... (from registry)
echo =============================================

echo [STOP] Stopping old engine...
if exist "%CD%\gateway.pid" (
    set /p OLDPID=<"%CD%\gateway.pid"
    if defined OLDPID (
        echo [STOP] Killing process PID !OLDPID!
        taskkill /F /PID !OLDPID! >nul 2>&1
    )
    del /q "%CD%\gateway.pid" >nul 2>&1
)
for /f "tokens=5" %%p in ('netstat -ano ^| findstr ":%PORT% .*LISTENING"') do (
    if not "%%p"=="0" (
        echo [STOP] Killing port %PORT% owner PID %%p
        taskkill /F /PID %%p >nul 2>&1
    )
)
timeout /t 1 /nobreak >nul

if not exist "%ENGINE%" (
    echo [ERROR] Engine binary not found: bin\gh_upx.exe / bin\gh.exe
    pause & exit /b 1
)
echo [START] Starting engine...
start "General Harness Engine" cmd /k ""%ENGINE%" serve --port %PORT%"
set /a TRY=0
:wait_loop
set /a TRY+=1
if !TRY! gtr 30 (
    echo [ERROR] Engine start timeout. Port %PORT% may be in use.
    pause & exit /b 1
)
curl -s -o nul -w "%%{http_code}" --max-time 2 "http://127.0.0.1:%PORT%/health" > "%TEMP%\gh_r.txt" 2>nul
set /p CODE=<"%TEMP%\gh_r.txt"
if not "!CODE!"=="200" ( timeout /t 1 /nobreak >nul & goto :wait_loop )
echo [DONE] Engine restarted at http://127.0.0.1:%PORT%

if "%MODE%"=="server" (
    echo [MODE] Server-only mode. Engine runs in its own window.
    pause
    exit /b 0
)
if "%MODE%"=="cli" (
    echo [MODE] Terminal CLI mode. Type /exit or Ctrl+C to quit.
    "%ENGINE%" chat
    exit /b 0
)

rem ---- Default mode: Webview GUI ----
echo [MODE] Starting Webview GUI...
python --version >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Python not found. GUI mode requires Python 3.10+.
    echo         Try: restart.bat cli  ^(terminal^)  or  install Python.
    pause & exit /b 1
)
start "General Harness GUI" cmd /k "cd /d %~dp0 && python gui\gui.py --url http://127.0.0.1:%PORT%"
exit /b 0

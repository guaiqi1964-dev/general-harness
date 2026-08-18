@echo off
setlocal enabledelayedexpansion
title General Harness - Start
cd /d "%~dp0"

set "ENGINE=%~dp0bin\gh_upx.exe"
set PORT=8000
if not exist "%ENGINE%" set "ENGINE=%~dp0bin\gh.exe"
if not exist "%ENGINE%" (
    echo [ERROR] Engine binary not found: bin\gh_upx.exe / bin\gh.exe
    pause & exit /b 1
)

rem ---- Parse mode arg: default gui (GUI); optional cli / server ----
set MODE=gui
if /i "%~1"=="cli"    set MODE=cli
if /i "%~1"=="server" set MODE=server

rem ---- Load user-level env vars from registry (fresh windows inherit them,
rem       but stale windows don't; this guarantees the engine always gets them) ----
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v DEEPSEEK_API_KEY 2^>nul') do set "DEEPSEEK_API_KEY=%%a"
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTPS_PROXY 2^>nul') do set "HTTPS_PROXY=%%a"
)
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTP_PROXY 2^>nul') do set "HTTP_PROXY=%%a"
)

echo =============================================
echo   General Harness - One-click Start
echo   Engine: %ENGINE%
echo   Port:   %PORT%
echo   Mode:   %MODE%
echo   API Key: %DEEPSEEK_API_KEY:~0,6%... (from registry)
echo =============================================

call :check_health
if "%HEALTHY%"=="1" (
    echo [ENGINE] Already running (http://127.0.0.1:%PORT%)
) else (
    echo [ENGINE] Starting...
    start "General Harness Engine" cmd /k ""%ENGINE%" serve --port %PORT%"
    set /a TRY=0
    :wait_loop
    set /a TRY+=1
    if !TRY! gtr 30 (
        echo [ERROR] Engine start timeout. Port %PORT% may be in use.
        pause & exit /b 1
    )
    call :check_health
    if not "!HEALTHY!"=="1" ( timeout /t 1 /nobreak >nul & goto :wait_loop )
    echo [ENGINE] Ready at http://127.0.0.1:%PORT%
)

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
    echo         Try: start.bat cli  ^(terminal^)  or  install Python.
    pause & exit /b 1
)
start "General Harness GUI" cmd /k "cd /d %~dp0 && python gui\gui.py --url http://127.0.0.1:%PORT%"
exit /b 0

:check_health
set HEALTHY=0
curl -s -o nul -w "%%{http_code}" --max-time 2 "http://127.0.0.1:%PORT%/health" > "%TEMP%\gh_health.txt" 2>nul
set /p CODE=<"%TEMP%\gh_health.txt"
if "%CODE%"=="200" set HEALTHY=1
exit /b 0

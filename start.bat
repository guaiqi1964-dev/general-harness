@echo off
setlocal enabledelayedexpansion
title General Harness - Start (CLI)
cd /d "%~dp0"

set "ENGINE=%~dp0bin\gh_upx.exe"
set PORT=8000
if not exist "%ENGINE%" set "ENGINE=%~dp0bin\gh.exe"
if not exist "%ENGINE%" (
    echo [ERROR] Engine binary not found: bin\gh_upx.exe / bin\gh.exe
    pause & exit /b 1
)

rem ---- Parse mode arg: default cli; optional server ----
set MODE=cli
if /i "%~1"=="server" set MODE=server
if /i "%~1"=="gui" (
    echo [WARN] This build is CLI-only ^(no webview/web UI^). Starting the terminal CLI instead.
    set MODE=cli
)

rem ---- Load user-level env vars from registry (fresh windows inherit them,
rem       but stale windows don't; this guarantees the engine always gets them) ----
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v DEEPSEEK_API_KEY 2^>nul') do set "DEEPSEEK_API_KEY=%%a"
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTPS_PROXY 2^>nul') do set "HTTPS_PROXY=%%a"
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTP_PROXY 2^>nul') do set "HTTP_PROXY=%%a"

echo =============================================
echo   General Harness - One-click Start (CLI)
echo   Engine: %ENGINE%
echo   Port:   %PORT%
echo   Mode:   %MODE%
if defined DEEPSEEK_API_KEY (echo   API Key: configured [from registry]) else (echo   API Key: not configured)
echo =============================================

call :check_health
if "%HEALTHY%"=="1" (
    echo [ENGINE] Already running - http://127.0.0.1:%PORT%
) else (
    echo [ENGINE] Starting...
    start "General Harness Engine" /D "%~dp0" cmd /k ""%ENGINE%" serve --port %PORT%"
    set /a TRY=0
    :wait_loop
    set /a TRY+=1
    if !TRY! gtr 30 (
        echo [ERROR] Engine start timeout. Port %PORT% may be in use.
        pause & exit /b 1
    )
    call :check_health
    if not "!HEALTHY!"=="1" ( ping -n 2 127.0.0.1 >nul & goto :wait_loop )
    echo [ENGINE] Ready at http://127.0.0.1:%PORT%
)

if "%MODE%"=="server" (
    echo [MODE] Server-only mode. Engine runs in its own window; close it to stop.
    exit /b 0
)

echo [MODE] Terminal CLI mode. Type /exit or Ctrl+C to quit.
"%ENGINE%" chat
exit /b 0

:check_health
set HEALTHY=0
curl -s -o nul -w "%%{http_code}" --max-time 2 "http://127.0.0.1:%PORT%/health" > "%TEMP%\gh_health.txt" 2>nul
set /p CODE=<"%TEMP%\gh_health.txt"
if "%CODE%"=="200" set HEALTHY=1
exit /b 0

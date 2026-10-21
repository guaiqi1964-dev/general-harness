@echo off
setlocal enabledelayedexpansion
title General Harness - Start
cd /d "%~dp0"

set "ENGINE=%~dp0bin\gh.exe"
set PORT=8000
if not exist "%ENGINE%" set "ENGINE=%~dp0bin\gh_upx.exe"

rem ---- Parse mode arg: default gui (GUI); optional cli / server ----
set MODE=gui
if /i "%~1"=="cli"    set MODE=cli
if /i "%~1"=="server" set MODE=server

rem ---- Load user-level env vars from registry (fresh windows inherit them,
rem       but stale windows don't; this guarantees the engine always gets them) ----
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v DEEPSEEK_API_KEY 2^>nul') do set "DEEPSEEK_API_KEY=%%a"
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTPS_PROXY 2^>nul') do set "HTTPS_PROXY=%%a"
for /f "tokens=3" %%a in ('reg query "HKCU\Environment" /v HTTP_PROXY 2^>nul') do set "HTTP_PROXY=%%a"

rem ---- GUI mode: silent start (no console windows at all) ----
if "%MODE%"=="gui" goto :gui_mode

echo =============================================
echo   General Harness - One-click Start
echo   Engine: %ENGINE%
echo   Port:   %PORT%
echo   Mode:   %MODE%
if defined DEEPSEEK_API_KEY (echo   API Key: configured [from registry]) else (echo   API Key: not configured)
echo =============================================

call :start_engine
if errorlevel 1 ( pause & exit /b 1 )

if "%MODE%"=="server" (
    echo [MODE] Server-only mode. Engine runs in its own window.
    pause
    exit /b 0
)

echo [MODE] Terminal CLI mode. Type /exit or Ctrl+C to quit.
"%ENGINE%" chat
exit /b 0

rem ============================================================
rem  GUI mode: open NO console window; only the pywebview window.
rem   - engine started by start.py with CREATE_NO_WINDOW, log -> engine.log
rem   - GUI started by pythonw (no console)
rem  This very cmd window exits immediately.
rem ============================================================
:gui_mode
rem ---- Hand over to start.py via pythonw (no console window) ----
rem  start.py does the rest: reuses a running GUI, detects pywebview (falls back
rem  to the built-in page), and starts the engine with CREATE_NO_WINDOW
rem  (log -> engine.log). This window exits at once, so nothing stays on screen.
where pythonw >nul 2>&1
if errorlevel 1 goto :gui_fallback
start "" pythonw "%~dp0start.py" --gui --port %PORT%
exit /b 0

:gui_fallback
rem ---- No Python (or no pywebview): serve the built-in page from the engine ----
echo [MODE] Python GUI not available - using the built-in browser UI.
call :start_engine
if errorlevel 1 ( pause & exit /b 1 )
echo        http://127.0.0.1:%PORT%/ui
start "" "http://127.0.0.1:%PORT%/ui"
exit /b 0

:start_engine
call :check_health
if "%HEALTHY%"=="1" (
    echo [ENGINE] Already running - http://127.0.0.1:%PORT%
    exit /b 0
)
if not exist "%ENGINE%" (
    echo [ERROR] Engine binary not found: bin\gh.exe
    exit /b 1
)
echo [ENGINE] Starting...
start "General Harness Engine" /D "%~dp0" cmd /k ""%ENGINE%" serve --port %PORT%"
set /a TRY=0
:wait_engine
set /a TRY+=1
if !TRY! gtr 30 (
    echo [ERROR] Engine start timeout. Port %PORT% may be in use.
    exit /b 1
)
call :check_health
if not "!HEALTHY!"=="1" ( ping -n 2 127.0.0.1 >nul & goto :wait_engine )
echo [ENGINE] Ready at http://127.0.0.1:%PORT%
exit /b 0

:check_health
set HEALTHY=0
curl -s -o nul -w "%%{http_code}" --max-time 2 "http://127.0.0.1:%PORT%/health" > "%TEMP%\gh_health.txt" 2>nul
set /p CODE=<"%TEMP%\gh_health.txt"
if "%CODE%"=="200" set HEALTHY=1
exit /b 0

@echo off
setlocal enabledelayedexpansion
title General Harness - Stop
cd /d "%~dp0"

set PORT=8000
echo =============================================
echo   General Harness - Stop Engine (port %PORT%)
echo =============================================

set STOPPED=0
if exist "%CD%\gateway.pid" (
    set /p OLDPID=<"%CD%\gateway.pid"
    if defined OLDPID (
        taskkill /F /PID !OLDPID! >nul 2>&1
        if not errorlevel 1 (
            echo [STOP] Killed process PID !OLDPID!
            set STOPPED=1
        )
    )
    del /q "%CD%\gateway.pid" >nul 2>&1
)
for /f "tokens=5" %%p in ('netstat -ano ^| findstr ":%PORT% .*LISTENING"') do (
    if not "%%p"=="0" (
        taskkill /F /PID %%p >nul 2>&1
        if not errorlevel 1 (
            echo [STOP] Killed port %PORT% owner PID %%p
            set STOPPED=1
        )
    )
)

if "%STOPPED%"=="1" (
    echo [DONE] Engine stopped.
) else (
    echo [INFO] No running engine detected (port %PORT% is free).
)
pause
exit /b 0

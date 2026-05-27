@echo off
setlocal enabledelayedexpansion

:: ============================================================
:: Morgue — Build & Package Script
:: Builds the production binary and copies it to dist\ folder
:: ============================================================

set "ROOT=%~dp0"
set "DIST=%ROOT%dist"
set "FRONTEND=%ROOT%frontend"

echo ==============================
echo  Morgue Build Script
echo ==============================
echo.

:: --- Get version info from git ---
for /f "tokens=*" %%v in ('git describe --tags --always --dirty 2^>nul') do set "VERSION=%%v"
if not defined VERSION set "VERSION=dev"

for /f "tokens=*" %%c in ('git rev-parse --short HEAD 2^>nul') do set "COMMIT=%%c"
if not defined COMMIT set "COMMIT=none"

echo Version: %VERSION%
echo Commit:  %COMMIT%
echo.

:: --- Kill stale processes that may lock files ---
taskkill /f /im morgue.exe >nul 2>&1
taskkill /f /im morgue-dev.exe >nul 2>&1
echo Killed stale processes (if any)
echo.

:: --- Step 1: Regenerate Wails bindings ---
echo [1/4] Regenerating Wails bindings...
if exist "%ROOT%frontend\bindings" rmdir /s /q "%ROOT%frontend\bindings"
wails3 generate bindings -d "%TEMP%\morgue-bindings" ./...
if errorlevel 1 (
    echo ERROR: bindings generation failed
    exit /b 1
)
xcopy /e /i /q /y "%TEMP%\morgue-bindings" "%ROOT%frontend\bindings" >nul
rmdir /s /q "%TEMP%\morgue-bindings"
echo       Bindings OK
echo.

:: --- Step 2: Build frontend ---
echo [2/4] Building frontend...
pushd "%FRONTEND%"
call npm install --silent
if errorlevel 1 (
    echo ERROR: npm install failed
    popd
    exit /b 1
)
call npm run build
if errorlevel 1 (
    echo ERROR: frontend build failed
    popd
    exit /b 1
)
popd
echo       Frontend OK
echo.

:: --- Step 3: Generate Windows icon resource ---
echo [3/4] Generating icon resource...
pushd "%ROOT%cmd\morgue"
go-winres simply --icon appicon.png >nul 2>&1
if errorlevel 1 (
    echo WARNING: go-winres not found or failed. EXE will have no icon.
    echo Install: go install github.com/tc-hib/go-winres@latest
) else (
    echo       Icon resource OK
)
popd
echo.

:: --- Step 4: Build Go binary ---
echo [4/4] Building morgue.exe...
:: Clean only the binary, preserve config and other files
if not exist "%DIST%" mkdir "%DIST%"
if exist "%DIST%\morgue.exe" del /f "%DIST%\morgue.exe"

go build -ldflags "-s -w -X main.Version=%VERSION% -X main.Commit=%COMMIT%" -o "%DIST%\morgue.exe" ./cmd/morgue
if errorlevel 1 (
    echo ERROR: go build failed
    exit /b 1
)
echo       Binary OK
echo.

echo.
echo ==============================
echo  Build complete!
echo ==============================
echo.
echo Output: %DIST%\
echo.
echo Contents:
dir /b "%DIST%"
echo.
echo Tools will be downloaded automatically on first run.
echo Run dist\morgue.exe to test.

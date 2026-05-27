@echo off
setlocal enabledelayedexpansion

:: ============================================================
:: Morgue — Test Runner
:: Copies build from dist\ into isolated testbed\ and launches
:: All tools, configs, artifacts stay inside testbed\
:: ============================================================

set "ROOT=%~dp0"
set "DIST=%ROOT%dist"
set "TESTBED=%ROOT%testbed"

:: --- Check dist exists ---
if not exist "%DIST%\morgue.exe" (
    echo ERROR: dist\morgue.exe not found. Run build.bat first.
    exit /b 1
)

:: --- Prepare testbed ---
echo ==============================
echo  Morgue Test Runner
echo ==============================
echo.

if not exist "%TESTBED%" (
    mkdir "%TESTBED%"
    echo Created testbed\
)

:: Always copy latest binary and config
copy /y "%DIST%\morgue.exe" "%TESTBED%\" >nul
echo Copied morgue.exe

if exist "%DIST%\morgue.yaml" (
    copy /y "%DIST%\morgue.yaml" "%TESTBED%\" >nul
    echo Copied morgue.yaml
)

echo.
echo Testbed: %TESTBED%\
echo.
echo Contents:
dir /b "%TESTBED%"
echo.

:: --- Launch from testbed ---
echo Launching morgue.exe from testbed...
echo ==============================
echo.

pushd "%TESTBED%"
start "" "%TESTBED%\morgue.exe" %*
popd

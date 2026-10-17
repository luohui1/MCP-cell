@echo off
cd /d "%~dp0"
py -3 -c "import sys; assert sys.version_info >= (3,11); import tomlkit, psutil" >nul 2>nul
if errorlevel 1 (
  echo Python 3.11+ and dependencies are required.
  echo Run: py -3 -m pip install -r requirements.txt
  pause
  exit /b 1
)
py -3 -m mcpcell --open %*
if errorlevel 1 pause

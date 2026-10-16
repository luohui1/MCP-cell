@echo off
cd /d "%~dp0"
py -3 -m mcpcell --open %*
if errorlevel 1 pause

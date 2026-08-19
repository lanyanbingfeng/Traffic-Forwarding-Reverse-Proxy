@echo off
chcp 65001 >nul
title TunnelProxy - Live Access Logs
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0launch-flclash-server.ps1" -Mode console
if errorlevel 1 pause


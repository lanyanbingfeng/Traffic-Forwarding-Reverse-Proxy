@echo off
chcp 65001 >nul
title TunnelProxy - Background Service
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0launch-flclash-server.ps1" -Mode background
if errorlevel 1 pause


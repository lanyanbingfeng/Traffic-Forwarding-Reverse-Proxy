@echo off
chcp 65001 >nul
title 隧道代理 - 服务端 (跳板机)
echo ============================================
echo   局域网53端口反向代理 - 服务端
echo   监听 0.0.0.0:53  (需管理员权限)
echo ============================================
echo.

cd /d "%~dp0"

set LISTEN=0.0.0.0:53
set KEY=

REM 提示: 监听 53 端口(小于1024)需要"以管理员身份运行"本脚本
REM 支持在此修改: KEY=口令(两端必须一致,无口令留空)
REM 若53端口被系统DNS占用, 可改 LISTEN 为其他端口(客户端 -server 需对应)

if exist "dist\tunnel-server.exe" (
    "dist\tunnel-server.exe" -listen %LISTEN% %KEY%
) else (
    echo [错误] 未找到 dist\tunnel-server.exe
    echo 请先运行 build.ps1 编译程序
)
echo.
pause

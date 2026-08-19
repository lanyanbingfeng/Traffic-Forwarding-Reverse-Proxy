@echo off
chcp 65001 >nul
title 隧道代理 - 客户端 (本机)
echo ============================================
echo   局域网53端口反向代理 - 客户端
echo   监听本机 127.0.0.1:1080  (SOCKS5代理)
echo ============================================
echo.

cd /d "%~dp0"

REM ---- 请在此输入跳板机地址(IP:端口), 也可以直接回车用下面的默认值 ----
set SERVER=
set LISTEN=127.0.0.1:1080
set KEY=

set /p SERVER=请输入跳板机地址[格式 IP:端口, 回车默认 192.168.0.109:53]: 
if "%SERVER%"=="" set SERVER=192.168.0.109:53
if "%KEY%"=="" set /p KEY=请输入隧道口令（服务端与客户端必须一致）:
if "%KEY%"=="" (
    echo [错误] 口令不能为空
    pause
    exit /b 1
)

echo.
echo [启动] 连接跳板机: %SERVER%
echo [启动] 本地SOCKS5代理: %LISTEN%
echo.

if exist "dist\tunnel-client.exe" (
    "dist\tunnel-client.exe" -server "%SERVER%" -listen "%LISTEN%" -key "%KEY%"
) else (
    echo [错误] 未找到 dist\tunnel-client.exe
    echo 请先运行 build.ps1 编译程序
)
echo.
pause

# ============================================================
#  Build script (Windows PowerShell)
#  Usage:
#     powershell -ExecutionPolicy Bypass -File build.ps1
#  Output:
#     dist/tunnel-client.exe  (defaults to client mode)
#     dist/tunnel-server.exe  (defaults to server mode)
#     dist/flclash-server.exe (FlClash direct SOCKS5 over TLS mode)
# ============================================================

$ErrorActionPreference = "Stop"

# Locate the Go toolchain: PATH, standard installer path, then local unzipped dir.
$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    $standardGo = Join-Path $env:ProgramFiles "Go\bin\go.exe"
    if (Test-Path -LiteralPath $standardGo) {
        $env:PATH = "$(Split-Path $standardGo);$env:PATH"
        $goCmd = Get-Command go -ErrorAction SilentlyContinue
    }
}
if (-not $goCmd) {
    $localGo = "$env:LOCALAPPDATA\GoToolchain\go\bin\go.exe"
    if (Test-Path -LiteralPath $localGo) {
        $env:GOROOT = "$env:LOCALAPPDATA\GoToolchain\go"
        $env:PATH = "$env:GOROOT\bin;$env:PATH"
        $goCmd = Get-Command go -ErrorAction SilentlyContinue
    }
}
if (-not $goCmd) {
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $wingetCmd) {
        Write-Host "[ERROR] 未找到 Go，系统也没有 winget，无法自动安装 Go。" -ForegroundColor Red
        Write-Host "请从 https://go.dev/dl/ 安装 Go 1.21+ 后重新双击启动入口。"
        exit 1
    }
    Write-Host "==> 首次运行需要 Go，正在通过 winget 自动安装……" -ForegroundColor Yellow
    & $wingetCmd.Source install --id GoLang.Go --exact --silent --accept-package-agreements --accept-source-agreements --disable-interactivity
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[ERROR] winget 安装 Go 失败，退出代码 $LASTEXITCODE。" -ForegroundColor Red
        exit 1
    }
    $standardGo = Join-Path $env:ProgramFiles "Go\bin\go.exe"
    if (Test-Path -LiteralPath $standardGo) {
        $env:PATH = "$(Split-Path $standardGo);$env:PATH"
        $goCmd = Get-Command go -ErrorAction SilentlyContinue
    }
    if (-not $goCmd) {
        Write-Host "[ERROR] Go 已安装，但当前脚本没有找到 go.exe。请重新双击启动入口。" -ForegroundColor Red
        exit 1
    }
}

$outDir = Join-Path $PSScriptRoot "dist"
New-Item -ItemType Directory -Path $outDir -Force | Out-Null

function Invoke-RoleBuild([string]$Label, [string]$OutputName, [string]$DefaultMode) {
    Write-Host "==> Building $Label ($OutputName) ..." -ForegroundColor Cyan
    & $goCmd.Source -C $PSScriptRoot build -trimpath -ldflags "-s -w -X main.defaultMode=$DefaultMode" -o (Join-Path $outDir $OutputName) .
    if ($LASTEXITCODE -ne 0) {
        throw "构建 $OutputName 失败，go build 退出代码：$LASTEXITCODE"
    }
    if (-not (Test-Path -LiteralPath (Join-Path $outDir $OutputName))) {
        throw "构建命令没有生成 $OutputName。"
    }
}

Invoke-RoleBuild "client" "tunnel-client.exe" "client"
Invoke-RoleBuild "server" "tunnel-server.exe" "server"
Invoke-RoleBuild "FlClash direct server" "flclash-server.exe" "flclash-server"

Write-Host ""
Write-Host "Build OK. Artifacts:" -ForegroundColor Green
Write-Host "  $outDir\tunnel-client.exe  (default client mode)" -ForegroundColor Green
Write-Host "  $outDir\tunnel-server.exe  (default server mode)" -ForegroundColor Green
Write-Host "  $outDir\flclash-server.exe (FlClash direct mode)" -ForegroundColor Green
Write-Host ""
Write-Host "Note: all binaries are the same program; role can be overridden with -mode." -ForegroundColor DarkYellow

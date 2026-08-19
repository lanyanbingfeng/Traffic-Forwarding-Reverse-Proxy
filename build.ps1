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

# Locate the go toolchain: prefer system PATH, fall back to local unzipped dir
$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    $localGo = "$env:LOCALAPPDATA\GoToolchain\go\bin\go.exe"
    if (Test-Path $localGo) {
        $env:GOROOT = "$env:LOCALAPPDATA\GoToolchain\go"
        $env:PATH = "$env:GOROOT\bin;$env:PATH"
        $goCmd = Get-Command go
    }
}
if (-not $goCmd) {
    Write-Host "[ERROR] go not found. Install Go 1.21+ or unzip it to $env:LOCALAPPDATA\GoToolchain" -ForegroundColor Red
    exit 1
}

$outDir = Join-Path $PSScriptRoot "dist"
New-Item -ItemType Directory -Path $outDir -Force | Out-Null

Write-Host "==> Building client (tunnel-client.exe) ..." -ForegroundColor Cyan
go build -trimpath -ldflags "-s -w -X main.defaultMode=client" -o (Join-Path $outDir "tunnel-client.exe") .

Write-Host "==> Building server (tunnel-server.exe) ..." -ForegroundColor Cyan
go build -trimpath -ldflags "-s -w -X main.defaultMode=server" -o (Join-Path $outDir "tunnel-server.exe") .

Write-Host "==> Building FlClash direct server (flclash-server.exe) ..." -ForegroundColor Cyan
go build -trimpath -ldflags "-s -w -X main.defaultMode=flclash-server" -o (Join-Path $outDir "flclash-server.exe") .

Write-Host ""
Write-Host "Build OK. Artifacts:" -ForegroundColor Green
Write-Host "  $outDir\tunnel-client.exe  (default client mode)" -ForegroundColor Green
Write-Host "  $outDir\tunnel-server.exe  (default server mode)" -ForegroundColor Green
Write-Host "  $outDir\flclash-server.exe (FlClash direct mode)" -ForegroundColor Green
Write-Host ""
Write-Host "Note: all binaries are the same program; role can be overridden with -mode." -ForegroundColor DarkYellow

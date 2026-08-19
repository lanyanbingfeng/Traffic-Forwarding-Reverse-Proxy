param()

$ErrorActionPreference = "Stop"
$taskName = "TunnelProxy-FlClash-Server"
$sourceExe = Join-Path $PSScriptRoot "dist\flclash-server.exe"
$installDir = Join-Path $env:ProgramData "TunnelProxy"
$installedExe = Join-Path $installDir "flclash-server.exe"
$configPath = Join-Path $installDir "server.json"
$certPath = Join-Path $installDir "server.crt"
$keyPath = Join-Path $installDir "server.key"

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "请右键 PowerShell 并以管理员身份运行此脚本。"
}
if (-not (Test-Path -LiteralPath $sourceExe)) {
    throw "未找到 $sourceExe，请先运行 build.ps1。"
}

$existingTask = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($existingTask -and $existingTask.State -eq "Running") {
    Stop-ScheduledTask -TaskName $taskName
    Start-Sleep -Seconds 1
}

$listen = Read-Host "监听地址（回车使用 0.0.0.0:53）"
if ([string]::IsNullOrWhiteSpace($listen)) { $listen = "0.0.0.0:53" }
if ($listen -notmatch ':(\d+)$') { throw "监听地址格式无效，应类似 0.0.0.0:53。" }
$listenPort = [int]$Matches[1]
if ($listenPort -lt 1 -or $listenPort -gt 65535) { throw "监听端口必须在 1 到 65535 之间。" }
$portOwner = Get-NetTCPConnection -State Listen -LocalPort $listenPort -ErrorAction SilentlyContinue | Select-Object -First 1
if ($portOwner) {
    throw "TCP $listenPort 已被进程 PID $($portOwner.OwningProcess) 占用，请先停止旧隧道服务或 DNS 服务。"
}
$serverIP = Read-Host "客户端访问的跳板机局域网 IP（例如 192.168.0.109）"
if ([string]::IsNullOrWhiteSpace($serverIP)) { throw "服务器 IP 不能为空。" }
$username = Read-Host "SOCKS5 用户名（回车使用 flclash）"
if ([string]::IsNullOrWhiteSpace($username)) { $username = "flclash" }
$securePassword = Read-Host "SOCKS5 密码（至少 12 位）" -AsSecureString
$passwordPtr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePassword)
try {
    $password = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPtr)
} finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPtr)
}
if ($password.Length -lt 12) { throw "密码至少需要 12 位。" }
if ($username.Length -gt 255 -or $password.Length -gt 255) { throw "用户名和密码不能超过 255 个字符。" }

New-Item -ItemType Directory -Path $installDir -Force | Out-Null
& icacls.exe $installDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw "无法设置服务端配置目录权限。" }
Copy-Item -LiteralPath $sourceExe -Destination $installedExe -Force
$config = [ordered]@{
    listen = $listen
    username = $username
    password = $password
    cert_file = $certPath
    key_file = $keyPath
    handshake_timeout = "15s"
    idle_timeout = "10m"
    max_connections = 512
}
$json = $config | ConvertTo-Json
[IO.File]::WriteAllText($configPath, $json, [Text.UTF8Encoding]::new($false))

$initOutput = & $installedExe -config $configPath -init-only
if ($LASTEXITCODE -ne 0) { throw "TLS 证书初始化失败。" }
$fingerprintLine = $initOutput | Where-Object { $_ -like "CERT_SHA256=*" } | Select-Object -First 1
if (-not $fingerprintLine) { throw "没有获得证书指纹。" }
$fingerprint = $fingerprintLine.Substring("CERT_SHA256=".Length)

$action = New-ScheduledTaskAction -Execute $installedExe -Argument "-config `"$configPath`""
$trigger = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero)
$taskPrincipal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Principal $taskPrincipal -Force | Out-Null
$firewallRuleName = "TunnelProxy-FlClash-TCP-$listenPort"
if (-not (Get-NetFirewallRule -Name $firewallRuleName -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -Name $firewallRuleName -DisplayName "TunnelProxy FlClash TCP $listenPort" -Direction Inbound -Action Allow -Protocol TCP -LocalPort $listenPort -Program $installedExe -Profile Any | Out-Null
}
Start-ScheduledTask -TaskName $taskName

function ConvertTo-YamlQuoted([string]$value) {
    return $value.Replace('\', '\\').Replace('"', '\"')
}
$yamlPath = Join-Path ([Environment]::GetFolderPath("Desktop")) "FlClash-direct.yaml"
$yaml = @"
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

proxies:
  - name: "campus-tunnel-53"
    type: socks5
    server: "$(ConvertTo-YamlQuoted $serverIP)"
    port: $listenPort
    username: "$(ConvertTo-YamlQuoted $username)"
    password: "$(ConvertTo-YamlQuoted $password)"
    tls: true
    sni: "tunnel.local"
    fingerprint: "$fingerprint"
    udp: false
    ip-version: ipv4

proxy-groups:
  - name: "Proxy"
    type: select
    proxies:
      - "campus-tunnel-53"

rules:
  - MATCH,Proxy
"@
[IO.File]::WriteAllText($yamlPath, $yaml, [Text.UTF8Encoding]::new($false))

Write-Host ""
Write-Host "FlClash 直连服务端已安装并启动。" -ForegroundColor Green
Write-Host "计划任务: $taskName"
Write-Host "防火墙规则: $firewallRuleName"
Write-Host "服务配置: $configPath"
Write-Host "客户端 YAML: $yamlPath" -ForegroundColor Cyan
Write-Host "证书指纹: $fingerprint"
Write-Host "请把 YAML 复制到客户端并导入 FlClash。"

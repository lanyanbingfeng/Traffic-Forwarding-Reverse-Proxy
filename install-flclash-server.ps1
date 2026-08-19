param()

$ErrorActionPreference = "Stop"
$taskName = "TunnelProxy-FlClash-Server"
$sourceExe = Join-Path $PSScriptRoot "dist\flclash-server.exe"
$installDir = Join-Path $env:ProgramData "TunnelProxy"
$installedExe = Join-Path $installDir "flclash-server.exe"
$configPath = Join-Path $installDir "server.json"
$certPath = Join-Path $installDir "server.crt"
$keyPath = Join-Path $installDir "server.key"
$launcherSource = Join-Path $PSScriptRoot "launch-flclash-server.ps1"
$backgroundSource = Join-Path $PSScriptRoot "start-flclash-background.bat"
$consoleSource = Join-Path $PSScriptRoot "start-flclash-console.bat"

function Get-PreferredIPv4Address {
    function Test-LANIPv4([string]$Address) {
        $parsed = $null
        if (-not [Net.IPAddress]::TryParse($Address, [ref]$parsed) -or
            $parsed.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork) {
            return $false
        }
        $bytes = $parsed.GetAddressBytes()
        return ($bytes[0] -eq 10) -or
            ($bytes[0] -eq 172 -and $bytes[1] -ge 16 -and $bytes[1] -le 31) -or
            ($bytes[0] -eq 192 -and $bytes[1] -eq 168) -or
            ($bytes[0] -eq 100 -and $bytes[1] -ge 64 -and $bytes[1] -le 127)
    }
    $routes = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue |
        Sort-Object RouteMetric, InterfaceMetric)
    foreach ($route in $routes) {
        $candidate = Get-NetIPAddress -AddressFamily IPv4 -InterfaceIndex $route.InterfaceIndex -ErrorAction SilentlyContinue |
            Where-Object {
                Test-LANIPv4 $_.IPAddress
            } |
            Select-Object -First 1
        if ($candidate) {
            return $candidate.IPAddress
        }
    }
    $fallback = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object {
            Test-LANIPv4 $_.IPAddress
        } |
        Select-Object -First 1
    if ($fallback) {
        return $fallback.IPAddress
    }
    return $null
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "请右键 PowerShell 并以管理员身份运行此脚本。"
}
if (-not (Test-Path -LiteralPath $sourceExe)) {
    throw "未找到 $sourceExe，请先运行 build.ps1。"
}
foreach ($requiredFile in @($launcherSource, $backgroundSource, $consoleSource)) {
    if (-not (Test-Path -LiteralPath $requiredFile)) {
        throw "未找到启动入口文件：$requiredFile"
    }
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
$detectedIP = Get-PreferredIPv4Address
if ($detectedIP) {
    $serverIP = Read-Host "客户端访问的服务端局域网 IP（直接回车使用自动检测值 $detectedIP）"
    if ([string]::IsNullOrWhiteSpace($serverIP)) { $serverIP = $detectedIP }
} else {
    $serverIP = Read-Host "客户端访问的服务端局域网 IPv4 地址（例如 192.168.0.109）"
}
$parsedIP = $null
if ([string]::IsNullOrWhiteSpace($serverIP) -or
    -not [Net.IPAddress]::TryParse($serverIP, [ref]$parsedIP) -or
    $parsedIP.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork -or
    $serverIP -eq "0.0.0.0" -or
    $serverIP.StartsWith("127.")) {
    throw "服务端 IP 必须是客户端能够访问的有效局域网 IPv4 地址。"
}
$username = Read-Host "SOCKS5 用户名（回车使用 flclash）"
if ([string]::IsNullOrWhiteSpace($username)) { $username = "flclash" }
$securePassword = Read-Host "SOCKS5 密码（至少 12 位）" -AsSecureString
$passwordPtr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePassword)
try {
    $password = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPtr)
} finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPtr)
}
$usernameBytes = [Text.Encoding]::UTF8.GetByteCount($username)
$passwordBytes = [Text.Encoding]::UTF8.GetByteCount($password)
if ($passwordBytes -lt 12) { throw "密码的 UTF-8 长度至少需要 12 字节。" }
if ($usernameBytes -gt 255 -or $passwordBytes -gt 255) { throw "用户名和密码的 UTF-8 长度不能超过 255 字节。" }

New-Item -ItemType Directory -Path $installDir -Force | Out-Null
& icacls.exe $installDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw "无法设置服务端配置目录权限。" }
Copy-Item -LiteralPath $sourceExe -Destination $installedExe -Force
Copy-Item -LiteralPath $launcherSource -Destination (Join-Path $installDir "launch-flclash-server.ps1") -Force
Copy-Item -LiteralPath $backgroundSource -Destination (Join-Path $installDir "start-flclash-background.bat") -Force
Copy-Item -LiteralPath $consoleSource -Destination (Join-Path $installDir "start-flclash-console.bat") -Force
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

$action = New-ScheduledTaskAction -Execute $installedExe -Argument "-mode flclash-server -config `"$configPath`"" -WorkingDirectory $installDir
$trigger = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew
$taskPrincipal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Principal $taskPrincipal -Force | Out-Null
$firewallRuleName = "TunnelProxy-FlClash-TCP-$listenPort"
$firewallRule = Get-NetFirewallRule -Name $firewallRuleName -ErrorAction SilentlyContinue
if (-not $firewallRule) {
    $firewallRule = New-NetFirewallRule -Name $firewallRuleName -DisplayName "TunnelProxy FlClash TCP $listenPort" -Direction Inbound -Action Allow -Protocol TCP -LocalPort $listenPort -Program $installedExe -Profile Any -RemoteAddress LocalSubnet
} else {
    $firewallRule | Set-NetFirewallRule -Enabled True -Direction Inbound -Action Allow -Profile Any | Out-Null
    $firewallRule | Get-NetFirewallAddressFilter | Set-NetFirewallAddressFilter -RemoteAddress LocalSubnet | Out-Null
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

$shell = New-Object -ComObject WScript.Shell
$backgroundShortcut = $shell.CreateShortcut((Join-Path ([Environment]::GetFolderPath("Desktop")) "代理服务-后台运行.lnk"))
$backgroundShortcut.TargetPath = Join-Path $installDir "start-flclash-background.bat"
$backgroundShortcut.WorkingDirectory = $installDir
$backgroundShortcut.Description = "后台长期运行 FlClash TCP 53 代理服务"
$backgroundShortcut.Save()

$consoleShortcut = $shell.CreateShortcut((Join-Path ([Environment]::GetFolderPath("Desktop")) "代理服务-窗口日志.lnk"))
$consoleShortcut.TargetPath = Join-Path $installDir "start-flclash-console.bat"
$consoleShortcut.WorkingDirectory = $installDir
$consoleShortcut.Description = "前台运行代理服务并查看客户端访问日志"
$consoleShortcut.Save()

Write-Host ""
Write-Host "FlClash 直连服务端已安装并启动。" -ForegroundColor Green
Write-Host "计划任务: $taskName"
Write-Host "防火墙规则: $firewallRuleName"
Write-Host "服务配置: $configPath"
Write-Host "客户端 YAML: $yamlPath" -ForegroundColor Cyan
Write-Host "YAML 服务端地址: $serverIP`:$listenPort" -ForegroundColor Cyan
Write-Host "桌面入口: 代理服务-后台运行 / 代理服务-窗口日志" -ForegroundColor Cyan
Write-Host "证书指纹: $fingerprint"
Write-Host "请把 YAML 复制到客户端并导入 FlClash。"

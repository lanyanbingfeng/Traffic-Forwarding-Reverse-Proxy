param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("background", "console")]
    [string]$Mode
)

$ErrorActionPreference = "Stop"
$taskName = "TunnelProxy-FlClash-Server"
$installDir = Join-Path $env:ProgramData "TunnelProxy"
$installedExe = Join-Path $installDir "flclash-server.exe"
$configPath = Join-Path $installDir "server.json"
$clientYamlPath = Join-Path ([Environment]::GetFolderPath("Desktop")) "FlClash-direct.yaml"

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Start-ElevatedCopy {
    $windowsPowerShell = Join-Path $env:SystemRoot "System32\WindowsPowerShell\v1.0\powershell.exe"
    $arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`" -Mode $Mode"
    Start-Process -FilePath $windowsPowerShell -Verb RunAs -ArgumentList $arguments | Out-Null
}

function Get-InstalledProxyProcesses {
    $expectedPath = [IO.Path]::GetFullPath($installedExe)
    Get-CimInstance Win32_Process -Filter "Name='flclash-server.exe'" -ErrorAction SilentlyContinue |
        Where-Object {
            $_.ExecutablePath -and
            ([IO.Path]::GetFullPath($_.ExecutablePath) -eq $expectedPath)
        }
}

function Stop-InstalledProxyProcesses {
    $processes = @(Get-InstalledProxyProcesses)
    foreach ($process in $processes) {
        Stop-Process -Id $process.ProcessId -Force -ErrorAction SilentlyContinue
    }
    if ($processes.Count -gt 0) {
        Start-Sleep -Milliseconds 500
    }
}

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

function Get-PreferredIPv4Address {
    $routes = @(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue |
        Sort-Object RouteMetric, InterfaceMetric)
    foreach ($route in $routes) {
        $candidate = Get-NetIPAddress -AddressFamily IPv4 -InterfaceIndex $route.InterfaceIndex -ErrorAction SilentlyContinue |
            Where-Object { Test-LANIPv4 $_.IPAddress } |
            Select-Object -First 1
        if ($candidate) { return $candidate.IPAddress }
    }
    $fallback = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { Test-LANIPv4 $_.IPAddress } |
        Select-Object -First 1
    if ($fallback) { return $fallback.IPAddress }
    return $null
}

function ConvertTo-YamlQuoted([string]$Value) {
    return $Value.Replace('\', '\\').Replace('"', '\"')
}

function Sync-InstalledLaunchers {
    $files = @(
        "launch-flclash-server.ps1",
        "start-flclash-background.bat",
        "start-flclash-console.bat"
    )
    foreach ($file in $files) {
        $source = Join-Path $PSScriptRoot $file
        $destination = Join-Path $installDir $file
        if ((Test-Path -LiteralPath $source) -and
            ([IO.Path]::GetFullPath($source) -ne [IO.Path]::GetFullPath($destination))) {
            Copy-Item -LiteralPath $source -Destination $destination -Force
        }
    }
}

function Ensure-ClientYaml {
    if (Test-Path -LiteralPath $clientYamlPath) { return }

    Write-Host "检测到服务已经安装，但桌面缺少 FlClash YAML，正在补生成……" -ForegroundColor Yellow
    $config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
    if (-not $config.username -or -not $config.password -or -not $config.listen) {
        throw "原服务配置缺少用户名、密码或监听地址，无法补生成 YAML。"
    }
    if ([string]$config.listen -notmatch ':(\d+)$') {
        throw "原服务监听地址格式无效：$($config.listen)"
    }
    $listenPort = [int]$Matches[1]
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

    $initOutput = & $installedExe -config $configPath -init-only
    if ($LASTEXITCODE -ne 0) { throw "读取 TLS 证书指纹失败。" }
    $fingerprintLine = $initOutput | Where-Object { $_ -like "CERT_SHA256=*" } | Select-Object -First 1
    if (-not $fingerprintLine) { throw "没有获得证书指纹。" }
    $fingerprint = $fingerprintLine.Substring("CERT_SHA256=".Length)
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
    username: "$(ConvertTo-YamlQuoted ([string]$config.username))"
    password: "$(ConvertTo-YamlQuoted ([string]$config.password))"
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
    [IO.File]::WriteAllText($clientYamlPath, $yaml, [Text.UTF8Encoding]::new($false))
    Write-Host "已补生成客户端 YAML：$clientYamlPath" -ForegroundColor Green
    Write-Host "YAML 服务端地址：$serverIP`:$listenPort" -ForegroundColor Cyan
}

function Ensure-Installed {
    $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ((Test-Path -LiteralPath $installedExe) -and
        (Test-Path -LiteralPath $configPath) -and
        $task) {
        return
    }

    $installer = Join-Path $PSScriptRoot "install-flclash-server.ps1"
    if (-not (Test-Path -LiteralPath $installer)) {
        throw "服务尚未安装，请回到项目目录运行 install-flclash-server.ps1。"
    }
    $sourceExe = Join-Path $PSScriptRoot "dist\flclash-server.exe"
    if (-not (Test-Path -LiteralPath $sourceExe)) {
        $buildScript = Join-Path $PSScriptRoot "build.ps1"
        if (-not (Test-Path -LiteralPath $buildScript)) {
            throw "缺少 build.ps1，无法自动编译服务端。"
        }
        Write-Host "检测到这是首次 clone，正在自动准备服务端程序……" -ForegroundColor Yellow
        & $buildScript
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $sourceExe)) {
            throw "自动编译失败，请查看上方错误信息。"
        }
    }
    Write-Host "检测到服务尚未完成安装，正在进入首次配置。" -ForegroundColor Yellow
    & $installer
}

try {
    if (-not (Test-Administrator)) {
        Start-ElevatedCopy
        exit 0
    }

    $Host.UI.RawUI.WindowTitle = "TunnelProxy FlClash Server - $Mode"
    Ensure-Installed
    Sync-InstalledLaunchers
    Ensure-ClientYaml
    $task = Get-ScheduledTask -TaskName $taskName -ErrorAction Stop

    if ($Mode -eq "background") {
        if ($task.State -eq "Running") {
            Write-Host "后台服务已经在运行，无需重复启动。" -ForegroundColor Green
            Start-Sleep -Seconds 2
            exit 0
        }
        Stop-InstalledProxyProcesses
        Start-ScheduledTask -TaskName $taskName
        Start-Sleep -Milliseconds 500
        $task = Get-ScheduledTask -TaskName $taskName
        Write-Host "后台长期服务已启动，当前状态：$($task.State)" -ForegroundColor Green
        Write-Host "此窗口可以关闭，服务会继续运行。"
        Start-Sleep -Seconds 2
        exit 0
    }

    if ($task.State -eq "Running") {
        Write-Host "正在暂停后台服务，切换到窗口日志模式……" -ForegroundColor Yellow
        Stop-ScheduledTask -TaskName $taskName
        Start-Sleep -Milliseconds 500
    }
    Stop-InstalledProxyProcesses
    Write-Host "窗口日志模式已启动。客户端访问记录会显示在下面。" -ForegroundColor Green
    Write-Host "关闭窗口或按 Ctrl+C 即可停止；需要后台运行时双击后台启动入口。"
    Write-Host ""
    & $installedExe -config $configPath
    if ($LASTEXITCODE -ne 0) {
        throw "服务进程退出，代码：$LASTEXITCODE"
    }
} catch {
    Write-Host ""
    Write-Host "启动失败：$($_.Exception.Message)" -ForegroundColor Red
    Read-Host "按回车键关闭窗口"
    exit 1
}

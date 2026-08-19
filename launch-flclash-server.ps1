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


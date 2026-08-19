param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("status", "start", "stop")]
    [string]$Action
)

$ErrorActionPreference = "Stop"
$taskName = "TunnelProxy-FlClash-Server"
$task = Get-ScheduledTask -TaskName $taskName -ErrorAction Stop

switch ($Action) {
    "status" {
        $info = Get-ScheduledTaskInfo -TaskName $taskName
        [PSCustomObject]@{
            TaskName = $taskName
            State = $task.State
            LastRunTime = $info.LastRunTime
            LastTaskResult = $info.LastTaskResult
            NextRunTime = $info.NextRunTime
        } | Format-List
    }
    "start" {
        Start-ScheduledTask -TaskName $taskName
        Write-Host "服务端启动命令已发送。" -ForegroundColor Green
    }
    "stop" {
        Stop-ScheduledTask -TaskName $taskName
        Write-Host "服务端停止命令已发送。" -ForegroundColor Yellow
    }
}


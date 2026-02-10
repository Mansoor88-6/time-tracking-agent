# PowerShell script to stop all running time-tracking agent processes

Write-Host "Stopping time-tracking agent processes..." -ForegroundColor Yellow

# Find all Go processes that might be the agent
$processes = Get-Process | Where-Object {
    $_.ProcessName -eq "go" -or 
    $_.ProcessName -eq "time-tracking" -or
    ($_.CommandLine -like "*time-tracking*" -and $_.CommandLine -like "*main.go*")
}

if ($processes.Count -eq 0) {
    Write-Host "No time-tracking agent processes found." -ForegroundColor Green
    exit 0
}

foreach ($proc in $processes) {
    Write-Host "Found process: $($proc.ProcessName) (PID: $($proc.Id))" -ForegroundColor Cyan
    try {
        Stop-Process -Id $proc.Id -Force
        Write-Host "  -> Stopped successfully" -ForegroundColor Green
    } catch {
        Write-Host "  -> Error stopping process: $_" -ForegroundColor Red
    }
}

Write-Host "`nDone!" -ForegroundColor Green

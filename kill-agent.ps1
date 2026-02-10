# Aggressive script to kill all time-tracking agent processes

Write-Host "Killing all time-tracking agent processes..." -ForegroundColor Red

# Kill all Go processes (be careful - this will kill ALL Go processes)
$goProcesses = Get-Process -Name "go" -ErrorAction SilentlyContinue
if ($goProcesses) {
    foreach ($proc in $goProcesses) {
        Write-Host "Killing Go process: PID $($proc.Id)" -ForegroundColor Yellow
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    }
}

# Kill any time-tracking executables
$exeProcesses = Get-Process -Name "time-tracking" -ErrorAction SilentlyContinue
if ($exeProcesses) {
    foreach ($proc in $exeProcesses) {
        Write-Host "Killing time-tracking process: PID $($proc.Id)" -ForegroundColor Yellow
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    }
}

# Wait a moment
Start-Sleep -Milliseconds 500

# Check if any are still running
$remaining = Get-Process -Name "go","time-tracking" -ErrorAction SilentlyContinue
if ($remaining) {
    Write-Host "Some processes still running, using taskkill..." -ForegroundColor Red
    taskkill /F /IM go.exe /T 2>$null
    taskkill /F /IM time-tracking.exe /T 2>$null
} else {
    Write-Host "All processes killed successfully!" -ForegroundColor Green
}

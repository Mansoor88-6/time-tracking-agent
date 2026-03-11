# Build script for Time Tracking Agent Windows installer
# This script builds the agent binary and creates an NSIS installer

param(
    [string]$Version = "1.0.0",
    [string]$BackendURL = "http://192.168.18.18:4000",
    [switch]$SkipBuild = $false
)

$ErrorActionPreference = "Stop"

Write-Host "Building Time Tracking Agent v$Version" -ForegroundColor Cyan

# Get script directory
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = $ScriptDir
$BinDir = Join-Path $RootDir "bin"
$InstallerDir = Join-Path $RootDir "installer"
$OutputDir = Join-Path $RootDir "dist"

# Create directories
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

# Build Go binary
if (-not $SkipBuild) {
    Write-Host "`n[1/3] Building Go binary..." -ForegroundColor Yellow
    
    $BuildTime = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    $BuildFlags = @(
        "-ldflags=-s -w -H windowsgui -X main.Version=$Version -X main.BuildTime=$BuildTime",
        "-o", "$BinDir\time-tracking.exe",
        "./cmd/time-tracking"
    )
    
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    
    go build @BuildFlags
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Build failed!" -ForegroundColor Red
        exit 1
    }
    
    $exeSize = (Get-Item "$BinDir\time-tracking.exe").Length / 1MB
    Write-Host "[OK] Binary built successfully ($([math]::Round($exeSize, 2)) MB)" -ForegroundColor Green
} else {
    Write-Host "`n[1/3] Skipping build (using existing binary)" -ForegroundColor Yellow
}

# Update config template with backend URL if provided
if ($BackendURL -ne "http://192.168.18.18:4000") {
    Write-Host "`n[2/3] Updating config template..." -ForegroundColor Yellow
    $ConfigTemplate = Join-Path $InstallerDir "config-template.yaml"
    $ConfigContent = Get-Content $ConfigTemplate -Raw
    $ConfigContent = $ConfigContent -replace "base_url:.*", "base_url: `"$BackendURL`""
    Set-Content -Path $ConfigTemplate -Value $ConfigContent
    Write-Host "[OK] Config template updated" -ForegroundColor Green
} else {
    Write-Host "`n[2/3] Using default config template..." -ForegroundColor Yellow
}

# Build NSIS installer
Write-Host "`n[3/3] Building NSIS installer..." -ForegroundColor Yellow

# Check if makensis is available (PATH first, then standard install locations)
$makensisCmd = Get-Command makensis -ErrorAction SilentlyContinue
if ($makensisCmd) {
    $makensisPath = $makensisCmd.Source
} elseif (Test-Path "C:\Program Files (x86)\NSIS\makensis.exe") {
    $makensisPath = "C:\Program Files (x86)\NSIS\makensis.exe"
} elseif (Test-Path "C:\Program Files\NSIS\makensis.exe") {
    $makensisPath = "C:\Program Files\NSIS\makensis.exe"
} else {
    Write-Host "ERROR: NSIS (makensis) not found!" -ForegroundColor Red
    Write-Host "Please install NSIS from https://nsis.sourceforge.io/Download" -ForegroundColor Yellow
    exit 1
}
Write-Host "  Using NSIS: $makensisPath" -ForegroundColor Gray

# Update NSIS script with version
$NsiFile = Join-Path $InstallerDir "TimeTrackingAgent.nsi"
$NsiContent = Get-Content $NsiFile -Raw
$NsiContent = $NsiContent -replace '!define PRODUCT_VERSION ".*"', "!define PRODUCT_VERSION `"$Version`""
Set-Content -Path $NsiFile -Value $NsiContent

# Run makensis
Push-Location $InstallerDir
try {
    & $makensisPath TimeTrackingAgent.nsi
    if ($LASTEXITCODE -ne 0) {
        Write-Host "NSIS build failed!" -ForegroundColor Red
        exit 1
    }
} finally {
    Pop-Location
}

# Find the generated installer
$InstallerFile = Get-ChildItem -Path $InstallerDir -Filter "TimeTrackingAgent-Setup-*.exe" | Sort-Object LastWriteTime -Descending | Select-Object -First 1

if ($InstallerFile) {
    # Copy to dist folder
    Copy-Item $InstallerFile.FullName -Destination $OutputDir -Force
    $InstallerSize = $InstallerFile.Length / 1MB
    
    Write-Host "`n[OK] Installer built successfully!" -ForegroundColor Green
    Write-Host "  File: $($InstallerFile.Name)" -ForegroundColor Cyan
    Write-Host "  Size: $([math]::Round($InstallerSize, 2)) MB" -ForegroundColor Cyan
    Write-Host "  Location: $OutputDir" -ForegroundColor Cyan
} else {
    Write-Host "`nERROR: Installer file not found!" -ForegroundColor Red
    exit 1
}

Write-Host "`nBuild complete! [OK]" -ForegroundColor Green

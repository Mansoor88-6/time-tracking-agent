# Time Tracking Agent Installer

This directory contains the NSIS installer script and build tools for creating a Windows installer for the Time Tracking Agent.

## Prerequisites

1. **NSIS (Nullsoft Scriptable Install System)**
   - Download from: https://nsis.sourceforge.io/Download
   - Install and ensure `makensis.exe` is in your PATH
   - Or use the full path: `"C:\Program Files (x86)\NSIS\makensis.exe"`

2. **Go 1.25+** (for building the agent binary)

3. **PowerShell** (for running the build script)

## Building the Installer

### Quick Build

```powershell
.\build-release.ps1
```

This will:
1. Build the Go agent binary for Windows (x64)
2. Create the NSIS installer
3. Output: `dist/TimeTrackingAgent-Setup-1.0.0.exe`

### Custom Build

```powershell
.\build-release.ps1 -Version "1.0.1" -BackendURL "https://your-production-backend.com"
```

### Parameters

- `-Version`: Version number for the installer (default: "1.0.0")
- `-BackendURL`: Production backend URL to set in config template (default: "https://your-backend-url.com")
- `-SkipBuild`: Skip Go build step (use existing binary)

## Installer Features

- **User-level installation** (no admin required)
- **Installation directory**: `%LOCALAPPDATA%\TimeTrackingAgent\`
- **Components**:
  - Core agent binary (required)
  - Start with Windows (optional)
  - Desktop shortcut (optional)
- **Uninstaller**: Removes binaries and shortcuts, preserves user data (config/logs/storage)

## Installation Structure

```
%LOCALAPPDATA%\TimeTrackingAgent\
├── bin\
│   └── time-tracking.exe
├── config\
│   └── config.yaml
├── logs\
├── storage\
│   └── database.db
└── Uninstall.exe
```

## First Run

On first run after installation:
1. Agent starts and checks for device token
2. If no token exists, opens browser for device authorization
3. User logs in via the device auth page
4. Agent receives device token and saves it
5. Tracking begins automatically

## Tray Icon

The agent runs in the system tray with the following menu options:

- **Status**: Shows current tracking status (active session info)
- **Pause/Resume Tracking**: Temporarily stop/start tracking
- **Open Dashboard**: Opens web dashboard in browser
- **View Logs**: Opens logs folder in Explorer
- **Quit**: Gracefully exits the agent

## Customization

### Icon

Replace the default icon in `tray.go` (`getDefaultIcon()`) with your actual icon bytes, or load from a file.

### Branding

Edit `TimeTrackingAgent.nsi` to update:
- `PRODUCT_NAME`
- `PRODUCT_PUBLISHER`
- `PRODUCT_WEB_SITE`
- Installer icon (add `!define MUI_ICON` pointing to your `.ico` file)

## Testing

1. Build the installer: `.\build-release.ps1`
2. Run `dist/TimeTrackingAgent-Setup-1.0.0.exe` on a test Windows machine
3. Verify:
   - Installation completes successfully
   - Agent starts and appears in system tray
   - Device authorization flow works
   - Tracking begins after auth
   - Uninstaller removes files correctly

## Troubleshooting

### NSIS not found

Ensure NSIS is installed and `makensis` is in PATH, or update `build-release.ps1` to use the full path.

### Build fails

- Check Go version: `go version` (requires 1.25+)
- Ensure all dependencies are installed: `go mod download`
- Check NSIS installation

### Tray icon not appearing

- Verify Windows build tags are correct (`// +build windows`)
- Check that `github.com/getlantern/systray` is installed: `go get github.com/getlantern/systray`

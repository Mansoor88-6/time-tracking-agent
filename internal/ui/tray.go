//go:build windows
// +build windows

package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/assets"
	"Mansoor88-6/time-tracking-agent/internal/service"

	"github.com/getlantern/systray"
	"go.uber.org/zap"
)

// TrayManager manages the system tray icon and menu
type TrayManager struct {
	logger          *zap.Logger
	sessionManager  *service.SessionManager
	trackingService *service.TrackingService
	backendURL      string
	logsPath        string
	isPaused        bool
	pauseMu         sync.RWMutex
	quitChan        chan struct{}
	
	// Menu items
	statusItem      *systray.MenuItem
	pauseItem       *systray.MenuItem
	dashboardItem   *systray.MenuItem
	logsItem        *systray.MenuItem
	quitItem        *systray.MenuItem
}

// NewTrayManager creates a new tray manager
func NewTrayManager(
	logger *zap.Logger,
	sessionManager *service.SessionManager,
	trackingService *service.TrackingService,
	backendURL string,
	logsPath string,
) *TrayManager {
	return &TrayManager{
		logger:          logger,
		sessionManager:  sessionManager,
		trackingService: trackingService,
		backendURL:      backendURL,
		logsPath:        logsPath,
		quitChan:        make(chan struct{}),
		isPaused:        false,
	}
}

// Start starts the tray icon (must be called from main goroutine on Windows)
func (tm *TrayManager) Start(ctx context.Context) error {
	if runtime.GOOS != "windows" {
		// Tray icon only supported on Windows
		return nil
	}

	// Run systray in a goroutine
	go func() {
		systray.Run(tm.onReady, tm.onExit)
	}()

	// Update status periodically
	go tm.statusUpdateLoop(ctx)

	// Wait for quit signal
	select {
	case <-tm.quitChan:
		tm.logger.Info("Tray quit signal received")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// onReady is called when systray is ready
func (tm *TrayManager) onReady() {
	// Set icon (using a simple default icon - you can replace with your own)
	// For now, we'll use a built-in icon or create a simple one
	systray.SetIcon(getDefaultIcon())
	systray.SetTooltip("Time Tracking Agent")

	// Add menu items
	tm.statusItem = systray.AddMenuItem("Status: Active", "Current tracking status")
	tm.statusItem.Disable()

	systray.AddSeparator()

	tm.pauseItem = systray.AddMenuItem("Pause Tracking", "Temporarily stop tracking")
	
	systray.AddSeparator()

	tm.dashboardItem = systray.AddMenuItem("Open Dashboard", "Open web dashboard in browser")
	tm.logsItem = systray.AddMenuItem("View Logs", "Open logs folder")

	systray.AddSeparator()

	tm.quitItem = systray.AddMenuItem("Quit", "Exit Time Tracking Agent")

	// Set initial status
	tm.updateStatus()

	// Handle menu clicks
	go tm.handleMenuClicks()
	
	// Update tooltip to show auth status if needed
	tm.updateAuthStatus("")
}

// onExit is called when systray exits
func (tm *TrayManager) onExit() {
	tm.logger.Info("Tray icon exiting")
	close(tm.quitChan)
}

// handleMenuClicks handles menu item clicks
func (tm *TrayManager) handleMenuClicks() {
	for {
		select {
		case <-tm.pauseItem.ClickedCh:
			tm.togglePause()
		case <-tm.dashboardItem.ClickedCh:
			tm.openDashboard()
		case <-tm.logsItem.ClickedCh:
			tm.openLogs()
		case <-tm.quitItem.ClickedCh:
			tm.quit()
		}
	}
}

// togglePause toggles tracking pause state
func (tm *TrayManager) togglePause() {
	tm.pauseMu.Lock()
	tm.isPaused = !tm.isPaused
	isPaused := tm.isPaused
	tm.pauseMu.Unlock()

	// Update tracking service pause state
	if tm.trackingService != nil {
		tm.trackingService.SetPaused(isPaused)
	}

	if isPaused {
		tm.pauseItem.SetTitle("Resume Tracking")
		tm.updateTooltip("Time Tracking Agent - Paused")
		tm.logger.Info("Tracking paused by user")
	} else {
		tm.pauseItem.SetTitle("Pause Tracking")
		tm.updateTooltip("Time Tracking Agent - Active")
		tm.logger.Info("Tracking resumed by user")
	}
}

// IsPaused returns whether tracking is currently paused
func (tm *TrayManager) IsPaused() bool {
	tm.pauseMu.RLock()
	defer tm.pauseMu.RUnlock()
	return tm.isPaused
}

// openDashboard opens the web dashboard in the default browser
func (tm *TrayManager) openDashboard() {
	// Extract dashboard URL from backend URL (assuming dashboard is at /dashboard)
	dashboardURL := tm.backendURL
	if dashboardURL == "" {
		dashboardURL = "http://localhost:4000"
	}
	
	// Open in default browser
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", dashboardURL)
	case "darwin":
		cmd = exec.Command("open", dashboardURL)
	case "linux":
		cmd = exec.Command("xdg-open", dashboardURL)
	default:
		tm.logger.Warn("Unsupported OS for opening browser", zap.String("os", runtime.GOOS))
		return
	}

	if err := cmd.Run(); err != nil {
		tm.logger.Error("Failed to open dashboard", zap.Error(err))
	}
}

// openLogs opens the logs folder in Explorer
func (tm *TrayManager) openLogs() {
	logsDir := tm.logsPath
	if logsDir == "" {
		// Default to current directory logs
		logsDir = "logs"
	}

	// Ensure directory exists
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		tm.logger.Error("Failed to create logs directory", zap.Error(err))
		return
	}

	// Open in Explorer
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", logsDir)
	case "darwin":
		cmd = exec.Command("open", logsDir)
	case "linux":
		cmd = exec.Command("xdg-open", logsDir)
	default:
		tm.logger.Warn("Unsupported OS for opening folder", zap.String("os", runtime.GOOS))
		return
	}

	if err := cmd.Run(); err != nil {
		tm.logger.Error("Failed to open logs folder", zap.Error(err))
	}
}

// quit gracefully shuts down the agent
func (tm *TrayManager) quit() {
	tm.logger.Info("Quit requested from tray menu")
	systray.Quit()
	// Signal main to shutdown
	close(tm.quitChan)
}

// statusUpdateLoop periodically updates the status display
func (tm *TrayManager) statusUpdateLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tm.updateStatus()
		case <-ctx.Done():
			return
		}
	}
}

// updateStatus updates the status menu item with current session info
func (tm *TrayManager) updateStatus() {
	if tm.sessionManager == nil {
		return
	}

	session := tm.sessionManager.GetCurrentSession()
	if session == nil {
		tm.statusItem.SetTitle("Status: Idle")
		tm.updateTooltip("Time Tracking Agent - No active session")
		return
	}

	var statusText string
	if tm.IsPaused() {
		statusText = "Status: Paused"
	} else {
		duration := time.Since(session.StartTime)
		statusText = fmt.Sprintf("Status: Tracking (%s)", formatDuration(duration))
	}

	tm.statusItem.SetTitle(statusText)

	// Update tooltip with more details
	var tooltip string
	if session.Source == "browser" {
		tooltip = fmt.Sprintf("Time Tracking Agent\nActive: %s\nURL: %s", session.Application, session.URL)
	} else {
		tooltip = fmt.Sprintf("Time Tracking Agent\nActive: %s\nTitle: %s", session.Application, session.Title)
	}
	tm.updateTooltip(tooltip)
}

// updateTooltip updates the tray icon tooltip
func (tm *TrayManager) updateTooltip(text string) {
	systray.SetTooltip(text)
}

// updateAuthStatus updates the tray status based on auth state
func (tm *TrayManager) updateAuthStatus(authStatus string) {
	var tooltip string
	if authStatus != "" {
		tooltip = fmt.Sprintf("Time Tracking Agent\n%s", authStatus)
	} else {
		tooltip = "Time Tracking Agent"
	}
	tm.updateTooltip(tooltip)
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// getDefaultIcon returns the embedded tray icon
func getDefaultIcon() []byte {
	// Return embedded logo.ico from assets package
	if len(assets.TrayIcon) > 0 {
		return assets.TrayIcon
	}
	// Fallback: minimal 1x1 transparent PNG (should not be reached if embed works)
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
		0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}
}

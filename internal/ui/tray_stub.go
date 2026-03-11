//go:build !windows
// +build !windows

package ui

import (
	"context"

	"Mansoor88-6/time-tracking-agent/internal/service"

	"go.uber.org/zap"
)

// TrayManager stub for non-Windows platforms
type TrayManager struct {
	logger          *zap.Logger
	sessionManager  *service.SessionManager
	trackingService *service.TrackingService
	backendURL      string
	logsPath        string
	quitChan        chan struct{}
}

// NewTrayManager creates a new tray manager (stub for non-Windows)
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
	}
}

// Start starts the tray icon (no-op on non-Windows)
func (tm *TrayManager) Start(ctx context.Context) error {
	// Tray icon not supported on this platform
	<-ctx.Done()
	return ctx.Err()
}

// IsPaused returns whether tracking is currently paused
func (tm *TrayManager) IsPaused() bool {
	return false
}

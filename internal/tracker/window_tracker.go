package tracker

import (
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/platform"

	"go.uber.org/zap"
)

// AppFocusInfo represents application focus information (simplified from WindowInfo)
type AppFocusInfo struct {
	Application string
	PID         int
	Title       string
	Timestamp   time.Time
}

// WindowTracker monitors active window changes (simplified to only track app focus)
type WindowTracker struct {
	platform        platform.Platform
	pollInterval     time.Duration
	currentAppFocus  *AppFocusInfo
	onAppFocus       func(*AppFocusInfo)
	logger           *zap.Logger
	stopChan         chan struct{}
	wg               sync.WaitGroup
	mu               sync.RWMutex
	sequenceCounter  int // Sequence counter for app focus events
}

// NewWindowTracker creates a new window tracker
func NewWindowTracker(platform platform.Platform, pollInterval time.Duration, logger *zap.Logger) *WindowTracker {
	return &WindowTracker{
		platform:     platform,
		pollInterval: pollInterval,
		logger:       logger,
		stopChan:     make(chan struct{}),
	}
}

// Start begins monitoring application focus changes
func (wt *WindowTracker) Start(onAppFocus func(*AppFocusInfo)) error {
	wt.onAppFocus = onAppFocus

	wt.wg.Add(1)
	go wt.pollLoop()

	wt.logger.Info("Window tracker started",
		zap.Duration("poll_interval", wt.pollInterval),
	)
	return nil
}

// Stop stops monitoring window changes
func (wt *WindowTracker) Stop() {
	wt.mu.Lock()
	select {
	case <-wt.stopChan:
		// Already closed
		wt.mu.Unlock()
		return
	default:
		close(wt.stopChan)
	}
	wt.mu.Unlock()
	
	wt.wg.Wait()
	wt.logger.Info("Window tracker stopped")
}

// GetCurrentAppFocus returns the current app focus info
func (wt *WindowTracker) GetCurrentAppFocus() *AppFocusInfo {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	if wt.currentAppFocus == nil {
		return nil
	}
	// Return a copy
	appFocus := *wt.currentAppFocus
	return &appFocus
}

func (wt *WindowTracker) pollLoop() {
	defer wt.wg.Done()

	ticker := time.NewTicker(wt.pollInterval)
	defer ticker.Stop()

	// Initial poll
	wt.checkWindow()

	for {
		select {
		case <-ticker.C:
			wt.checkWindow()
		case <-wt.stopChan:
			return
		}
	}
}

func (wt *WindowTracker) checkWindow() {
	// Check if we should stop
	select {
	case <-wt.stopChan:
		return
	default:
	}

	window, err := wt.platform.GetActiveWindow()
	if err != nil {
		wt.logger.Error("Failed to get active window", zap.Error(err))
		return
	}

	// Check again after potentially slow operation
	select {
	case <-wt.stopChan:
		return
	default:
	}

	wt.mu.Lock()
	hasChanged := wt.hasAppFocusChanged(window)
	if hasChanged {
		now := time.Now()
		wt.currentAppFocus = &AppFocusInfo{
			Application: window.Application,
			PID:         window.ProcessID,
			Title:       window.Title,
			Timestamp:   now,
		}
		wt.mu.Unlock()

		// Final check before calling callback
		select {
		case <-wt.stopChan:
			return
		default:
		}

		wt.logger.Debug("App focus changed",
			zap.String("application", window.Application),
			zap.Int("pid", window.ProcessID),
			zap.String("title", window.Title),
			zap.String("title_length", string(rune(len(window.Title)))),
		)
		wt.logger.Info("AppFocusInfo created with title",
			zap.String("application", window.Application),
			zap.String("title", window.Title),
			zap.Bool("title_empty", window.Title == ""),
		)

		if wt.onAppFocus != nil {
			wt.onAppFocus(wt.currentAppFocus)
		}
	} else {
		wt.mu.Unlock()
	}
}

func (wt *WindowTracker) hasAppFocusChanged(newWindow *platform.WindowInfo) bool {
	if wt.currentAppFocus == nil {
		wt.logger.Debug("App focus changed: no previous focus",
			zap.String("new_application", newWindow.Application),
			zap.Int("new_pid", newWindow.ProcessID),
			zap.String("new_title", newWindow.Title),
		)
		return true
	}

	// Check if application or process ID changed
	if wt.currentAppFocus.PID != newWindow.ProcessID {
		wt.logger.Debug("App focus changed: pid changed",
			zap.Int("old_pid", wt.currentAppFocus.PID),
			zap.Int("new_pid", newWindow.ProcessID),
			zap.String("application", newWindow.Application),
		)
		return true
	}
	if wt.currentAppFocus.Application != newWindow.Application {
		wt.logger.Debug("App focus changed: application changed",
			zap.String("old_application", wt.currentAppFocus.Application),
			zap.String("new_application", newWindow.Application),
			zap.Int("pid", newWindow.ProcessID),
		)
		return true
	}

	// Also treat title changes as focus changes so we get per-tab/file sessions
	if wt.currentAppFocus.Title != newWindow.Title {
		wt.logger.Debug("App focus changed: title changed",
			zap.String("application", newWindow.Application),
			zap.Int("pid", newWindow.ProcessID),
			zap.String("old_title", wt.currentAppFocus.Title),
			zap.String("new_title", newWindow.Title),
		)
		return true
	}

	wt.logger.Debug("App focus unchanged",
		zap.String("application", newWindow.Application),
		zap.Int("pid", newWindow.ProcessID),
		zap.String("title", newWindow.Title),
	)
	return false
}

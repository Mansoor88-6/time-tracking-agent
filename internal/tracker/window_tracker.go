package tracker

import (
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/platform"

	"go.uber.org/zap"
)

// WindowTracker monitors active window changes
type WindowTracker struct {
	platform      platform.Platform
	pollInterval  time.Duration
	currentWindow *platform.WindowInfo
	onChange      func(*platform.WindowInfo)
	logger        *zap.Logger
	stopChan      chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
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

// Start begins monitoring window changes
func (wt *WindowTracker) Start(onChange func(*platform.WindowInfo)) error {
	wt.onChange = onChange

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

// GetCurrentWindow returns the current active window
func (wt *WindowTracker) GetCurrentWindow() *platform.WindowInfo {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	return wt.currentWindow
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
	hasChanged := wt.hasWindowChanged(window)
	if hasChanged {
		wt.currentWindow = window
		wt.mu.Unlock()

		// Final check before calling callback
		select {
		case <-wt.stopChan:
			return
		default:
		}

		wt.logger.Debug("Window changed",
			zap.String("application", window.Application),
			zap.String("title", window.Title),
		)

		if wt.onChange != nil {
			wt.onChange(window)
		}
	} else {
		wt.mu.Unlock()
	}
}

func (wt *WindowTracker) hasWindowChanged(newWindow *platform.WindowInfo) bool {
	if wt.currentWindow == nil {
		return true
	}

	// Check if window changed (title, application, or process ID)
	if wt.currentWindow.ProcessID != newWindow.ProcessID {
		return true
	}
	if wt.currentWindow.Title != newWindow.Title {
		return true
	}
	if wt.currentWindow.Application != newWindow.Application {
		return true
	}
	if wt.currentWindow.IsVisible != newWindow.IsVisible {
		return true
	}

	return false
}

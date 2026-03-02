package service

import (
	"strings"
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/models"

	"go.uber.org/zap"
)

// ActiveSession represents an active tracking session
type ActiveSession struct {
	Source        string    // "browser" | "app"
	Application   string
	PID           int
	TabID         int       // 0 for non-browser
	WindowID      int       // 0 for non-browser
	URL           string    // Empty for non-browser
	Title         string    // Empty for non-browser
	StartTime     time.Time
	LastEventTime time.Time
	Sequence      int // Last sequence number
}

// SessionManager manages active sessions and processes events immediately
// Browser events and app focus events are processed independently since they come from different sources
type SessionManager struct {
	currentSession *ActiveSession
	mu             sync.RWMutex
	logger         *zap.Logger
	onSessionEnd   func(*ActiveSession) // Callback when session ends
	stopChan       chan struct{}
	inactivityTimeout time.Duration
}

// NewSessionManager creates a new session manager
func NewSessionManager(
	onSessionEnd func(*ActiveSession),
	logger *zap.Logger,
	inactivityTimeout time.Duration,
) *SessionManager {
	sm := &SessionManager{
		onSessionEnd: onSessionEnd,
		logger:       logger,
		stopChan:     make(chan struct{}),
		inactivityTimeout: inactivityTimeout,
	}

	// Start inactivity watcher if timeout is configured
	if inactivityTimeout > 0 {
		go sm.inactivityLoop()
	}

	return sm
}

// Stop stops the session manager and closes current session
func (sm *SessionManager) Stop() {
	// Ensure we only close stopChan once
	select {
	case <-sm.stopChan:
		// Already closed
	default:
		close(sm.stopChan)
	}

	// Close current session on shutdown
	sm.mu.Lock()
	if sm.currentSession != nil {
		session := sm.currentSession
		sm.currentSession = nil
		sm.mu.Unlock()
		sm.closeSession(session, time.Now())
	} else {
		sm.mu.Unlock()
	}
}

// ProcessBrowserEvent processes a browser event from the extension immediately
// Browser events are authoritative for browser context and are processed without buffering
func (sm *SessionManager) ProcessBrowserEvent(event *models.BrowserEvent) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	eventTime := time.UnixMilli(event.Timestamp)
	sm.handleBrowserEventLocked(event, eventTime)
}

// ProcessAppFocusEvent processes an app focus event from OS immediately
// App focus events are authoritative for app context and are processed without buffering
// However, if the app is a browser, we wait for the browser event instead
func (sm *SessionManager) ProcessAppFocusEvent(event *models.AppFocusEvent) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Skip browser applications - browser events are authoritative
	if sm.isBrowserApplication(event.Application) {
		sm.logger.Debug("Skipping app focus event for browser, waiting for browser event",
			zap.String("application", event.Application),
		)
		return
	}

	eventTime := time.UnixMilli(event.Timestamp)
	sm.handleAppFocusEventLocked(event, eventTime)
}

// isBrowserApplication checks if an application is a browser
func (sm *SessionManager) isBrowserApplication(application string) bool {
	appLower := strings.ToLower(application)
	browsers := []string{
		"chrome", "google chrome", "chromium",
		"firefox", "mozilla firefox",
		"edge", "microsoft edge",
		"safari", "opera", "brave", "vivaldi", "tor browser",
	}
	for _, browser := range browsers {
		if strings.Contains(appLower, browser) {
			return true
		}
	}
	return false
}

// handleBrowserEventLocked handles a browser event (must be called with lock held)
func (sm *SessionManager) handleBrowserEventLocked(event *models.BrowserEvent, eventTime time.Time) {
	// Check if we need to close current session
	if sm.currentSession != nil {
		// Use current time for closing to ensure accurate duration
		closeTime := time.Now()
		// But ensure it's not before the session's last event time
		if closeTime.Before(sm.currentSession.LastEventTime) {
			closeTime = sm.currentSession.LastEventTime
		}
		
		// Close if switching from app to browser, or different browser tab/window
		if sm.currentSession.Source == "app" ||
			sm.currentSession.TabID != event.TabID ||
			sm.currentSession.WindowID != event.WindowID {
			sm.closeSessionLocked(sm.currentSession, closeTime)
			sm.currentSession = nil
		} else if sm.currentSession.URL != event.URL {
			// Same tab but URL changed, close old session and start new
			sm.closeSessionLocked(sm.currentSession, closeTime)
			sm.currentSession = nil
		}
	}

	// Start new browser session if needed
	if sm.currentSession == nil {
		sm.currentSession = &ActiveSession{
			Source:        "browser",
			Application:   event.Browser,
			TabID:         event.TabID,
			WindowID:      event.WindowID,
			URL:           event.URL,
			Title:         event.Title,
			StartTime:     eventTime,
			LastEventTime: eventTime,
			Sequence:      event.Sequence,
		}
		sm.logger.Info("Started new browser session",
			zap.String("url", event.URL),
			zap.String("title", event.Title),
			zap.Int("tabId", event.TabID),
			zap.Int("windowId", event.WindowID),
		)
	} else {
		// Update existing session
		sm.currentSession.URL = event.URL
		sm.currentSession.Title = event.Title
		sm.currentSession.LastEventTime = eventTime
		sm.currentSession.Sequence = event.Sequence
	}
}

// handleAppFocusEventLocked handles an app focus event (must be called with lock held)
func (sm *SessionManager) handleAppFocusEventLocked(event *models.AppFocusEvent, eventTime time.Time) {
	// Check if we need to close current session
	if sm.currentSession != nil {
		// Use current time for closing to ensure accurate duration
		closeTime := time.Now()
		// But ensure it's not before the session's last event time
		if closeTime.Before(sm.currentSession.LastEventTime) {
			closeTime = sm.currentSession.LastEventTime
		}
		
		// Close if switching from browser to app, different app/PID, or title changed
		// Title changes within the same app should create a new session for accurate per-tab tracking
		if sm.currentSession.Source == "browser" ||
			sm.currentSession.Application != event.Application ||
			sm.currentSession.PID != event.PID ||
			sm.currentSession.Title != event.Title {
			closeReason := "source_change"
			if sm.currentSession.Source == "app" && 
				sm.currentSession.Application == event.Application &&
				sm.currentSession.PID == event.PID &&
				sm.currentSession.Title != event.Title {
				closeReason = "title_change"
			}
			sm.logger.Info("Closing app session before starting new one",
				zap.String("reason", closeReason),
				zap.String("application", sm.currentSession.Application),
				zap.String("old_title", sm.currentSession.Title),
				zap.String("new_title", event.Title),
			)
			sm.closeSessionLocked(sm.currentSession, closeTime)
			sm.currentSession = nil
		}
	}

	// Start new app session if needed
	if sm.currentSession == nil {
		sm.currentSession = &ActiveSession{
			Source:        "app",
			Application:   event.Application,
			PID:           event.PID,
			TabID:         0,
			WindowID:      0,
			URL:           "",
			Title:         event.Title,
			StartTime:     eventTime,
			LastEventTime: eventTime,
			Sequence:      event.Sequence,
		}
		sm.logger.Info("Started new app session",
			zap.String("application", event.Application),
			zap.Int("pid", event.PID),
			zap.String("title", event.Title),
			zap.Bool("title_empty", event.Title == ""),
			zap.Int("title_length", len(event.Title)),
		)
		sm.logger.Debug("App session created with title details",
			zap.String("session_title", sm.currentSession.Title),
			zap.String("event_title", event.Title),
			zap.Bool("titles_match", sm.currentSession.Title == event.Title),
		)
	} else {
		// Update existing session (same app, PID, and title)
		sm.currentSession.LastEventTime = eventTime
		sm.currentSession.Sequence = event.Sequence
	}
}

// closeSessionLocked closes a session and emits the event (must be called with lock held)
func (sm *SessionManager) closeSessionLocked(session *ActiveSession, endTime time.Time) {
	sm.mu.Unlock()
	sm.closeSession(session, endTime)
	sm.mu.Lock()
}

// closeSession closes a session and emits the event (called without lock)
func (sm *SessionManager) closeSession(session *ActiveSession, endTime time.Time) {
	// Update LastEventTime to the actual end time before calculating duration
	// This ensures OnSessionEnd calculates duration correctly even if no events
	// occurred between session start and close
	session.LastEventTime = endTime
	
	sm.logger.Debug("Closing session",
		zap.String("source", session.Source),
		zap.String("application", session.Application),
		zap.Time("start_time", session.StartTime),
		zap.Time("end_time", endTime),
		zap.Duration("duration", endTime.Sub(session.StartTime)),
	)
	
	if sm.onSessionEnd != nil {
		sm.onSessionEnd(session)
	}
}

// inactivityLoop periodically checks for inactive sessions and closes them
func (sm *SessionManager) inactivityLoop() {
	// Check every 5 seconds for inactivity
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if sm.inactivityTimeout <= 0 {
				continue
			}

			sm.mu.Lock()
			if sm.currentSession != nil {
				now := time.Now()
				inactiveFor := now.Sub(sm.currentSession.LastEventTime)
				if inactiveFor > sm.inactivityTimeout {
					session := sm.currentSession
					// Calculate end time: session ended when activity stopped
					// Use LastEventTime + inactivityTimeout to represent when the session actually ended
					// This ensures non-zero duration even if LastEventTime == StartTime
					endTime := session.LastEventTime.Add(sm.inactivityTimeout)
					// Ensure endTime is at least StartTime + inactivityTimeout
					if endTime.Before(session.StartTime) {
						endTime = session.StartTime.Add(sm.inactivityTimeout)
					}
					// Cap endTime at now to avoid future timestamps
					if endTime.After(now) {
						endTime = now
					}
					sm.logger.Info("Closing session due to inactivity",
						zap.String("source", session.Source),
						zap.String("application", session.Application),
						zap.String("title", session.Title),
						zap.Time("start_time", session.StartTime),
						zap.Time("last_event_time", session.LastEventTime),
						zap.Time("end_time", endTime),
						zap.Duration("inactive_for", inactiveFor),
						zap.Duration("timeout", sm.inactivityTimeout),
						zap.Duration("calculated_duration", endTime.Sub(session.StartTime)),
					)
					sm.closeSessionLocked(session, endTime)
					sm.currentSession = nil
				}
			}
			sm.mu.Unlock()
		case <-sm.stopChan:
			return
		}
	}
}

// GetCurrentSession returns the current active session (for debugging/monitoring)
func (sm *SessionManager) GetCurrentSession() *ActiveSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.currentSession == nil {
		return nil
	}

	// Return a copy
	session := *sm.currentSession
	return &session
}

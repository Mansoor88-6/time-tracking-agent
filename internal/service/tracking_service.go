package service

import (
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/client"
	"Mansoor88-6/time-tracking-agent/internal/collector"
	"Mansoor88-6/time-tracking-agent/internal/models"
	"Mansoor88-6/time-tracking-agent/internal/platform"
	"Mansoor88-6/time-tracking-agent/internal/queue"
	"Mansoor88-6/time-tracking-agent/internal/tracker"

	"go.uber.org/zap"
)

// TrackingService orchestrates all tracking components
type TrackingService struct {
	platform        platform.Platform
	windowTracker   *tracker.WindowTracker
	activityTracker *tracker.ActivityTracker
	eventCollector  *collector.EventCollector
	apiClient       *client.APIClient
	eventQueue      *queue.EventQueue
	sessionManager  *SessionManager
	deviceID        string
	logger          *zap.Logger
	
	currentState     tracker.ActivityState
	stopped          bool
	mu               sync.RWMutex
	appSequenceCounter int // Sequence counter for app focus events
	
	stopChan         chan struct{}
	wg               sync.WaitGroup
}

// NewTrackingService creates a new tracking service
func NewTrackingService(
	platform platform.Platform,
	windowTracker *tracker.WindowTracker,
	activityTracker *tracker.ActivityTracker,
	eventCollector *collector.EventCollector,
	apiClient *client.APIClient,
	eventQueue *queue.EventQueue,
	sessionManager *SessionManager,
	deviceID string,
	logger *zap.Logger,
) *TrackingService {
	return &TrackingService{
		platform:       platform,
		windowTracker:  windowTracker,
		activityTracker: activityTracker,
		eventCollector: eventCollector,
		apiClient:     apiClient,
		eventQueue:    eventQueue,
		sessionManager: sessionManager,
		deviceID:      deviceID,
		logger:        logger,
		stopChan:      make(chan struct{}),
		currentState:  tracker.StateActive,
	}
}

// Start begins tracking
func (ts *TrackingService) Start() error {
	ts.logger.Info("Starting tracking service", zap.String("device_id", ts.deviceID))

	// Start window tracker with app focus callback
	if err := ts.windowTracker.Start(ts.onAppFocus); err != nil {
		return err
	}

	// Start activity tracker
	if err := ts.activityTracker.Start(ts.onActivityStateChange); err != nil {
		ts.windowTracker.Stop()
		return err
	}

	// Start event collector
	ts.eventCollector.Start(ts.onBatchReady)

	// Start queue processor
	ts.wg.Add(1)
	go ts.queueProcessor()

	ts.logger.Info("Tracking service started")
	return nil
}

// Stop stops tracking
func (ts *TrackingService) Stop() {
	ts.logger.Info("Stopping tracking service")

	ts.mu.Lock()
	select {
	case <-ts.stopChan:
		// Already stopped
		ts.mu.Unlock()
		return
	default:
		ts.stopped = true // Set stopped flag immediately
		close(ts.stopChan)
	}
	ts.mu.Unlock()
	
	// Stop session manager (will close current session)
	ts.sessionManager.Stop()
	
	// Stop activity tracker FIRST (removes Windows hooks immediately)
	ts.activityTracker.Stop()
	
	// Stop window tracker
	ts.windowTracker.Stop()
	
	// Stop event collector (stops creating new events)
	ts.eventCollector.Stop()
	
	// Wait for all goroutines with timeout
	done := make(chan struct{})
	go func() {
		ts.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// All goroutines stopped
	case <-time.After(2 * time.Second):
		ts.logger.Warn("Some goroutines did not stop within timeout")
	}

	// Flush any remaining events (but don't wait for send)
	ts.eventCollector.Flush()

	ts.logger.Info("Tracking service stopped")
}

// onAppFocus handles app focus change events from window tracker
func (ts *TrackingService) onAppFocus(appFocus *tracker.AppFocusInfo) {
	ts.mu.Lock()
	sequence := ts.appSequenceCounter
	ts.appSequenceCounter++
	ts.mu.Unlock()

	// App focus changes indicate user activity - reset activity timer
	ts.activityTracker.RecordActivity()

	// Create app focus event
	appFocusEvent := &models.AppFocusEvent{
		Type:        "APP_FOCUS",
		Application: appFocus.Application,
		PID:         appFocus.PID,
		Title:       appFocus.Title,
		Timestamp:   appFocus.Timestamp.UnixMilli(),
		Sequence:    sequence,
	}

	ts.logger.Debug("Creating AppFocusEvent with title",
		zap.String("application", appFocus.Application),
		zap.String("title", appFocus.Title),
		zap.Bool("title_empty", appFocus.Title == ""),
		zap.Int("sequence", sequence),
	)

	// Process through session manager
	ts.sessionManager.ProcessAppFocusEvent(appFocusEvent)
}

// onActivityStateChange handles activity state changes
func (ts *TrackingService) onActivityStateChange(state tracker.ActivityState) {
	ts.mu.Lock()
	oldState := ts.currentState
	ts.currentState = state
	ts.mu.Unlock()

	// Activity state changes don't create sessions, they're metadata
	// Sessions are created by app focus and browser events
	if oldState != state {
		ts.logger.Debug("Activity state changed",
			zap.String("old_state", string(oldState)),
			zap.String("new_state", string(state)),
		)
	}
}

// OnSessionEnd is called by SessionManager when a session ends
// Converts ActiveSession to TrackingEvent and adds to collector
func (ts *TrackingService) OnSessionEnd(session *ActiveSession) {
	ts.mu.RLock()
	stopped := ts.stopped
	ts.mu.RUnlock()
	
	if stopped {
		return
	}

	// Calculate duration
	duration := session.LastEventTime.Sub(session.StartTime).Milliseconds()
	
	// Apply minimum duration threshold (500ms) to avoid skipping very short but valid sessions
	// This handles cases where app switching happens very quickly
	const minDurationMs int64 = 500
	if duration <= 0 {
		// Skip sessions with zero or negative duration
		ts.logger.Debug("Skipping session with zero or negative duration",
			zap.String("source", session.Source),
			zap.String("application", session.Application),
			zap.Int64("duration_ms", duration),
		)
		return
	}
	
	// Apply minimum duration for very short sessions
	if duration < minDurationMs {
		ts.logger.Debug("Applying minimum duration threshold",
			zap.String("source", session.Source),
			zap.String("application", session.Application),
			zap.Int64("original_duration_ms", duration),
			zap.Int64("adjusted_duration_ms", minDurationMs),
		)
		duration = minDurationMs
	}

	// Create tracking event from session
	event := models.TrackingEvent{
		DeviceID:  ts.deviceID,
		Timestamp: session.StartTime.UnixMilli(),
		Status:    string(tracker.StateActive), // Sessions are always "active"
		Duration:  &duration,
	}

	// Set source
	source := session.Source
	event.Source = &source

	// Set application
	if session.Application != "" {
		event.Application = &session.Application
		}

	// Set browser-specific fields
	if session.Source == "browser" {
		if session.URL != "" {
			event.URL = &session.URL
		}
		if session.Title != "" {
			event.Title = &session.Title
		}
		if session.TabID > 0 {
			event.TabID = &session.TabID
		}
		if session.WindowID > 0 {
			event.WindowID = &session.WindowID
		}
	} else if session.Source == "app" {
		// Set app-specific fields (including title)
		if session.Title != "" {
			event.Title = &session.Title
			ts.logger.Debug("Setting title for app event",
				zap.String("application", session.Application),
				zap.String("title", session.Title),
				zap.Int("title_length", len(session.Title)),
				)
			} else {
			ts.logger.Debug("App session has empty title, not setting event title",
				zap.String("application", session.Application),
			)
		}
	}

	// Set sequence
	event.Sequence = &session.Sequence

	// Set start and end times
	startTime := session.StartTime.UnixMilli()
	endTime := session.LastEventTime.UnixMilli()
	event.StartTime = &startTime
	event.EndTime = &endTime

	eventTitleValue := "<nil>"
	if event.Title != nil {
		eventTitleValue = *event.Title
	}
	ts.logger.Info("Session ended, creating event",
		zap.String("source", session.Source),
		zap.String("application", session.Application),
		zap.String("url", session.URL),
		zap.String("session_title", session.Title),
		zap.Bool("title_set", event.Title != nil),
		zap.String("event_title", eventTitleValue),
		zap.Int64("duration_ms", duration),
		zap.Time("start_time", session.StartTime),
		zap.Time("end_time", session.LastEventTime),
	)

	// Add to event collector
	ts.eventCollector.AddEvent(event)
	ts.logger.Debug("Event added to collector",
		zap.String("source", session.Source),
		zap.String("application", session.Application),
	)
}

// onBatchReady handles when a batch is ready to be sent
func (ts *TrackingService) onBatchReady(events []models.TrackingEvent) {
	if len(events) == 0 {
		ts.logger.Debug("Batch ready but empty, skipping")
		return
	}

	ts.logger.Info("Batch ready to send",
		zap.Int("event_count", len(events)),
	)

	// Try to send to backend
	err := ts.apiClient.SendBatch(ts.deviceID, events)
	if err != nil {
		ts.logger.Warn("Failed to send batch, queuing locally",
			zap.Error(err),
			zap.Int("event_count", len(events)),
		)

		// Queue events locally for retry
		if queueErr := ts.eventQueue.Enqueue(ts.deviceID, events); queueErr != nil {
			ts.logger.Error("Failed to queue events",
				zap.Error(queueErr),
			)
		}
	}
}

// queueProcessor processes queued events in the background
func (ts *TrackingService) queueProcessor() {
	defer ts.wg.Done()

	ticker := time.NewTicker(60 * time.Second) // Check queue every minute
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ts.processQueue()
		case <-ts.stopChan:
			// Process queue one more time before stopping
			ts.processQueue()
			return
		}
	}
}

// processQueue attempts to send queued events
func (ts *TrackingService) processQueue() {
	// Get pending count
	pendingCount, err := ts.eventQueue.GetPendingCount(ts.deviceID)
	if err != nil {
		ts.logger.Error("Failed to get pending count", zap.Error(err))
		return
	}

	if pendingCount == 0 {
		return
	}

	ts.logger.Debug("Processing queued events",
		zap.Int("pending_count", pendingCount),
	)

	// Dequeue a batch
	events, ids, err := ts.eventQueue.Dequeue(ts.deviceID, 100)
	if err != nil {
		ts.logger.Error("Failed to dequeue events", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	// Try to send
	err = ts.apiClient.SendBatch(ts.deviceID, events)
	if err != nil {
		ts.logger.Warn("Failed to send queued batch",
			zap.Error(err),
			zap.Int("event_count", len(events)),
		)

		// Increment retry count
		if retryErr := ts.eventQueue.IncrementRetry(ids); retryErr != nil {
			ts.logger.Error("Failed to increment retry count", zap.Error(retryErr))
		}

		// Check if we should give up (too many retries)
		// This is handled by the cleanup function
		return
	}

	// Successfully sent, remove from queue
	if err := ts.eventQueue.Remove(ids); err != nil {
		ts.logger.Error("Failed to remove sent events from queue", zap.Error(err))
	} else {
		ts.logger.Info("Successfully sent queued events",
			zap.Int("event_count", len(events)),
		)
	}
}

// GetStatus returns the current tracking status
func (ts *TrackingService) GetStatus() map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	pendingCount, _ := ts.eventQueue.GetPendingCount(ts.deviceID)

	currentSession := ts.sessionManager.GetCurrentSession()
	sessionInfo := map[string]interface{}{}
	if currentSession != nil {
		sessionInfo["source"] = currentSession.Source
		sessionInfo["application"] = currentSession.Application
		if currentSession.Source == "browser" {
			sessionInfo["url"] = currentSession.URL
		}
	}

	return map[string]interface{}{
		"device_id":      ts.deviceID,
		"current_state":  string(ts.currentState),
		"pending_events": pendingCount,
		"collector_pending": ts.eventCollector.GetPendingCount(),
		"current_session": sessionInfo,
	}
}

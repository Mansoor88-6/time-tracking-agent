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
	deviceID        string
	logger          *zap.Logger
	
	currentWindow   *platform.WindowInfo
	currentState     tracker.ActivityState
	lastEventTime    time.Time
	stopped          bool
	mu               sync.RWMutex
	
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
		deviceID:      deviceID,
		logger:        logger,
		stopChan:      make(chan struct{}),
		currentState:  tracker.StateActive,
	}
}

// Start begins tracking
func (ts *TrackingService) Start() error {
	ts.logger.Info("Starting tracking service", zap.String("device_id", ts.deviceID))

	// Start window tracker
	if err := ts.windowTracker.Start(ts.onWindowChange); err != nil {
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

// onWindowChange handles window change events
func (ts *TrackingService) onWindowChange(window *platform.WindowInfo) {
	ts.mu.Lock()
	ts.currentWindow = window
	ts.mu.Unlock()

	// Window changes indicate user activity - reset activity timer
	// This ensures tab/app switches are treated as activity
	ts.activityTracker.RecordActivity()

	ts.createEvent(window, nil)
}

// onActivityStateChange handles activity state changes
func (ts *TrackingService) onActivityStateChange(state tracker.ActivityState) {
	ts.mu.Lock()
	oldState := ts.currentState
	ts.currentState = state
	ts.mu.Unlock()

	if oldState != state {
		ts.createEvent(nil, &state)
	}
}

// createEvent creates a tracking event
func (ts *TrackingService) createEvent(window *platform.WindowInfo, state *tracker.ActivityState) {
	ts.mu.RLock()
	stopped := ts.stopped
	currentWindow := ts.currentWindow
	currentState := ts.currentState
	ts.mu.RUnlock()
	
	// Don't create events if we're shutting down
	if stopped {
		return
	}

	// Use provided state or current state
	eventState := string(currentState)
	if state != nil {
		eventState = string(*state)
	}

	// Use provided window or current window
	eventWindow := currentWindow
	if window != nil {
		eventWindow = window
	}

	now := time.Now()
	timestamp := now.UnixMilli()

	// Calculate duration since last event
	var duration *int64
	if !ts.lastEventTime.IsZero() {
		dur := now.Sub(ts.lastEventTime).Milliseconds()
		if dur > 0 {
			duration = &dur
		}
	}

	event := models.TrackingEvent{
		DeviceID:  ts.deviceID,
		Timestamp: timestamp,
		Status:    eventState,
		Duration:  duration,
	}

	// Add window information if available
	if eventWindow != nil {
		if eventWindow.Application != "" {
			event.Application = &eventWindow.Application
		}
		if eventWindow.Title != "" {
			event.Title = &eventWindow.Title
		}
	}

	ts.eventCollector.AddEvent(event)
	ts.lastEventTime = now
}

// onBatchReady handles when a batch is ready to be sent
func (ts *TrackingService) onBatchReady(events []models.TrackingEvent) {
	if len(events) == 0 {
		return
	}

	ts.logger.Debug("Batch ready to send",
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

	return map[string]interface{}{
		"device_id":      ts.deviceID,
		"current_state":  string(ts.currentState),
		"pending_events": pendingCount,
		"collector_pending": ts.eventCollector.GetPendingCount(),
	}
}

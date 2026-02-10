package tracker

import (
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/platform"

	"go.uber.org/zap"
)

// ActivityState represents the current activity state
type ActivityState string

const (
	StateActive  ActivityState = "active"
	StateIdle    ActivityState = "idle"
	StateAway    ActivityState = "away"
	StateOffline ActivityState = "offline"
)

// ActivityTracker monitors user activity and determines idle/away states
type ActivityTracker struct {
	platform        platform.Platform
	idleThreshold   time.Duration
	awayThreshold   time.Duration
	lastActivity    time.Time
	currentState    ActivityState
	onStateChange   func(ActivityState)
	logger          *zap.Logger
	mu              sync.RWMutex
	checkTicker      *time.Ticker
	stopChan        chan struct{}
	wg              sync.WaitGroup
}

// NewActivityTracker creates a new activity tracker
func NewActivityTracker(
	platform platform.Platform,
	idleThreshold time.Duration,
	awayThreshold time.Duration,
	logger *zap.Logger,
) *ActivityTracker {
	return &ActivityTracker{
		platform:      platform,
		idleThreshold: idleThreshold,
		awayThreshold: awayThreshold,
		lastActivity:   time.Now(),
		currentState:  StateActive,
		logger:        logger,
		stopChan:      make(chan struct{}),
	}
}

// Start begins monitoring activity
func (at *ActivityTracker) Start(onStateChange func(ActivityState)) error {
	at.onStateChange = onStateChange

	// Start activity monitoring with platform
	if err := at.platform.StartActivityMonitoring(at.handleActivityEvent); err != nil {
		return err
	}

	// Start state checking loop - check more frequently for better responsiveness
	at.checkTicker = time.NewTicker(5 * time.Second) // Check state every 5 seconds
	at.wg.Add(1)
	go at.stateCheckLoop()

	at.logger.Info("Activity tracker started",
		zap.Duration("idle_threshold", at.idleThreshold),
		zap.Duration("away_threshold", at.awayThreshold),
	)

	return nil
}

// Stop stops monitoring activity
func (at *ActivityTracker) Stop() {
	at.mu.Lock()
	select {
	case <-at.stopChan:
		// Already closed
		at.mu.Unlock()
		return
	default:
		close(at.stopChan)
	}
	at.mu.Unlock()
	
	at.wg.Wait()
	at.platform.StopActivityMonitoring()
	if at.checkTicker != nil {
		at.checkTicker.Stop()
	}
	at.logger.Info("Activity tracker stopped")
}

// GetCurrentState returns the current activity state
func (at *ActivityTracker) GetCurrentState() ActivityState {
	at.mu.RLock()
	defer at.mu.RUnlock()
	return at.currentState
}

// GetLastActivity returns the timestamp of last activity
func (at *ActivityTracker) GetLastActivity() time.Time {
	at.mu.RLock()
	defer at.mu.RUnlock()
	return at.lastActivity
}

func (at *ActivityTracker) handleActivityEvent(event platform.ActivityEvent) {
	at.mu.Lock()
	at.lastActivity = event.Timestamp
	currentState := at.currentState
	at.mu.Unlock()

	// Any activity should immediately switch to active if we're not already active
	// This ensures we don't stay in idle/away state when user is clearly active
	if currentState != StateActive {
		at.setState(StateActive)
	}
}

// RecordActivity manually records activity (e.g., from window changes)
// This allows window switches to also count as user activity
func (at *ActivityTracker) RecordActivity() {
	at.mu.Lock()
	at.lastActivity = time.Now()
	currentState := at.currentState
	at.mu.Unlock()

	// Window changes indicate user activity, so switch to active if not already
	if currentState != StateActive {
		at.setState(StateActive)
	}
}

func (at *ActivityTracker) stateCheckLoop() {
	defer at.wg.Done()

	for {
		select {
		case <-at.checkTicker.C:
			at.checkState()
		case <-at.stopChan:
			return
		}
	}
}

func (at *ActivityTracker) checkState() {
	// Check if we should stop
	select {
	case <-at.stopChan:
		return
	default:
	}

	at.mu.Lock()
	idleDuration := time.Since(at.lastActivity)
	currentState := at.currentState
	at.mu.Unlock()

	// Check again
	select {
	case <-at.stopChan:
		return
	default:
	}

	var newState ActivityState
	switch {
	case idleDuration >= at.awayThreshold:
		newState = StateAway
	case idleDuration >= at.idleThreshold:
		newState = StateIdle
	default:
		newState = StateActive
	}

	if newState != currentState {
		at.setState(newState)
	}
}

func (at *ActivityTracker) setState(newState ActivityState) {
	// Check if we should stop before state change
	select {
	case <-at.stopChan:
		return
	default:
	}

	at.mu.Lock()
	oldState := at.currentState
	at.currentState = newState
	at.mu.Unlock()

	if oldState != newState {
		// Final check before callback
		select {
		case <-at.stopChan:
			return
		default:
		}

		at.logger.Info("Activity state changed",
			zap.String("old_state", string(oldState)),
			zap.String("new_state", string(newState)),
		)

		if at.onStateChange != nil {
			at.onStateChange(newState)
		}
	}
}

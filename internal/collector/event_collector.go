package collector

import (
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/models"

	"go.uber.org/zap"
)

// EventCollector collects and batches tracking events
type EventCollector struct {
	events         []models.TrackingEvent
	batchSize      int
	flushInterval  time.Duration
	onBatchReady   func([]models.TrackingEvent)
	logger         *zap.Logger
	mu             sync.Mutex
	flushTicker    *time.Ticker
	stopChan       chan struct{}
	wg             sync.WaitGroup
}

// NewEventCollector creates a new event collector
func NewEventCollector(
	batchSize int,
	flushInterval time.Duration,
	logger *zap.Logger,
) *EventCollector {
	return &EventCollector{
		batchSize:     batchSize,
		flushInterval: flushInterval,
		logger:        logger,
		stopChan:      make(chan struct{}),
	}
}

// Start begins the event collector with auto-flush
func (ec *EventCollector) Start(onBatchReady func([]models.TrackingEvent)) {
	ec.onBatchReady = onBatchReady
	ec.flushTicker = time.NewTicker(ec.flushInterval)

	ec.wg.Add(1)
	go ec.autoFlushLoop()

	ec.logger.Info("Event collector started",
		zap.Int("batch_size", ec.batchSize),
		zap.Duration("flush_interval", ec.flushInterval),
	)
}

// Stop stops the event collector
func (ec *EventCollector) Stop() {
	ec.mu.Lock()
	select {
	case <-ec.stopChan:
		// Already closed
		ec.mu.Unlock()
		return
	default:
		close(ec.stopChan)
	}
	ec.mu.Unlock()
	
	ec.wg.Wait()
	if ec.flushTicker != nil {
		ec.flushTicker.Stop()
	}
	
	// Flush any remaining events
	ec.mu.Lock()
	if len(ec.events) > 0 {
		events := make([]models.TrackingEvent, len(ec.events))
		copy(events, ec.events)
		ec.events = ec.events[:0]
		ec.mu.Unlock()
		if ec.onBatchReady != nil {
			ec.onBatchReady(events)
		}
	} else {
		ec.mu.Unlock()
	}

	ec.logger.Info("Event collector stopped")
}

// AddEvent adds a new event to the collection
func (ec *EventCollector) AddEvent(event models.TrackingEvent) {
	ec.mu.Lock()
	ec.events = append(ec.events, event)
	shouldFlush := len(ec.events) >= ec.batchSize
	events := make([]models.TrackingEvent, 0)
	if shouldFlush {
		events = make([]models.TrackingEvent, len(ec.events))
		copy(events, ec.events)
		ec.events = ec.events[:0]
	}
	ec.mu.Unlock()

	if shouldFlush {
		ec.logger.Debug("Batch size reached, flushing events",
			zap.Int("count", len(events)),
		)
		if ec.onBatchReady != nil {
			ec.onBatchReady(events)
		}
	}
}

// Flush manually flushes all pending events
func (ec *EventCollector) Flush() {
	ec.mu.Lock()
	if len(ec.events) == 0 {
		ec.mu.Unlock()
		return
	}
	events := make([]models.TrackingEvent, len(ec.events))
	copy(events, ec.events)
	ec.events = ec.events[:0]
	ec.mu.Unlock()

	ec.logger.Debug("Manual flush triggered",
		zap.Int("count", len(events)),
	)
	if ec.onBatchReady != nil {
		ec.onBatchReady(events)
	}
}

// GetPendingCount returns the number of pending events
func (ec *EventCollector) GetPendingCount() int {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return len(ec.events)
}

func (ec *EventCollector) autoFlushLoop() {
	defer ec.wg.Done()

	for {
		select {
		case <-ec.flushTicker.C:
			ec.Flush()
		case <-ec.stopChan:
			return
		}
	}
}

package queue

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/models"

	"go.uber.org/zap"
)

// EventQueue manages a local queue of pending events
type EventQueue struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewEventQueue creates a new event queue
func NewEventQueue(db *sql.DB, logger *zap.Logger) *EventQueue {
	return &EventQueue{
		db:     db,
		logger: logger,
	}
}

// Enqueue adds events to the queue
func (eq *EventQueue) Enqueue(deviceID string, events []models.TrackingEvent) error {
	tx, err := eq.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO pending_events (event_data, device_id, created_at, retry_count)
		VALUES (?, ?, ?, 0)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, event := range events {
		eventData, err := json.Marshal(event)
		if err != nil {
			eq.logger.Error("Failed to marshal event", zap.Error(err))
			continue
		}

		_, err = stmt.Exec(string(eventData), deviceID, time.Now())
		if err != nil {
			eq.logger.Error("Failed to enqueue event", zap.Error(err))
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	eq.logger.Debug("Events enqueued",
		zap.Int("count", len(events)),
		zap.String("device_id", deviceID),
	)

	return nil
}

// Dequeue retrieves a batch of events from the queue
func (eq *EventQueue) Dequeue(deviceID string, limit int) ([]models.TrackingEvent, []int64, error) {
	rows, err := eq.db.Query(`
		SELECT id, event_data, retry_count
		FROM pending_events
		WHERE device_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, deviceID, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query pending events: %w", err)
	}
	defer rows.Close()

	var events []models.TrackingEvent
	var ids []int64

	for rows.Next() {
		var id int64
		var eventData string
		var retryCount int

		if err := rows.Scan(&id, &eventData, &retryCount); err != nil {
			eq.logger.Error("Failed to scan row", zap.Error(err))
			continue
		}

		var event models.TrackingEvent
		if err := json.Unmarshal([]byte(eventData), &event); err != nil {
			eq.logger.Error("Failed to unmarshal event", zap.Error(err), zap.Int64("id", id))
			// Remove corrupted event
			eq.db.Exec("DELETE FROM pending_events WHERE id = ?", id)
			continue
		}

		events = append(events, event)
		ids = append(ids, id)
	}

	return events, ids, nil
}

// Remove removes events from the queue by their IDs
func (eq *EventQueue) Remove(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	// Build query with placeholders
	query := "DELETE FROM pending_events WHERE id IN ("
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	result, err := eq.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to remove events: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	eq.logger.Debug("Events removed from queue",
		zap.Int64("count", rowsAffected),
	)

	return nil
}

// IncrementRetry increments the retry count for events
func (eq *EventQueue) IncrementRetry(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	query := "UPDATE pending_events SET retry_count = retry_count + 1, last_attempt = ? WHERE id IN ("
	args := make([]interface{}, len(ids)+1)
	args[0] = time.Now()
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i+1] = id
	}
	query += ")"

	_, err := eq.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to increment retry: %w", err)
	}

	return nil
}

// GetPendingCount returns the number of pending events for a device
func (eq *EventQueue) GetPendingCount(deviceID string) (int, error) {
	var count int
	err := eq.db.QueryRow(`
		SELECT COUNT(*) FROM pending_events WHERE device_id = ?
	`, deviceID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get pending count: %w", err)
	}
	return count, nil
}

// CleanupOldEvents removes events older than the specified duration
func (eq *EventQueue) CleanupOldEvents(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	result, err := eq.db.Exec(`
		DELETE FROM pending_events
		WHERE created_at < ? AND retry_count > 10
	`, cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup old events: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		eq.logger.Info("Cleaned up old events",
			zap.Int64("count", rowsAffected),
		)
	}

	return nil
}

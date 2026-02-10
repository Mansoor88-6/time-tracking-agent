package models

// TrackingEvent represents a single tracking event matching the backend EventDto structure
type TrackingEvent struct {
	DeviceID    string  `json:"deviceId"`
	Timestamp   int64   `json:"timestamp"` // Unix timestamp in milliseconds
	Status       string  `json:"status"`    // active, idle, away, offline
	Application *string `json:"application,omitempty"`
	Title        *string `json:"title,omitempty"`
	URL          *string `json:"url,omitempty"`
	Duration     *int64  `json:"duration,omitempty"` // milliseconds
	ProjectID    *string `json:"projectId,omitempty"`
}

// BatchEventRequest represents a batch of events to send to the backend
type BatchEventRequest struct {
	Events        []TrackingEvent `json:"events"`
	DeviceID      string          `json:"deviceId"`
	BatchTimestamp int64          `json:"batchTimestamp"` // Unix timestamp in milliseconds
}

// EventStatus constants matching backend EventStatus enum
const (
	StatusActive  = "active"
	StatusIdle    = "idle"
	StatusAway    = "away"
	StatusOffline = "offline"
)

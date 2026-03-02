package models

// BrowserEvent represents a browser event from the extension
type BrowserEvent struct {
	Source    string `json:"source"`    // "browser"
	Browser   string `json:"browser"`
	TabID     int    `json:"tabId"`
	WindowID  int    `json:"windowId"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Timestamp int64  `json:"timestamp"` // Unix ms
	Sequence  int    `json:"sequence"`
}

// AppFocusEvent represents an application focus event from OS
type AppFocusEvent struct {
	Type        string `json:"type"`        // "APP_FOCUS"
	Application string `json:"application"`
	PID         int    `json:"pid"`
	Title       string `json:"title"`
	Timestamp   int64  `json:"timestamp"`   // Unix ms
	Sequence    int    `json:"sequence"`
}

// TrackingEvent represents a single tracking event matching the backend EventDto structure
type TrackingEvent struct {
	DeviceID    string  `json:"deviceId"`
	Timestamp   int64   `json:"timestamp"` // Unix timestamp in milliseconds (session start)
	Status       string  `json:"status"`    // active, idle, away, offline
	Application *string `json:"application,omitempty"`
	Title        *string `json:"title,omitempty"`
	URL          *string `json:"url,omitempty"`
	Duration     *int64  `json:"duration,omitempty"` // milliseconds
	ProjectID    *string `json:"projectId,omitempty"`
	Source       *string `json:"source,omitempty"`       // "browser" | "app"
	TabID        *int    `json:"tabId,omitempty"`
	WindowID     *int    `json:"windowId,omitempty"`
	Sequence     *int    `json:"sequence,omitempty"`
	StartTime    *int64  `json:"startTime,omitempty"`    // Unix ms
	EndTime      *int64  `json:"endTime,omitempty"`      // Unix ms
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

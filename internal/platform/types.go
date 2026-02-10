package platform

import "time"

// Platform defines the interface for platform-specific operations
type Platform interface {
	// GetActiveWindow returns information about the currently active window
	GetActiveWindow() (*WindowInfo, error)
	
	// StartActivityMonitoring starts monitoring mouse and keyboard activity
	// It calls the callback function whenever activity is detected
	StartActivityMonitoring(callback func(ActivityEvent)) error
	
	// StopActivityMonitoring stops the activity monitoring
	StopActivityMonitoring() error
	
	// GetDeviceID returns a unique identifier for this device
	GetDeviceID() (string, error)
	
	// GetSystemInfo returns system information
	GetSystemInfo() (*SystemInfo, error)
	
	// OpenBrowser opens the default browser with the given URL
	OpenBrowser(url string) error
}

// WindowInfo contains information about a window
type WindowInfo struct {
	Title       string
	Application string
	ProcessID   int
	ProcessPath string
	IsVisible   bool
	Timestamp   time.Time
}

// ActivityEvent represents a user activity event
type ActivityEvent struct {
	Type      ActivityType
	Timestamp time.Time
}

// ActivityType represents the type of activity
type ActivityType string

const (
	ActivityMouseMove  ActivityType = "mouse_move"
	ActivityMouseClick ActivityType = "mouse_click"
	ActivityKeyPress   ActivityType = "key_press"
)

// SystemInfo contains system information
type SystemInfo struct {
	OS       string
	OSVersion string
	Arch     string
	Hostname string
}

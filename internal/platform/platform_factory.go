package platform

import (
	"runtime"
)

// NewPlatform creates a platform-specific implementation based on the current OS
func NewPlatform() (Platform, error) {
	switch runtime.GOOS {
	case "windows":
		return newWindowsPlatform()
	case "darwin":
		return newDarwinPlatform()
	case "linux":
		return newLinuxPlatform()
	default:
		return nil, &UnsupportedPlatformError{OS: runtime.GOOS}
	}
}

// UnsupportedPlatformError represents an error for unsupported platforms
type UnsupportedPlatformError struct {
	OS string
}

func (e *UnsupportedPlatformError) Error() string {
	return "unsupported platform: " + e.OS
}

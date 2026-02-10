//go:build !windows && !darwin && !linux
// +build !windows,!darwin,!linux

package platform

// Stub implementations for unsupported platforms
func newWindowsPlatform() (Platform, error) {
	return nil, &UnsupportedPlatformError{OS: "windows (not compiled for this platform)"}
}

func newDarwinPlatform() (Platform, error) {
	return nil, &UnsupportedPlatformError{OS: "darwin (not compiled for this platform)"}
}

func newLinuxPlatform() (Platform, error) {
	return nil, &UnsupportedPlatformError{OS: "linux (not compiled for this platform)"}
}

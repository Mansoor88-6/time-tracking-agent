//go:build windows
// +build windows

package platform

// Stubs for non-Windows platforms when building for Windows
func newDarwinPlatform() (Platform, error) {
	return nil, &UnsupportedPlatformError{OS: "darwin (building for windows)"}
}

func newLinuxPlatform() (Platform, error) {
	return nil, &UnsupportedPlatformError{OS: "linux (building for windows)"}
}

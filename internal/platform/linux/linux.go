//go:build linux
// +build linux

package linux

import (
	"fmt"
	"os"
	"runtime"

	"Mansoor88-6/time-tracking-agent/internal/platform"
)


type LinuxPlatform struct{}

func New() *LinuxPlatform {
	return &LinuxPlatform{}
}

func (p *LinuxPlatform) GetActiveWindow() (*platform.WindowInfo, error) {
	// TODO: Implement Linux window tracking using X11 or Wayland
	return nil, fmt.Errorf("Linux window tracking not yet implemented")
}

func (p *LinuxPlatform) StartActivityMonitoring(callback func(platform.ActivityEvent)) error {
	// TODO: Implement Linux activity monitoring using X11 events
	return fmt.Errorf("Linux activity monitoring not yet implemented")
}

func (p *LinuxPlatform) StopActivityMonitoring() error {
	return nil
}

func (p *LinuxPlatform) GetDeviceID() (string, error) {
	hostname, _ := os.Hostname()
	if hostname != "" {
		return hostname, nil
	}
	return "unknown-device", nil
}

func (p *LinuxPlatform) GetSystemInfo() (*platform.SystemInfo, error) {
	hostname, _ := os.Hostname()
	return &platform.SystemInfo{
		OS:        "linux",
		OSVersion: runtime.GOOS,
		Arch:      runtime.GOARCH,
		Hostname:  hostname,
	}, nil
}

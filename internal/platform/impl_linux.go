//go:build linux
// +build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

type linuxImpl struct{}

func newLinuxPlatform() (Platform, error) {
	return nil, fmt.Errorf("Linux implementation not yet available")
}

func (p *linuxImpl) GetActiveWindow() (*WindowInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *linuxImpl) StartActivityMonitoring(callback func(ActivityEvent)) error {
	return fmt.Errorf("not implemented")
}

func (p *linuxImpl) StopActivityMonitoring() error {
	return nil
}

func (p *linuxImpl) GetDeviceID() (string, error) {
	hostname, _ := os.Hostname()
	if hostname != "" {
		return hostname, nil
	}
	return "unknown-device", nil
}

func (p *linuxImpl) GetSystemInfo() (*SystemInfo, error) {
	hostname, _ := os.Hostname()
	return &SystemInfo{
		OS:        "linux",
		OSVersion: runtime.GOOS,
		Arch:      runtime.GOARCH,
		Hostname:  hostname,
	}, nil
}

func (p *linuxImpl) OpenBrowser(url string) error {
	// Try common Linux browser commands
	browsers := []string{"xdg-open", "x-www-browser", "firefox", "google-chrome", "chromium"}
	for _, browser := range browsers {
		cmd := exec.Command(browser, url)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no browser found")
}

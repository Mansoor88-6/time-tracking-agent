//go:build darwin
// +build darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

type darwinImpl struct{}

func newDarwinPlatform() (Platform, error) {
	return nil, fmt.Errorf("macOS implementation not yet available")
}

func (p *darwinImpl) GetActiveWindow() (*WindowInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *darwinImpl) StartActivityMonitoring(callback func(ActivityEvent)) error {
	return fmt.Errorf("not implemented")
}

func (p *darwinImpl) StopActivityMonitoring() error {
	return nil
}

func (p *darwinImpl) GetDeviceID() (string, error) {
	hostname, _ := os.Hostname()
	if hostname != "" {
		return hostname, nil
	}
	return "unknown-device", nil
}

func (p *darwinImpl) GetSystemInfo() (*SystemInfo, error) {
	hostname, _ := os.Hostname()
	return &SystemInfo{
		OS:        "darwin",
		OSVersion: runtime.GOOS,
		Arch:      runtime.GOARCH,
		Hostname:  hostname,
	}, nil
}

func (p *darwinImpl) OpenBrowser(url string) error {
	// Use macOS open command
	cmd := exec.Command("open", url)
	return cmd.Run()
}

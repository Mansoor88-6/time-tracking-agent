package device

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

// DeviceManager handles device ID generation and management
type DeviceManager struct{}

// NewDeviceManager creates a new device manager
func NewDeviceManager() *DeviceManager {
	return &DeviceManager{}
}

// GetOrGenerateDeviceID gets the device ID from config or generates a new one
func (dm *DeviceManager) GetOrGenerateDeviceID(existingID string) (string, error) {
	if existingID != "" {
		return existingID, nil
	}

	// Try to get platform-specific device ID
	deviceID, err := dm.getPlatformDeviceID()
	if err == nil && deviceID != "" {
		return deviceID, nil
	}

	// Fallback: generate UUID
	newUUID := uuid.New()
	return newUUID.String(), nil
}

// getPlatformDeviceID gets a platform-specific device identifier
func (dm *DeviceManager) getPlatformDeviceID() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return dm.getWindowsDeviceID()
	case "darwin":
		return dm.getDarwinDeviceID()
	case "linux":
		return dm.getLinuxDeviceID()
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// getWindowsDeviceID gets Windows machine GUID
func (dm *DeviceManager) getWindowsDeviceID() (string, error) {
	// Try wmic csproduct get uuid
	cmd := exec.Command("wmic", "csproduct", "get", "uuid")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && line != "UUID" && len(line) > 10 {
				return strings.TrimSpace(line), nil
			}
		}
	}

	// Try wmic bios get serialnumber
	cmd = exec.Command("wmic", "bios", "get", "serialnumber")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && line != "SerialNumber" && len(line) > 3 {
				return strings.TrimSpace(line), nil
			}
		}
	}

	// Fallback to hostname
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		return "windows-" + hostname, nil
	}

	return "", fmt.Errorf("could not determine Windows device ID")
}

// getDarwinDeviceID gets macOS device ID
func (dm *DeviceManager) getDarwinDeviceID() (string, error) {
	// Try system_profiler SPHardwareDataType
	cmd := exec.Command("system_profiler", "SPHardwareDataType")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Hardware UUID") {
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					return strings.TrimSpace(parts[1]), nil
				}
			}
		}
	}

	// Fallback to hostname
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		return "darwin-" + hostname, nil
	}

	return "", fmt.Errorf("could not determine macOS device ID")
}

// getLinuxDeviceID gets Linux device ID
func (dm *DeviceManager) getLinuxDeviceID() (string, error) {
	// Try /etc/machine-id
	machineID, err := os.ReadFile("/etc/machine-id")
	if err == nil && len(machineID) > 0 {
		return strings.TrimSpace(string(machineID)), nil
	}

	// Try /var/lib/dbus/machine-id
	machineID, err = os.ReadFile("/var/lib/dbus/machine-id")
	if err == nil && len(machineID) > 0 {
		return strings.TrimSpace(string(machineID)), nil
	}

	// Fallback to hostname
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		return "linux-" + hostname, nil
	}

	return "", fmt.Errorf("could not determine Linux device ID")
}

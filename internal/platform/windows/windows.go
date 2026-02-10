//go:build windows
// +build windows

package windows

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"Mansoor88-6/time-tracking-agent/internal/platform"

	"golang.org/x/sys/windows"
)


var (
	user32                  = windows.NewLazyDLL("user32.dll")
	kernel32                = windows.NewLazyDLL("kernel32.dll")
	psapi                   = windows.NewLazyDLL("psapi.dll")
	
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	
	procGetModuleFileNameEx = psapi.NewProc("GetModuleFileNameExW")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
)

const (
	WH_MOUSE_LL    = 14
	WH_KEYBOARD_LL = 13
	WM_MOUSEMOVE   = 0x0200
	WM_LBUTTONDOWN = 0x0201
	WM_RBUTTONDOWN = 0x0204
	WM_KEYDOWN     = 0x0100
	PROCESS_QUERY_INFORMATION = 0x0400
	PROCESS_VM_READ            = 0x0010
)

type WindowsPlatform struct {
	mouseHook    windows.Handle
	keyboardHook windows.Handle
	activityCallback func(platform.ActivityEvent)
}

func New() *WindowsPlatform {
	return &WindowsPlatform{}
}

func (p *WindowsPlatform) GetActiveWindow() (*platform.WindowInfo, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return nil, fmt.Errorf("failed to get foreground window")
	}

	// Get window title
	length, _, _ := procGetWindowTextLength.Call(hwnd)
	if length == 0 {
		return &platform.WindowInfo{
			Title:       "",
			Application: "",
			ProcessID:   0,
			ProcessPath: "",
			IsVisible:   true,
			Timestamp:   time.Now(),
		}, nil
	}

	length++ // Include null terminator
	buf := make([]uint16, length)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(length))
	title := windows.UTF16ToString(buf)

	// Get process ID
	var processID uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&processID)))

	// Get process path
	processPath := p.getProcessPath(int(processID))

	// Get application name from process path
	application := p.getApplicationName(processPath)

	// Check if window is visible
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	isVisible := visible != 0

	return &platform.WindowInfo{
		Title:       title,
		Application: application,
		ProcessID:   int(processID),
		ProcessPath: processPath,
		IsVisible:   isVisible,
		Timestamp:   time.Now(),
	}, nil
}

func (p *WindowsPlatform) getProcessPath(processID int) string {
	if processID == 0 {
		return ""
	}

	handle, _, _ := procOpenProcess.Call(
		PROCESS_QUERY_INFORMATION|PROCESS_VM_READ,
		0,
		uintptr(processID),
	)
	if handle == 0 {
		return ""
	}
	defer procCloseHandle.Call(handle)

	buf := make([]uint16, 260)
	ret, _, _ := procGetModuleFileNameEx.Call(
		handle,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		260,
	)
	if ret == 0 {
		return ""
	}

	return windows.UTF16ToString(buf)
}

func (p *WindowsPlatform) getApplicationName(processPath string) string {
	if processPath == "" {
		return ""
	}

	parts := strings.Split(processPath, "\\")
	if len(parts) > 0 {
		exeName := parts[len(parts)-1]
		// Remove .exe extension
		if strings.HasSuffix(exeName, ".exe") {
			exeName = exeName[:len(exeName)-4]
		}
		return exeName
	}
	return ""
}

func (p *WindowsPlatform) StartActivityMonitoring(callback func(platform.ActivityEvent)) error {
	p.activityCallback = callback

	// Set up low-level mouse hook
	mouseHookProc := syscall.NewCallback(p.mouseHookProc)
	mouseHook, _, _ := procSetWindowsHookEx.Call(
		WH_MOUSE_LL,
		mouseHookProc,
		0,
		0,
	)
	if mouseHook == 0 {
		return fmt.Errorf("failed to set mouse hook")
	}
	p.mouseHook = windows.Handle(mouseHook)

	// Set up low-level keyboard hook
	keyboardHookProc := syscall.NewCallback(p.keyboardHookProc)
	keyboardHook, _, _ := procSetWindowsHookEx.Call(
		WH_KEYBOARD_LL,
		keyboardHookProc,
		0,
		0,
	)
	if keyboardHook == 0 {
		procUnhookWindowsHookEx.Call(uintptr(p.mouseHook))
		return fmt.Errorf("failed to set keyboard hook")
	}
	p.keyboardHook = windows.Handle(keyboardHook)

	return nil
}

func (p *WindowsPlatform) StopActivityMonitoring() error {
	if p.mouseHook != 0 {
		procUnhookWindowsHookEx.Call(uintptr(p.mouseHook))
		p.mouseHook = 0
	}
	if p.keyboardHook != 0 {
		procUnhookWindowsHookEx.Call(uintptr(p.keyboardHook))
		p.keyboardHook = 0
	}
	p.activityCallback = nil
	return nil
}

func (p *WindowsPlatform) mouseHookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && p.activityCallback != nil {
		switch wParam {
		case WM_MOUSEMOVE:
			p.activityCallback(platform.ActivityEvent{
				Type:      platform.ActivityMouseMove,
				Timestamp: time.Now(),
			})
		case WM_LBUTTONDOWN, WM_RBUTTONDOWN:
			p.activityCallback(platform.ActivityEvent{
				Type:      platform.ActivityMouseClick,
				Timestamp: time.Now(),
			})
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func (p *WindowsPlatform) keyboardHookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && wParam == WM_KEYDOWN && p.activityCallback != nil {
		p.activityCallback(platform.ActivityEvent{
			Type:      platform.ActivityKeyPress,
			Timestamp: time.Now(),
		})
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func (p *WindowsPlatform) GetDeviceID() (string, error) {
	// Try to get machine GUID from Windows
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

	// Fallback: use hostname + MAC address or generate UUID
	hostname, _ := os.Hostname()
	if hostname != "" {
		return hostname, nil
	}

	// Last resort: return a placeholder (should be replaced with UUID generation)
	return "unknown-device", nil
}

func (p *WindowsPlatform) GetSystemInfo() (*platform.SystemInfo, error) {
	hostname, _ := os.Hostname()
	return &platform.SystemInfo{
		OS:        "windows",
		OSVersion: runtime.GOOS,
		Arch:      runtime.GOARCH,
		Hostname:  hostname,
	}, nil
}

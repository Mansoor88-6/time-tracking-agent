package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/auth"
	"Mansoor88-6/time-tracking-agent/internal/client"
	"Mansoor88-6/time-tracking-agent/internal/collector"
	"Mansoor88-6/time-tracking-agent/internal/config"
	"Mansoor88-6/time-tracking-agent/internal/database"
	"Mansoor88-6/time-tracking-agent/internal/device"
	"Mansoor88-6/time-tracking-agent/internal/logger"
	"Mansoor88-6/time-tracking-agent/internal/platform"
	"Mansoor88-6/time-tracking-agent/internal/queue"
	"Mansoor88-6/time-tracking-agent/internal/server"
	"Mansoor88-6/time-tracking-agent/internal/service"
	"Mansoor88-6/time-tracking-agent/internal/tracker"
	"Mansoor88-6/time-tracking-agent/internal/ui"

	"go.uber.org/zap"
)

// Version is set by the build system via -ldflags
var Version = "dev"

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file (auto-detected if empty)")
	flag.Parse()

	// Resolve config path (auto-detect if not specified)
	resolvedConfigPath, err := config.ResolveConfigPath(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find config: %v\n", err)
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.LoadConfig(resolvedConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Determine logs path relative to base dir (needed early for file logger)
	logsPath := filepath.Join(cfg.BaseDir, "logs")

	// Initialize logger with file output for production
	log, err := logger.NewWithFile(cfg.Log.Level, cfg.Log.Format, logsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	log.Info("Starting time-tracking agent",
		zap.String("version", Version),
		zap.String("env", cfg.Env),
		zap.String("config_path", resolvedConfigPath),
		zap.String("base_dir", cfg.BaseDir),
		zap.String("logs_path", logsPath),
	)

	// Initialize database
	db, err := database.New(cfg.StoragePath, log.Logger)
	if err != nil {
		log.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("Failed to close database", zap.Error(err))
		}
	}()

	// Initialize platform
	platformInstance, err := platform.NewPlatform()
	if err != nil {
		log.Fatal("Failed to initialize platform", zap.Error(err))
	}

	// Get or generate device ID
	deviceManager := device.NewDeviceManager()
	deviceID, err := deviceManager.GetOrGenerateDeviceID(cfg.Device.ID)
	if err != nil {
		log.Fatal("Failed to get device ID", zap.Error(err))
	}

	// Save generated device ID back to config so it persists across restarts
	if cfg.Device.ID == "" {
		log.Info("Generated device ID", zap.String("device_id", deviceID))
		cfg.Device.ID = deviceID
		if err := saveConfigField(resolvedConfigPath, "id:", fmt.Sprintf("  id: \"%s\"", deviceID)); err != nil {
			log.Warn("Failed to save device ID to config", zap.Error(err))
		} else {
			log.Info("Device ID saved to config")
		}
	} else {
		log.Info("Using configured device ID", zap.String("device_id", deviceID))
	}

	// Check if device token exists, if not, perform authorization
	deviceToken := cfg.Auth.DeviceToken
	if deviceToken == "" {
		log.Info("No device token found, starting authorization flow")

		// Create device authorization service
		deviceAuth := auth.NewDeviceAuthService(
			platformInstance,
			cfg.Auth.CallbackPort,
			cfg.Backend.BaseURL,
			log.Logger,
		)

		// Retry authorization up to 3 times (user may close the browser, etc.)
		var code string
		for attempt := 1; attempt <= 3; attempt++ {
			code, err = deviceAuth.AuthorizeDevice(deviceID, cfg.Device.Name)
			if err == nil {
				break
			}
			log.Warn("Device authorization attempt failed",
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			if attempt < 3 {
				log.Info("Retrying authorization in 5 seconds...")
				time.Sleep(5 * time.Second)
			}
		}
		if err != nil {
			log.Fatal("Device authorization failed after all retries", zap.Error(err))
		}

		// Exchange code for token
		tokenResp, err := deviceAuth.ExchangeCodeForToken(code, deviceID)
		if err != nil {
			log.Fatal("Token exchange failed", zap.Error(err))
		}

		deviceToken = tokenResp.AccessToken
		log.Info("Device authorized successfully",
			zap.String("device_id", tokenResp.DeviceID),
			zap.Int("expires_in", tokenResp.ExpiresIn),
		)

		// Save token to config file
		cfg.Auth.DeviceToken = deviceToken
		if err := saveConfig(resolvedConfigPath, cfg); err != nil {
			log.Warn("Failed to save device token to config", zap.Error(err))
		} else {
			log.Info("Device token saved to config")
		}
	} else {
		log.Info("Using existing device token")
	}

	// Initialize API client
	apiClient := client.NewAPIClient(
		cfg.Backend.BaseURL,
		cfg.Backend.APIKey,
		time.Duration(cfg.Backend.Timeout)*time.Second,
		log.Logger,
	)

	// Set device token in API client
	if deviceToken != "" {
		apiClient.SetDeviceToken(deviceToken)
	}

	// Initialize event queue
	eventQueue := queue.NewEventQueue(db.DB, log.Logger)

	// Initialize event collector
	eventCollector := collector.NewEventCollector(
		cfg.Tracking.BatchSize,
		time.Duration(cfg.Tracking.BatchFlushInterval)*time.Second,
		log.Logger,
	)

	// Create a callback variable that will be set after tracking service is created
	var sessionEndCallback func(*service.ActiveSession)

	// Initialize session manager with temporary callback
	sessionManager := service.NewSessionManager(
		func(session *service.ActiveSession) {
			if sessionEndCallback != nil {
				sessionEndCallback(session)
			}
		},
		log.Logger,
		time.Duration(cfg.Tracking.SessionInactivityTimeout)*time.Second,
	)

	// Initialize window tracker
	windowTracker := tracker.NewWindowTracker(
		platformInstance,
		time.Duration(cfg.Tracking.WindowPollInterval)*time.Second,
		log.Logger,
	)

	// Initialize activity tracker
	activityTracker := tracker.NewActivityTracker(
		platformInstance,
		time.Duration(cfg.Tracking.IdleThreshold)*time.Second,
		time.Duration(cfg.Tracking.AwayThreshold)*time.Second,
		log.Logger,
	)

	// Initialize tracking service with session manager
	trackingService := service.NewTrackingService(
		platformInstance,
		windowTracker,
		activityTracker,
		eventCollector,
		apiClient,
		eventQueue,
		sessionManager,
		deviceID,
		log.Logger,
	)

	// Set up session manager callback to use tracking service's OnSessionEnd
	sessionEndCallback = trackingService.OnSessionEnd

	// Initialize browser event server (for browser extension)
	var browserHTTPServer *http.Server

	if cfg.Server.Enabled {
		browserEventServer := server.NewBrowserEventServer(sessionManager, log.Logger)

		// Try the configured port; if busy, try nearby ports
		browserListener, browserPort, err := listenWithFallback(cfg.Server.Port, log)
		if err != nil {
			log.Error("Failed to start browser event server on any port", zap.Error(err))
		} else {
			browserHTTPServer = &http.Server{
				Handler:      browserEventServer,
				ReadTimeout:  15 * time.Second,
				WriteTimeout: 15 * time.Second,
				IdleTimeout:  60 * time.Second,
			}

			// Start browser event server in goroutine
			go func() {
				log.Info("Browser event server started for extension",
					zap.Int("configured_port", cfg.Server.Port),
					zap.Int("actual_port", browserPort),
				)
				if err := browserHTTPServer.Serve(browserListener); err != nil && err != http.ErrServerClosed {
					log.Error("Browser event server error", zap.Error(err))
				}
			}()

			if browserPort != cfg.Server.Port {
				log.Warn("Browser extension server is on a different port than configured!",
					zap.Int("configured", cfg.Server.Port),
					zap.Int("actual", browserPort),
					zap.String("note", "Make sure the browser extension is configured to use the correct port"),
				)
			}
		}
	} else {
		log.Info("Browser event server disabled in configuration")
	}

	// Purge any queued events that are too old for the backend to accept.
	// The backend rejects events with timestamps older than ~24h, so we drop
	// them now to avoid flooding the backend with guaranteed-to-fail requests.
	if err := eventQueue.PurgeExpiredEvents(24 * time.Hour); err != nil {
		log.Warn("Failed to purge expired queued events", zap.Error(err))
	}

	// Start tracking service
	if err := trackingService.Start(); err != nil {
		log.Fatal("Failed to start tracking service", zap.Error(err))
	}

	// Create and start tray manager (Windows only)
	var trayQuitChan chan struct{}
	trayManager := ui.NewTrayManager(
		log.Logger,
		sessionManager,
		trackingService,
		cfg.Backend.BaseURL,
		logsPath,
	)

	// Start tray in background (on Windows)
	trayCtx, trayCancel := context.WithCancel(context.Background())
	trayQuitChan = make(chan struct{})
	go func() {
		defer close(trayQuitChan)
		if err := trayManager.Start(trayCtx); err != nil {
			log.Warn("Tray manager error", zap.Error(err))
		}
	}()

	log.Info("Time-tracking agent started successfully",
		zap.String("version", Version),
		zap.String("device_id", deviceID),
		zap.String("backend_url", cfg.Backend.BaseURL),
	)

	// Wait for interrupt signal or tray quit
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Wait for signal or tray quit
	select {
	case sig := <-quit:
		log.Info("Received shutdown signal", zap.String("signal", sig.String()))
	case <-trayQuitChan:
		log.Info("Received quit from tray menu")
		trayCancel()
	}

	log.Info("Shutting down time-tracking agent...")

	// Stop browser event server if enabled
	if browserHTTPServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := browserHTTPServer.Shutdown(ctx); err != nil {
			log.Warn("Browser event server shutdown error", zap.Error(err))
		} else {
			log.Info("Browser event server stopped")
		}
	}

	// Stop tracking service immediately (synchronous, with timeout)
	done := make(chan struct{})
	go func() {
		trackingService.Stop()
		close(done)
	}()

	// Wait for shutdown with timeout
	select {
	case <-done:
		log.Info("Tracking service stopped successfully")
	case <-time.After(3 * time.Second):
		log.Warn("Shutdown timeout reached, forcing immediate exit")
		os.Exit(1)
	}

	// Cleanup old queued events (older than 7 days with >10 retries) - quick, don't wait
	go func() {
		if err := eventQueue.CleanupOldEvents(7 * 24 * time.Hour); err != nil {
			log.Error("Failed to cleanup old events", zap.Error(err))
		}
	}()

	log.Info("Time-tracking agent stopped")

	// Force exit immediately to ensure process terminates
	// Windows hooks can prevent normal exit, so we must force it
	os.Exit(0)
}

// listenWithFallback tries to bind to the preferred port, then nearby ports,
// then lets the OS pick a free port. Returns the listener and actual port.
func listenWithFallback(preferredPort int, log *logger.Logger) (net.Listener, int, error) {
	// Try preferred port
	addr := fmt.Sprintf("localhost:%d", preferredPort)
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		return listener, preferredPort, nil
	}
	log.Warn("Preferred browser server port unavailable",
		zap.Int("port", preferredPort),
		zap.Error(err),
	)

	// Try nearby ports
	for offset := 1; offset <= 10; offset++ {
		altPort := preferredPort + offset
		altAddr := fmt.Sprintf("localhost:%d", altPort)
		listener, err = net.Listen("tcp", altAddr)
		if err == nil {
			return listener, altPort, nil
		}
	}

	// Last resort: OS-assigned port
	listener, err = net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, 0, fmt.Errorf("could not bind to any port: %w", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	return listener, actualPort, nil
}

// saveConfig saves the device token back to the YAML config file.
func saveConfig(path string, cfg *config.Config) error {
	return saveConfigField(path, "device_token:", fmt.Sprintf("  device_token: \"%s\"", cfg.Auth.DeviceToken))
}

// saveConfigField does a line-level find-and-replace in the config YAML.
// It finds the first line whose trimmed content starts with fieldPrefix
// and replaces the entire line with newLine.
func saveConfigField(path string, fieldPrefix, newLine string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), fieldPrefix) {
			lines[i] = newLine
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("field %q not found in config file", fieldPrefix)
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

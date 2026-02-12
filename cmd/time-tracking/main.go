package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
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

	"go.uber.org/zap"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config/local.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	log.Info("Starting time-tracking agent",
		zap.String("env", cfg.Env),
		zap.String("config_path", *configPath),
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

	if cfg.Device.ID == "" {
		log.Info("Generated device ID", zap.String("device_id", deviceID))
		// Note: In a production app, you'd want to save this back to config
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

		// Perform authorization
		code, err := deviceAuth.AuthorizeDevice(deviceID, cfg.Device.Name)
		if err != nil {
			log.Fatal("Device authorization failed", zap.Error(err))
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
		if err := saveConfig(*configPath, cfg); err != nil {
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

	// Initialize URL store and server (for browser extension)
	var urlStore *service.URLStore
	var urlServer *server.URLServer
	var urlHTTPServer *http.Server
	
	if cfg.Server.Enabled {
		urlStore = service.NewURLStore(cfg.Server.URLStoreTTL, log.Logger)
		urlServer = server.NewURLServer(urlStore, log.Logger)
		
		// Create HTTP server for URL updates
		addr := fmt.Sprintf("localhost:%d", cfg.Server.Port)
		urlHTTPServer = &http.Server{
			Addr:         addr,
			Handler:      urlServer,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		
		// Start URL server in goroutine
		go func() {
			log.Info("Starting URL server for browser extension",
				zap.String("address", addr),
			)
			if err := urlHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("URL server error", zap.Error(err))
			}
		}()
	} else {
		log.Info("URL server disabled in configuration")
	}

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

	// Initialize event collector
	eventCollector := collector.NewEventCollector(
		cfg.Tracking.BatchSize,
		time.Duration(cfg.Tracking.BatchFlushInterval)*time.Second,
		log.Logger,
	)

	// Initialize tracking service
	trackingService := service.NewTrackingService(
		platformInstance,
		windowTracker,
		activityTracker,
		eventCollector,
		apiClient,
		eventQueue,
		urlStore, // Can be nil if server disabled
		deviceID,
		log.Logger,
	)

	// Start tracking service
	if err := trackingService.Start(); err != nil {
		log.Fatal("Failed to start tracking service", zap.Error(err))
	}

	log.Info("Time-tracking agent started successfully",
		zap.String("device_id", deviceID),
		zap.String("backend_url", cfg.Backend.BaseURL),
	)

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	
	// Wait for signal
	sig := <-quit
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	log.Info("Shutting down time-tracking agent...")

	// Stop URL server if enabled
	if urlHTTPServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := urlHTTPServer.Shutdown(ctx); err != nil {
			log.Warn("URL server shutdown error", zap.Error(err))
		} else {
			log.Info("URL server stopped")
		}
	}

	// Stop URL store if enabled
	if urlStore != nil {
		urlStore.Stop()
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
		// Force exit immediately if graceful shutdown fails
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

// saveConfig saves the configuration back to the YAML file
// Note: This is a simple implementation. In production, you might want to use
// a proper YAML library or update cleanenv to support writing configs.
func saveConfig(path string, cfg *config.Config) error {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Simple string replacement for device_token
	content := string(data)
	
	// Find and replace device_token line
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "device_token:") {
			lines[i] = fmt.Sprintf("  device_token: \"%s\"", cfg.Auth.DeviceToken)
			found = true
			break
		}
	}
	
	if !found {
		// Find the auth section and add device_token
		for i, line := range lines {
			if strings.TrimSpace(line) == "auth:" {
				// Insert device_token after auth:
				lines = append(lines[:i+1], append([]string{fmt.Sprintf("  device_token: \"%s\"", cfg.Auth.DeviceToken)}, lines[i+1:]...)...)
				found = true
				break
			}
		}
	}

	if !found {
		return fmt.Errorf("could not find auth section in config file")
	}

	// Write back
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

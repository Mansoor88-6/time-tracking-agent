package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/config"
	"Mansoor88-6/time-tracking-agent/internal/database"
	"Mansoor88-6/time-tracking-agent/internal/handler"
	"Mansoor88-6/time-tracking-agent/internal/logger"
	"Mansoor88-6/time-tracking-agent/internal/repository"
	"Mansoor88-6/time-tracking-agent/internal/router"
	"Mansoor88-6/time-tracking-agent/internal/server"
	"Mansoor88-6/time-tracking-agent/internal/service"

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

	// Initialize repositories
	timeEntryRepo := repository.NewTimeEntryRepository(db.DB)

	// Initialize services
	timeEntryService := service.NewTimeEntryService(timeEntryRepo)

	// Initialize handlers
	timeEntryHandler := handler.NewTimeEntryHandler(timeEntryService, log.Logger)

	// Initialize router
	httpHandler := router.New(timeEntryHandler, log.Logger)

	// Initialize HTTP server
	httpServer := server.New(cfg.HttpServer.Address, httpHandler, log.Logger)

	// Start server in a goroutine
	go func() {
		if err := httpServer.Start(); err != nil {
			log.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	log.Info("Time-tracking agent started successfully",
		zap.String("address", cfg.HttpServer.Address),
	)

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down time-tracking agent...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown", zap.Error(err))
	}

	log.Info("Time-tracking agent stopped")
}

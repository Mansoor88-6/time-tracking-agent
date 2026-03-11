package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.Logger
}

// New creates a logger that writes to stderr (for development / console runs).
func New(level, format string) (*Logger, error) {
	zapLevel := parseLevel(level)

	var config zap.Config
	if format == "json" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
	}

	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{Logger: logger}, nil
}

// NewWithFile creates a logger that writes to both stderr and a log file.
// This is used in production when the agent runs as a GUI process (no console).
func NewWithFile(level, format, logDir string) (*Logger, error) {
	zapLevel := parseLevel(level)

	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// Fall back to console-only logger
		return New(level, format)
	}

	logFilePath := filepath.Join(logDir, "agent.log")

	// Open/create log file (append mode)
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// Fall back to console-only logger
		return New(level, format)
	}

	// Create encoder config
	encConfig := zap.NewProductionEncoderConfig()
	encConfig.TimeKey = "timestamp"
	encConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var encoder zapcore.Encoder
	if format == "json" {
		encoder = zapcore.NewJSONEncoder(encConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encConfig)
	}

	// Write to both file and stderr
	core := zapcore.NewTee(
		zapcore.NewCore(encoder, zapcore.AddSync(logFile), zapLevel),
		zapcore.NewCore(encoder, zapcore.AddSync(os.Stderr), zapLevel),
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &Logger{Logger: logger}, nil
}

func parseLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func (l *Logger) Sync() error {
	return l.Logger.Sync()
}

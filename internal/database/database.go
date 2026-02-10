package database

import (
	"database/sql"
	"fmt"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	logger *zap.Logger
}

func New(storagePath string, logger *zap.Logger) (*DB, error) {
	db, err := sql.Open("sqlite", storagePath+"?_foreign_keys=1&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	database := &DB{
		DB:     db,
		logger: logger,
	}

	if err := database.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	logger.Info("Database connection established", zap.String("path", storagePath))
	return database, nil
}

func (db *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		// Device info table
		`CREATE TABLE IF NOT EXISTS device_info (
			id INTEGER PRIMARY KEY,
			device_id TEXT UNIQUE NOT NULL,
			device_name TEXT,
			device_token TEXT,
			registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_sync_at TIMESTAMP,
			token_expires_at TIMESTAMP
		)`,
		// Pending events queue
		`CREATE TABLE IF NOT EXISTS pending_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_data TEXT NOT NULL,
			device_id TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			retry_count INTEGER DEFAULT 0,
			last_attempt TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pending_events_device ON pending_events(device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pending_events_created ON pending_events(created_at)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	db.logger.Info("Database migrations completed")
	return nil
}

func (db *DB) Close() error {
	if err := db.DB.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	db.logger.Info("Database connection closed")
	return nil
}

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
		`CREATE TABLE IF NOT EXISTS time_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			project_id TEXT,
			description TEXT,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP,
			duration_seconds INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_user_id ON time_entries(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_project_id ON time_entries(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_start_time ON time_entries(start_time)`,
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

package repository

import (
	"database/sql"
	"fmt"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/models"
)

type TimeEntryRepository struct {
	db *sql.DB
}

func NewTimeEntryRepository(db *sql.DB) *TimeEntryRepository {
	return &TimeEntryRepository{db: db}
}

func (r *TimeEntryRepository) Create(entry *models.CreateTimeEntryRequest) (*models.TimeEntry, error) {
	var durationSeconds *int64
	if entry.EndTime != nil {
		duration := int64(entry.EndTime.Sub(entry.StartTime).Seconds())
		durationSeconds = &duration
	}

	query := `
		INSERT INTO time_entries (user_id, project_id, description, start_time, end_time, duration_seconds)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at
	`

	var id int64
	var createdAt, updatedAt time.Time
	err := r.db.QueryRow(
		query,
		entry.UserID,
		entry.ProjectID,
		entry.Description,
		entry.StartTime,
		entry.EndTime,
		durationSeconds,
	).Scan(&id, &createdAt, &updatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create time entry: %w", err)
	}

	return &models.TimeEntry{
		ID:              id,
		UserID:          entry.UserID,
		ProjectID:       entry.ProjectID,
		Description:     entry.Description,
		StartTime:       entry.StartTime,
		EndTime:         entry.EndTime,
		DurationSeconds: durationSeconds,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

func (r *TimeEntryRepository) GetByID(id int64) (*models.TimeEntry, error) {
	query := `
		SELECT id, user_id, project_id, description, start_time, end_time, duration_seconds, created_at, updated_at
		FROM time_entries
		WHERE id = ?
	`

	var entry models.TimeEntry
	err := r.db.QueryRow(query, id).Scan(
		&entry.ID,
		&entry.UserID,
		&entry.ProjectID,
		&entry.Description,
		&entry.StartTime,
		&entry.EndTime,
		&entry.DurationSeconds,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("time entry not found: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get time entry: %w", err)
	}

	return &entry, nil
}

func (r *TimeEntryRepository) GetByUserID(userID string, limit, offset int) ([]*models.TimeEntry, error) {
	query := `
		SELECT id, user_id, project_id, description, start_time, end_time, duration_seconds, created_at, updated_at
		FROM time_entries
		WHERE user_id = ?
		ORDER BY start_time DESC
		LIMIT ? OFFSET ?
	`

	rows, err := r.db.Query(query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query time entries: %w", err)
	}
	defer rows.Close()

	var entries []*models.TimeEntry
	for rows.Next() {
		var entry models.TimeEntry
		err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.ProjectID,
			&entry.Description,
			&entry.StartTime,
			&entry.EndTime,
			&entry.DurationSeconds,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan time entry: %w", err)
		}
		entries = append(entries, &entry)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return entries, nil
}

func (r *TimeEntryRepository) Update(id int64, update *models.UpdateTimeEntryRequest) (*models.TimeEntry, error) {
	// Get current entry first
	current, err := r.GetByID(id)
	if err != nil {
		return nil, err
	}

	// Build dynamic update query
	setParts := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []interface{}{}

	startTime := current.StartTime
	endTime := current.EndTime

	if update.ProjectID != nil {
		setParts = append(setParts, "project_id = ?")
		args = append(args, *update.ProjectID)
	}
	if update.Description != nil {
		setParts = append(setParts, "description = ?")
		args = append(args, *update.Description)
	}
	if update.StartTime != nil {
		setParts = append(setParts, "start_time = ?")
		args = append(args, *update.StartTime)
		startTime = *update.StartTime
	}
	if update.EndTime != nil {
		setParts = append(setParts, "end_time = ?")
		args = append(args, *update.EndTime)
		endTime = update.EndTime
	}

	// Recalculate duration if start_time or end_time changed
	if update.StartTime != nil || update.EndTime != nil {
		if endTime != nil {
			duration := int64(endTime.Sub(startTime).Seconds())
			setParts = append(setParts, "duration_seconds = ?")
			args = append(args, duration)
		} else {
			setParts = append(setParts, "duration_seconds = NULL")
		}
	} else if update.DurationSeconds != nil {
		setParts = append(setParts, "duration_seconds = ?")
		args = append(args, *update.DurationSeconds)
	}

	if len(setParts) == 1 {
		// Only updated_at changed, return current entry
		return current, nil
	}

	// Build SET clause
	setClause := setParts[0]
	for i := 1; i < len(setParts); i++ {
		setClause += ", " + setParts[i]
	}

	query := fmt.Sprintf(`
		UPDATE time_entries
		SET %s
		WHERE id = ?
	`, setClause)

	args = append(args, id)

	result, err := r.db.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update time entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, fmt.Errorf("time entry not found")
	}

	// Return updated entry
	return r.GetByID(id)
}

func (r *TimeEntryRepository) Delete(id int64) error {
	result, err := r.db.Exec("DELETE FROM time_entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete time entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("time entry not found")
	}

	return nil
}

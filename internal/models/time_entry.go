package models

import "time"

type TimeEntry struct {
	ID              int64     `json:"id"`
	UserID          string    `json:"user_id"`
	ProjectID       *string   `json:"project_id,omitempty"`
	Description     *string   `json:"description,omitempty"`
	StartTime       time.Time `json:"start_time"`
	EndTime         *time.Time `json:"end_time,omitempty"`
	DurationSeconds *int64    `json:"duration_seconds,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateTimeEntryRequest struct {
	UserID      string     `json:"user_id" binding:"required"`
	ProjectID   *string    `json:"project_id,omitempty"`
	Description *string    `json:"description,omitempty"`
	StartTime   time.Time  `json:"start_time" binding:"required"`
	EndTime     *time.Time `json:"end_time,omitempty"`
}

type UpdateTimeEntryRequest struct {
	ProjectID       *string    `json:"project_id,omitempty"`
	Description     *string    `json:"description,omitempty"`
	StartTime       *time.Time `json:"start_time,omitempty"`
	EndTime         *time.Time `json:"end_time,omitempty"`
	DurationSeconds *int64     `json:"duration_seconds,omitempty"`
}

package service

import (
	"Mansoor88-6/time-tracking-agent/internal/models"
	"Mansoor88-6/time-tracking-agent/internal/repository"
)

type TimeEntryService struct {
	repo *repository.TimeEntryRepository
}

func NewTimeEntryService(repo *repository.TimeEntryRepository) *TimeEntryService {
	return &TimeEntryService{repo: repo}
}

func (s *TimeEntryService) CreateTimeEntry(req *models.CreateTimeEntryRequest) (*models.TimeEntry, error) {
	return s.repo.Create(req)
}

func (s *TimeEntryService) GetTimeEntry(id int64) (*models.TimeEntry, error) {
	return s.repo.GetByID(id)
}

func (s *TimeEntryService) GetTimeEntriesByUser(userID string, limit, offset int) ([]*models.TimeEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.GetByUserID(userID, limit, offset)
}

func (s *TimeEntryService) UpdateTimeEntry(id int64, req *models.UpdateTimeEntryRequest) (*models.TimeEntry, error) {
	return s.repo.Update(id, req)
}

func (s *TimeEntryService) DeleteTimeEntry(id int64) error {
	return s.repo.Delete(id)
}

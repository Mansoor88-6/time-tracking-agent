package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"Mansoor88-6/time-tracking-agent/internal/models"
	"Mansoor88-6/time-tracking-agent/internal/service"

	"go.uber.org/zap"
)

type TimeEntryHandler struct {
	service *service.TimeEntryService
	logger  *zap.Logger
}

func NewTimeEntryHandler(service *service.TimeEntryService, logger *zap.Logger) *TimeEntryHandler {
	return &TimeEntryHandler{
		service: service,
		logger:  logger,
	}
}

func (h *TimeEntryHandler) CreateTimeEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.CreateTimeEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	entry, err := h.service.CreateTimeEntry(&req)
	if err != nil {
		h.logger.Error("Failed to create time entry", zap.Error(err))
		http.Error(w, "Failed to create time entry", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(entry)
}

func (h *TimeEntryHandler) GetTimeEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid id parameter", http.StatusBadRequest)
		return
	}

	entry, err := h.service.GetTimeEntry(id)
	if err != nil {
		h.logger.Error("Failed to get time entry", zap.Error(err))
		http.Error(w, "Time entry not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

func (h *TimeEntryHandler) GetTimeEntriesByUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "Missing user_id parameter", http.StatusBadRequest)
		return
	}

	limit := 50
	offset := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	entries, err := h.service.GetTimeEntriesByUser(userID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get time entries", zap.Error(err))
		http.Error(w, "Failed to get time entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (h *TimeEntryHandler) UpdateTimeEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid id parameter", http.StatusBadRequest)
		return
	}

	var req models.UpdateTimeEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	entry, err := h.service.UpdateTimeEntry(id, &req)
	if err != nil {
		h.logger.Error("Failed to update time entry", zap.Error(err))
		http.Error(w, "Failed to update time entry", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

func (h *TimeEntryHandler) DeleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid id parameter", http.StatusBadRequest)
		return
	}

	if err := h.service.DeleteTimeEntry(id); err != nil {
		h.logger.Error("Failed to delete time entry", zap.Error(err))
		http.Error(w, "Failed to delete time entry", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

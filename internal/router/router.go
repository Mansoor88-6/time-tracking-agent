package router

import (
	"net/http"

	"Mansoor88-6/time-tracking-agent/internal/handler"

	"go.uber.org/zap"
)

func New(timeEntryHandler *handler.TimeEntryHandler, logger *zap.Logger) http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Time entry endpoints
	mux.HandleFunc("/api/v1/time-entries", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			timeEntryHandler.CreateTimeEntry(w, r)
		case http.MethodGet:
			// Check if it's a single entry or list
			if r.URL.Query().Get("id") != "" {
				timeEntryHandler.GetTimeEntry(w, r)
			} else {
				timeEntryHandler.GetTimeEntriesByUser(w, r)
			}
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/time-entries/update", timeEntryHandler.UpdateTimeEntry)
	mux.HandleFunc("/api/v1/time-entries/delete", timeEntryHandler.DeleteTimeEntry)

	// Logging middleware
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
		)
		mux.ServeHTTP(w, r)
	})
}

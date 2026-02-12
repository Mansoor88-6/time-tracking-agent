package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/service"

	"go.uber.org/zap"
)

// URLUpdateRequest represents the request body from the extension
type URLUpdateRequest struct {
	Application string `json:"application"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Timestamp   int64  `json:"timestamp"`
}

// URLServer handles HTTP requests from the browser extension
type URLServer struct {
	urlStore *service.URLStore
	logger   *zap.Logger
}

// NewURLServer creates a new URL server
func NewURLServer(urlStore *service.URLStore, logger *zap.Logger) *URLServer {
	return &URLServer{
		urlStore: urlStore,
		logger:   logger,
	}
}

// ServeHTTP implements http.Handler
func (s *URLServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for extension
	s.setCORSHeaders(w)

	// Handle preflight requests
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Route requests
	switch r.URL.Path {
	case "/api/v1/url-update":
		if r.Method == http.MethodPost {
			s.handleURLUpdate(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case "/api/v1/health":
		if r.Method == http.MethodGet {
			s.handleHealth(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.NotFound(w, r)
	}
}

// setCORSHeaders sets CORS headers for extension communication
func (s *URLServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*") // Extension origin
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "3600")
}

// handleURLUpdate processes URL updates from the extension
func (s *URLServer) handleURLUpdate(w http.ResponseWriter, r *http.Request) {
	var req URLUpdateRequest

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		s.logger.Warn("Failed to decode URL update request", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Application == "" || req.URL == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Validate that application is a browser (security check)
	if !s.isBrowserApplication(req.Application) {
		s.logger.Warn("Rejected URL update from non-browser application",
			zap.String("application", req.Application),
		)
		http.Error(w, "Invalid application", http.StatusBadRequest)
		return
	}

	// Validate URL format
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		s.logger.Warn("Rejected invalid URL format",
			zap.String("url", req.URL),
		)
		http.Error(w, "Invalid URL format", http.StatusBadRequest)
		return
	}

	// Store URL - URLStore will normalize the application name
	s.urlStore.StoreByApplicationAndTitle(req.Application, req.Title, req.URL)

	s.logger.Info("URL update received",
		zap.String("application", req.Application),
		zap.String("title", req.Title),
		zap.String("url", req.URL),
	)

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// handleHealth provides a health check endpoint
func (s *URLServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	})
}

// isBrowserApplication checks if the application is a known browser
func (s *URLServer) isBrowserApplication(application string) bool {
	appLower := strings.ToLower(application)
	browsers := []string{
		"chrome",
		"google chrome",
		"chromium",
		"firefox",
		"mozilla firefox",
		"edge",
		"microsoft edge",
		"safari",
		"opera",
		"brave",
		"vivaldi",
	}

	for _, browser := range browsers {
		if strings.Contains(appLower, browser) {
			return true
		}
	}

	return false
}

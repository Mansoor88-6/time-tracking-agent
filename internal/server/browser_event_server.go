package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/models"
	"Mansoor88-6/time-tracking-agent/internal/service"

	"go.uber.org/zap"
)

// BrowserEventServer handles HTTP requests from the browser extension
type BrowserEventServer struct {
	sessionManager *service.SessionManager
	logger         *zap.Logger
}

// NewBrowserEventServer creates a new browser event server
func NewBrowserEventServer(sessionManager *service.SessionManager, logger *zap.Logger) *BrowserEventServer {
	return &BrowserEventServer{
		sessionManager: sessionManager,
		logger:         logger,
	}
}

// ServeHTTP implements http.Handler
func (s *BrowserEventServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for extension
	s.setCORSHeaders(w)

	// Handle preflight requests
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Route requests
	switch r.URL.Path {
	case "/api/v1/browser-event":
		if r.Method == http.MethodPost {
			s.handleBrowserEvent(w, r)
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
func (s *BrowserEventServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*") // Extension origin
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "3600")
}

// handleBrowserEvent processes browser events from the extension
func (s *BrowserEventServer) handleBrowserEvent(w http.ResponseWriter, r *http.Request) {
	var event models.BrowserEvent

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&event); err != nil {
		s.logger.Warn("Failed to decode browser event request", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if event.Source != "browser" {
		http.Error(w, "Invalid source, must be 'browser'", http.StatusBadRequest)
		return
	}

	if event.Browser == "" {
		http.Error(w, "Missing browser field", http.StatusBadRequest)
		return
	}

	if event.URL == "" {
		http.Error(w, "Missing URL field", http.StatusBadRequest)
		return
	}

	// Validate URL format
	if !strings.HasPrefix(event.URL, "http://") && !strings.HasPrefix(event.URL, "https://") {
		s.logger.Warn("Rejected invalid URL format",
			zap.String("url", event.URL),
		)
		http.Error(w, "Invalid URL format", http.StatusBadRequest)
		return
	}

	// Validate sequence number
	if event.Sequence < 0 {
		http.Error(w, "Invalid sequence number", http.StatusBadRequest)
		return
	}

	// Validate that browser is a known type (security check)
	if !s.isValidBrowser(event.Browser) {
		s.logger.Warn("Rejected browser event from unknown browser",
			zap.String("browser", event.Browser),
		)
		http.Error(w, "Invalid browser type", http.StatusBadRequest)
		return
	}

	s.logger.Info("Browser event received",
		zap.String("browser", event.Browser),
		zap.String("url", event.URL),
		zap.String("title", event.Title),
		zap.Int("tabId", event.TabID),
		zap.Int("windowId", event.WindowID),
		zap.Int("sequence", event.Sequence),
	)

	// Process event through session manager
	s.sessionManager.ProcessBrowserEvent(&event)

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// handleHealth provides a health check endpoint
func (s *BrowserEventServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	})
}

// isValidBrowser checks if the browser is a known browser type
func (s *BrowserEventServer) isValidBrowser(browser string) bool {
	browserLower := strings.ToLower(browser)
	validBrowsers := []string{
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

	for _, validBrowser := range validBrowsers {
		if strings.Contains(browserLower, validBrowser) {
			return true
		}
	}

	return false
}

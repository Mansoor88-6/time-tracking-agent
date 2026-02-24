package service

import (
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// URLInfo stores URL information with timestamp
type URLInfo struct {
	URL       string
	Timestamp time.Time
}

// URLStore provides thread-safe storage for browser URLs
// Maps window title/application to current URL
type URLStore struct {
	mu        sync.RWMutex
	urls      map[string]*URLInfo
	ttl       time.Duration
	logger    *zap.Logger
	stopChan  chan struct{}
	cleanupWg sync.WaitGroup
}

// NewURLStore creates a new URL store with TTL-based expiration
func NewURLStore(ttlSeconds int, logger *zap.Logger) *URLStore {
	store := &URLStore{
		urls:     make(map[string]*URLInfo),
		ttl:      time.Duration(ttlSeconds) * time.Second,
		logger:   logger,
		stopChan: make(chan struct{}),
	}

	// Start cleanup goroutine
	store.cleanupWg.Add(1)
	go store.cleanupLoop()

	return store
}

// Store stores or updates a URL for a given key (application:title)
func (s *URLStore) Store(key string, url string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.urls[key] = &URLInfo{
		URL:       url,
		Timestamp: time.Now(),
	}

	s.logger.Debug("Stored URL",
		zap.String("key", key),
		zap.String("url", url),
	)
}

// StoreByApplicationAndTitle stores URL using application and title (normalizes application name)
// Stores with multiple key variations to improve matching reliability
func (s *URLStore) StoreByApplicationAndTitle(application, title, url string) {
	normalizedApp := s.normalizeApplicationName(application)
	
	// Store with original title
	key1 := normalizedApp + ":" + title
	s.Store(key1, url)
	
	// Store with normalized title (without browser suffix)
	normalizedTitle := s.normalizeTitle(title)
	if normalizedTitle != title {
		key2 := normalizedApp + ":" + normalizedTitle
		s.Store(key2, url)
	}
	
	// Store with title variations (with and without browser suffix)
	// This handles cases where extension sends "Page Title" but window tracker sees "Page Title - Google Chrome"
	browserSuffixes := []string{
		" - Google Chrome", " - Chrome",
		" - Microsoft Edge", " - Edge",
		" - Mozilla Firefox", " - Firefox",
		" - Safari", " - Opera", " - Brave", " - Vivaldi",
	}
	
	// If title doesn't have browser suffix, also store with common suffixes
	if normalizedTitle == title {
		for _, suffix := range browserSuffixes {
			keyWithSuffix := normalizedApp + ":" + title + suffix
			s.Store(keyWithSuffix, url)
		}
	}
	
	s.logger.Debug("Stored URL with multiple key variations",
		zap.String("original_application", application),
		zap.String("normalized_app", normalizedApp),
		zap.String("original_title", title),
		zap.String("normalized_title", normalizedTitle),
		zap.String("url", url),
	)
}

// Get retrieves a URL for a given key if it exists and hasn't expired
func (s *URLStore) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.urls[key]
	if !exists {
		return "", false
	}

	// Check if expired
	if time.Since(info.Timestamp) > s.ttl {
		// Mark for deletion (will be cleaned up by cleanup loop)
		return "", false
	}

	return info.URL, true
}

// GetByApplicationAndTitle retrieves URL using application and title
// Tries exact match first, then fuzzy match (removes browser suffixes)
func (s *URLStore) GetByApplicationAndTitle(application, title string) (string, bool) {
	normalizedApp := s.normalizeApplicationName(application)
	
	// Try exact match first
	exactKey := normalizedApp + ":" + title
	if url, found := s.Get(exactKey); found {
		s.logger.Debug("URL lookup successful (exact match)",
			zap.String("key", exactKey),
			zap.String("url", url),
		)
		return url, true
	}
	
	// Try normalized title match
	normalizedTitle := s.normalizeTitle(title)
	if normalizedTitle != title {
		normalizedKey := normalizedApp + ":" + normalizedTitle
		if url, found := s.Get(normalizedKey); found {
			s.logger.Debug("URL lookup successful (normalized title match)",
				zap.String("key", normalizedKey),
				zap.String("url", url),
			)
			return url, true
		}
	}
	
	// Try fuzzy match - search all stored keys for a match
	// Extension sends: "Page Title"
	// Window tracker sees: "Page Title - Google Chrome"
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	// First, try to find the most recent matching URL (by timestamp)
	var bestMatch *URLInfo
	var bestMatchKey string
	bestMatchTime := time.Time{}
	
	for key, info := range s.urls {
		// Check if expired
		if time.Since(info.Timestamp) > s.ttl {
			continue
		}
		
		// Check if key starts with normalizedApp:
		if !strings.HasPrefix(key, normalizedApp+":") {
			continue
		}
		
		storedTitle := strings.TrimPrefix(key, normalizedApp+":")
		normalizedStoredTitle := s.normalizeTitle(storedTitle)
		
		// Match if normalized titles are similar
		// Use more lenient matching: check if either contains the other
		// or if they share significant common parts
		titleMatch := false
		if normalizedStoredTitle == normalizedTitle {
			titleMatch = true
		} else if strings.Contains(normalizedStoredTitle, normalizedTitle) || 
		          strings.Contains(normalizedTitle, normalizedStoredTitle) {
			// Check if the overlap is significant (at least 50% of shorter string)
			shorter := normalizedTitle
			longer := normalizedStoredTitle
			if len(normalizedStoredTitle) < len(normalizedTitle) {
				shorter = normalizedStoredTitle
				longer = normalizedTitle
			}
			if len(shorter) > 0 && len(longer) >= len(shorter)/2 {
				titleMatch = true
			}
		}
		
		if titleMatch {
			// Prefer more recent matches
			if info.Timestamp.After(bestMatchTime) {
				bestMatch = info
				bestMatchKey = key
				bestMatchTime = info.Timestamp
			}
		}
	}
	
	if bestMatch != nil {
		s.logger.Debug("URL lookup successful (fuzzy match)",
			zap.String("original_title", title),
			zap.String("matched_key", bestMatchKey),
			zap.String("url", bestMatch.URL),
		)
		return bestMatch.URL, true
	}
	
	// Last resort: if we have a browser application and title is similar to any stored title
	// This handles cases where title changes slightly but it's the same tab
	// Only use this if we have very few stored URLs (likely single tab scenario)
	if normalizedApp == "chrome" || normalizedApp == "firefox" || normalizedApp == "edge" {
		appURLCount := 0
		var recentURL *URLInfo
		recentTime := time.Time{}
		
		for key, info := range s.urls {
			if time.Since(info.Timestamp) > s.ttl {
				continue
			}
			if strings.HasPrefix(key, normalizedApp+":") {
				appURLCount++
				// Only use if URL was updated very recently (within last 2 seconds)
				// and it's the most recent one
				if time.Since(info.Timestamp) < 2*time.Second && info.Timestamp.After(recentTime) {
					recentURL = info
					recentTime = info.Timestamp
				}
			}
		}
		
		// Only use fallback if we have very few URLs (likely single tab) and very recent match
		if recentURL != nil && appURLCount <= 3 {
			s.logger.Debug("URL lookup successful (fallback to recent app URL)",
				zap.String("application", application),
				zap.String("url", recentURL.URL),
				zap.Int("app_url_count", appURLCount),
			)
			return recentURL.URL, true
		}
	}
	
	s.logger.Debug("URL lookup failed",
		zap.String("original_application", application),
		zap.String("normalized_app", normalizedApp),
		zap.String("original_title", title),
		zap.String("normalized_title", normalizedTitle),
	)
	
	return "", false
}

// normalizeTitle removes browser suffixes and normalizes for matching
func (s *URLStore) normalizeTitle(title string) string {
	title = strings.TrimSpace(title)
	
	// Remove common browser suffixes
	browserSuffixes := []string{
		" - Google Chrome",
		" - Chrome",
		" - Microsoft Edge",
		" - Edge",
		" - Mozilla Firefox",
		" - Firefox",
		" - Safari",
		" - Opera",
		" - Brave",
		" - Vivaldi",
	}
	
	for _, suffix := range browserSuffixes {
		if strings.HasSuffix(title, suffix) {
			title = strings.TrimSuffix(title, suffix)
			title = strings.TrimSpace(title)
		}
	}
	
	return title
}

// makeKey creates a key from application and title
// Normalizes application name to handle variations like "Google Chrome" vs "chrome"
func (s *URLStore) makeKey(application, title string) string {
	normalizedApp := s.normalizeApplicationName(application)
	return normalizedApp + ":" + title
}

// normalizeApplicationName normalizes browser application names to handle variations
func (s *URLStore) normalizeApplicationName(application string) string {
	appLower := strings.ToLower(application)
	
	// Map common browser name variations to a standard form
	browserMap := map[string]string{
		"chrome":        "chrome",
		"google chrome": "chrome",
		"chromium":      "chrome",
		"firefox":       "firefox",
		"mozilla firefox": "firefox",
		"edge":          "edge",
		"microsoft edge": "edge",
		"safari":        "safari",
		"opera":         "opera",
		"brave":         "brave",
		"vivaldi":       "vivaldi",
	}
	
	// Check if it's a known browser
	for key, normalized := range browserMap {
		if strings.Contains(appLower, key) {
			return normalized
		}
	}
	
	// Return lowercase if not a known browser
	return appLower
}

// cleanupLoop periodically removes expired entries
func (s *URLStore) cleanupLoop() {
	defer s.cleanupWg.Done()

	ticker := time.NewTicker(10 * time.Second) // Cleanup every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.stopChan:
			return
		}
	}
}

// cleanup removes expired entries
func (s *URLStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	expiredCount := 0

	for key, info := range s.urls {
		if now.Sub(info.Timestamp) > s.ttl {
			delete(s.urls, key)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		s.logger.Debug("Cleaned up expired URLs",
			zap.Int("count", expiredCount),
		)
	}
}

// Stop stops the cleanup goroutine
func (s *URLStore) Stop() {
	close(s.stopChan)
	s.cleanupWg.Wait()
	s.logger.Info("URL store stopped")
}

// Clear removes all entries (useful for testing)
func (s *URLStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.urls = make(map[string]*URLInfo)
}

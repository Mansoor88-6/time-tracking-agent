package service

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/client"
	"Mansoor88-6/time-tracking-agent/internal/collector"
	"Mansoor88-6/time-tracking-agent/internal/models"
	"Mansoor88-6/time-tracking-agent/internal/platform"
	"Mansoor88-6/time-tracking-agent/internal/queue"
	"Mansoor88-6/time-tracking-agent/internal/tracker"

	"go.uber.org/zap"
)

// TrackingService orchestrates all tracking components
type TrackingService struct {
	platform        platform.Platform
	windowTracker   *tracker.WindowTracker
	activityTracker *tracker.ActivityTracker
	eventCollector  *collector.EventCollector
	apiClient       *client.APIClient
	eventQueue      *queue.EventQueue
	urlStore        *URLStore // Optional: for extension-provided URLs
	deviceID        string
	logger          *zap.Logger
	
	currentWindow   *platform.WindowInfo
	currentState     tracker.ActivityState
	lastEventTime    time.Time
	stopped          bool
	mu               sync.RWMutex
	
	stopChan         chan struct{}
	wg               sync.WaitGroup
}

// NewTrackingService creates a new tracking service
func NewTrackingService(
	platform platform.Platform,
	windowTracker *tracker.WindowTracker,
	activityTracker *tracker.ActivityTracker,
	eventCollector *collector.EventCollector,
	apiClient *client.APIClient,
	eventQueue *queue.EventQueue,
	urlStore *URLStore, // Optional: can be nil if extension not available
	deviceID string,
	logger *zap.Logger,
) *TrackingService {
	return &TrackingService{
		platform:       platform,
		windowTracker:  windowTracker,
		activityTracker: activityTracker,
		eventCollector: eventCollector,
		apiClient:     apiClient,
		eventQueue:    eventQueue,
		urlStore:      urlStore,
		deviceID:      deviceID,
		logger:        logger,
		stopChan:      make(chan struct{}),
		currentState:  tracker.StateActive,
	}
}

// Start begins tracking
func (ts *TrackingService) Start() error {
	ts.logger.Info("Starting tracking service", zap.String("device_id", ts.deviceID))

	// Start window tracker
	if err := ts.windowTracker.Start(ts.onWindowChange); err != nil {
		return err
	}

	// Start activity tracker
	if err := ts.activityTracker.Start(ts.onActivityStateChange); err != nil {
		ts.windowTracker.Stop()
		return err
	}

	// Start event collector
	ts.eventCollector.Start(ts.onBatchReady)

	// Start queue processor
	ts.wg.Add(1)
	go ts.queueProcessor()

	ts.logger.Info("Tracking service started")
	return nil
}

// Stop stops tracking
func (ts *TrackingService) Stop() {
	ts.logger.Info("Stopping tracking service")

	ts.mu.Lock()
	select {
	case <-ts.stopChan:
		// Already stopped
		ts.mu.Unlock()
		return
	default:
		ts.stopped = true // Set stopped flag immediately
		close(ts.stopChan)
	}
	ts.mu.Unlock()
	
	// Stop activity tracker FIRST (removes Windows hooks immediately)
	ts.activityTracker.Stop()
	
	// Stop window tracker
	ts.windowTracker.Stop()
	
	// Stop event collector (stops creating new events)
	ts.eventCollector.Stop()
	
	// Wait for all goroutines with timeout
	done := make(chan struct{})
	go func() {
		ts.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// All goroutines stopped
	case <-time.After(2 * time.Second):
		ts.logger.Warn("Some goroutines did not stop within timeout")
	}

	// Flush any remaining events (but don't wait for send)
	ts.eventCollector.Flush()

	ts.logger.Info("Tracking service stopped")
}

// onWindowChange handles window change events
func (ts *TrackingService) onWindowChange(window *platform.WindowInfo) {
	ts.mu.Lock()
	ts.currentWindow = window
	ts.mu.Unlock()

	// Window changes indicate user activity - reset activity timer
	// This ensures tab/app switches are treated as activity
	ts.activityTracker.RecordActivity()

	ts.createEvent(window, nil)
}

// onActivityStateChange handles activity state changes
func (ts *TrackingService) onActivityStateChange(state tracker.ActivityState) {
	ts.mu.Lock()
	oldState := ts.currentState
	ts.currentState = state
	ts.mu.Unlock()

	if oldState != state {
		ts.createEvent(nil, &state)
	}
}

// createEvent creates a tracking event
func (ts *TrackingService) createEvent(window *platform.WindowInfo, state *tracker.ActivityState) {
	ts.mu.RLock()
	stopped := ts.stopped
	currentWindow := ts.currentWindow
	currentState := ts.currentState
	ts.mu.RUnlock()
	
	// Don't create events if we're shutting down
	if stopped {
		return
	}

	// Use provided state or current state
	eventState := string(currentState)
	if state != nil {
		eventState = string(*state)
	}

	// Use provided window or current window
	eventWindow := currentWindow
	if window != nil {
		eventWindow = window
	}

	now := time.Now()
	timestamp := now.UnixMilli()

	// Calculate duration since last event
	var duration *int64
	if !ts.lastEventTime.IsZero() {
		dur := now.Sub(ts.lastEventTime).Milliseconds()
		if dur > 0 {
			duration = &dur
		}
	}

	event := models.TrackingEvent{
		DeviceID:  ts.deviceID,
		Timestamp: timestamp,
		Status:    eventState,
		Duration:  duration,
	}

	// Add window information if available
	if eventWindow != nil {
		if eventWindow.Application != "" {
			event.Application = &eventWindow.Application
		}
		if eventWindow.Title != "" {
			event.Title = &eventWindow.Title
		}

		// Priority: Extension URL > Title-extracted URL > No URL
		// Check URL store first (extension-provided URLs)
		if eventWindow.Application != "" && ts.urlStore != nil {
			if extensionURL, found := ts.urlStore.GetByApplicationAndTitle(eventWindow.Application, eventWindow.Title); found {
				event.URL = &extensionURL
				ts.logger.Info("Using extension-provided URL",
					zap.String("url", extensionURL),
					zap.String("application", eventWindow.Application),
					zap.String("title", eventWindow.Title),
				)
			} else {
				// Log when extension URL not found for debugging
				ts.logger.Debug("Extension URL not found, trying title extraction",
					zap.String("application", eventWindow.Application),
					zap.String("title", eventWindow.Title),
				)
				// Fallback to title extraction
				extractedURL := ts.extractDomainFromTitle(eventWindow.Title, eventWindow.Application)
				if extractedURL != nil {
					event.URL = extractedURL
					ts.logger.Debug("Using title-extracted URL",
						zap.String("url", *extractedURL),
					)
				}
			}
		} else if eventWindow.Application != "" {
			// No URL store available, use title extraction
			extractedURL := ts.extractDomainFromTitle(eventWindow.Title, eventWindow.Application)
			if extractedURL != nil {
				event.URL = extractedURL
			}
		}
	}

	ts.eventCollector.AddEvent(event)
	ts.lastEventTime = now
}

// onBatchReady handles when a batch is ready to be sent
func (ts *TrackingService) onBatchReady(events []models.TrackingEvent) {
	if len(events) == 0 {
		return
	}

	ts.logger.Debug("Batch ready to send",
		zap.Int("event_count", len(events)),
	)

	// Try to send to backend
	err := ts.apiClient.SendBatch(ts.deviceID, events)
	if err != nil {
		ts.logger.Warn("Failed to send batch, queuing locally",
			zap.Error(err),
			zap.Int("event_count", len(events)),
		)

		// Queue events locally for retry
		if queueErr := ts.eventQueue.Enqueue(ts.deviceID, events); queueErr != nil {
			ts.logger.Error("Failed to queue events",
				zap.Error(queueErr),
			)
		}
	}
}

// queueProcessor processes queued events in the background
func (ts *TrackingService) queueProcessor() {
	defer ts.wg.Done()

	ticker := time.NewTicker(60 * time.Second) // Check queue every minute
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ts.processQueue()
		case <-ts.stopChan:
			// Process queue one more time before stopping
			ts.processQueue()
			return
		}
	}
}

// processQueue attempts to send queued events
func (ts *TrackingService) processQueue() {
	// Get pending count
	pendingCount, err := ts.eventQueue.GetPendingCount(ts.deviceID)
	if err != nil {
		ts.logger.Error("Failed to get pending count", zap.Error(err))
		return
	}

	if pendingCount == 0 {
		return
	}

	ts.logger.Debug("Processing queued events",
		zap.Int("pending_count", pendingCount),
	)

	// Dequeue a batch
	events, ids, err := ts.eventQueue.Dequeue(ts.deviceID, 100)
	if err != nil {
		ts.logger.Error("Failed to dequeue events", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	// Try to send
	err = ts.apiClient.SendBatch(ts.deviceID, events)
	if err != nil {
		ts.logger.Warn("Failed to send queued batch",
			zap.Error(err),
			zap.Int("event_count", len(events)),
		)

		// Increment retry count
		if retryErr := ts.eventQueue.IncrementRetry(ids); retryErr != nil {
			ts.logger.Error("Failed to increment retry count", zap.Error(retryErr))
		}

		// Check if we should give up (too many retries)
		// This is handled by the cleanup function
		return
	}

	// Successfully sent, remove from queue
	if err := ts.eventQueue.Remove(ids); err != nil {
		ts.logger.Error("Failed to remove sent events from queue", zap.Error(err))
	} else {
		ts.logger.Info("Successfully sent queued events",
			zap.Int("event_count", len(events)),
		)
	}
}

// GetStatus returns the current tracking status
func (ts *TrackingService) GetStatus() map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	pendingCount, _ := ts.eventQueue.GetPendingCount(ts.deviceID)

	return map[string]interface{}{
		"device_id":      ts.deviceID,
		"current_state":  string(ts.currentState),
		"pending_events": pendingCount,
		"collector_pending": ts.eventCollector.GetPendingCount(),
	}
}

// extractDomainFromTitle extracts the domain from browser window titles
// Returns the domain as a URL (e.g., "https://youtube.com") or nil if not found
func (ts *TrackingService) extractDomainFromTitle(title, application string) *string {
	if title == "" || application == "" {
		return nil
	}

	// Normalize application name to lowercase for comparison
	appLower := strings.ToLower(application)

	// Common browser names to detect
	browsers := []string{
		"chrome", "google chrome", "chromium",
		"firefox", "mozilla firefox",
		"edge", "microsoft edge",
		"safari",
		"opera",
		"brave",
		"vivaldi",
		"tor browser",
	}

	// Check if application is a browser
	isBrowser := false
	for _, browser := range browsers {
		if strings.Contains(appLower, browser) {
			isBrowser = true
			break
		}
	}

	if !isBrowser {
		return nil
	}

	// Try to extract domain from title
	domain := ts.extractDomainFromTitleText(title)
	if domain == "" {
		return nil
	}

	// Return as URL
	url := "https://" + domain
	return &url
}

// extractDomainFromTitleText extracts domain from window title text
// Handles various title formats like:
// - "YouTube - Watch Videos" → "youtube.com"
// - "YouTube - Google Chrome" → "youtube.com" (first part, ignore browser name)
// - "Google - YouTube" → "youtube.com" (destination site)
// - "GitHub - Microsoft/vscode" → "github.com"
// - "Stack Overflow - Where Developers Learn" → "stackoverflow.com"
func (ts *TrackingService) extractDomainFromTitleText(title string) string {
	if title == "" {
		return ""
	}

	titleLower := strings.ToLower(title)

	// Browser names and search terms to exclude from matching
	// (to avoid matching "google" in "Google Chrome" or "Google Search")
	browserNames := []string{
		"google chrome", "chrome", "chromium",
		"mozilla firefox", "firefox",
		"microsoft edge", "edge",
		"safari", "opera", "brave", "vivaldi", "tor browser",
		"google search", "search", // Exclude search terms
	}

	// First, try to find full URL pattern in title
	urlRegex := regexp.MustCompile(`https?://([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`)
	if matches := urlRegex.FindStringSubmatch(title); len(matches) > 1 {
		domain := strings.ToLower(matches[1])
		// Remove www. prefix
		domain = strings.TrimPrefix(domain, "www.")
		return domain
	}

	// Try to find domain pattern directly (domain.tld)
	domainRegex := regexp.MustCompile(`([a-zA-Z0-9.-]+\.(com|org|net|io|co|edu|gov|uk|de|fr|jp|au|ca|in|br|ru|cn|es|it|nl|se|no|dk|fi|pl|cz|at|ch|be|ie|pt|gr|tr|za|mx|ar|cl|pe|ve|ec|uy|py|bo|cr|pa|do|gt|hn|ni|sv|bz|jm|tt|bb|gd|lc|vc|ag|dm|kn|ai|vg|ky|ms|tc|fk|gi|mt|cy|is|li|mc|ad|sm|va|lu|mo|hk|sg|my|th|ph|id|vn|kh|la|mm|bn|pk|bd|lk|np|af|ir|iq|sa|ae|kw|bh|qa|om|ye|jo|lb|sy|il|ps|eg|ly|tn|dz|ma|mr|sn|ml|bf|ne|td|sd|er|et|dj|so|ke|ug|rw|bi|tz|zm|mw|mz|ao|na|bw|sz|ls|mg|mu|sc|km|yt|re|io|sh|ac|gs|tf|aq|bv|hm|sj|um|as|gu|mp|pr|vi|fm|mh|pw|ck|nu|pn|tk|to|tv|vu|ws|nf|nr|ki|sb|pg|fj|nc|pf|wf|eh|ax|gg|je|im|fo|gl|pm|bl|mf|so|dev))`)
	if matches := domainRegex.FindStringSubmatch(titleLower); len(matches) > 1 {
		domain := strings.ToLower(matches[1])
		// Remove www. prefix
		domain = strings.TrimPrefix(domain, "www.")
		return domain
	}

	// Pattern matching for common sites
	// Note: "google" is intentionally excluded from general matching
	// to avoid false matches in "Google Chrome" or "Google Search"
	domainMap := map[string]string{
		"youtube":          "youtube.com",
		"github":           "github.com",
		"stack overflow":   "stackoverflow.com",
		"facebook":          "facebook.com",
		"twitter":          "twitter.com",
		"x.com":            "x.com",
		"linkedin":         "linkedin.com",
		"reddit":           "reddit.com",
		"instagram":        "instagram.com",
		"discord":          "discord.com",
		"slack":            "slack.com",
		"gmail":            "gmail.com",
		"outlook":          "outlook.com",
		"notion":           "notion.so",
		"figma":            "figma.com",
		"trello":           "trello.com",
		"asana":            "asana.com",
		"jira":             "jira.com",
		"confluence":      "confluence.com",
		"medium":           "medium.com",
		"dev":              "dev.to",
		"stack exchange":   "stackexchange.com",
		"wikipedia":        "wikipedia.org",
		"amazon":           "amazon.com",
		"netflix":          "netflix.com",
		"spotify":          "spotify.com",
		"zoom":             "zoom.us",
		"microsoft teams":  "teams.microsoft.com",
		"google meet":      "meet.google.com",
	}

	// Helper to check if a string contains a browser name or search term
	isBrowserOrSearchTerm := func(text string) bool {
		for _, browser := range browserNames {
			if strings.Contains(text, browser) {
				return true
			}
		}
		return false
	}

	// Helper to safely match "google" only when it's clearly a site (not browser/search)
	// Only match "google" if it appears alone or with site indicators
	matchGoogleSite := func(text string) string {
		// Only match "google" if it's not part of "google chrome", "google search", etc.
		textLower := strings.ToLower(text)
		if strings.Contains(textLower, "google chrome") ||
			strings.Contains(textLower, "google search") ||
			strings.Contains(textLower, "chromium") {
			return ""
		}
		// Match "google" only if it appears as a standalone word or with site context
		if regexp.MustCompile(`\bgoogle\b`).MatchString(textLower) {
			return "google.com"
		}
		return ""
	}

	// Split title by " - " to handle patterns like "Site - Browser" or "Site - Description"
	parts := strings.Split(titleLower, " - ")
	
	// Priority 1: Check first part (site name) if it exists
	if len(parts) > 0 && parts[0] != "" {
		firstPart := strings.TrimSpace(parts[0])
		// Remove leading numbers/parentheses like "(2) YouTube" → "youtube"
		firstPart = regexp.MustCompile(`^[\(\d\)\s]+`).ReplaceAllString(firstPart, "")
		firstPart = strings.TrimSpace(firstPart)
		
		// Check for "google" site (with special handling)
		if googleDomain := matchGoogleSite(firstPart); googleDomain != "" {
			return googleDomain
		}
		
		// Check known sites
		for key, domain := range domainMap {
			if strings.Contains(firstPart, key) {
				return domain
			}
		}
	}

	// Priority 2: Check second part only if it's NOT a browser/search term
	// This handles cases like "Google - YouTube" where second part is the destination
	if len(parts) > 1 && parts[1] != "" {
		secondPart := strings.TrimSpace(parts[1])
		// Skip if this part contains a browser name or search term
		if !isBrowserOrSearchTerm(secondPart) {
			// Check for "google" site (with special handling)
			if googleDomain := matchGoogleSite(secondPart); googleDomain != "" {
				return googleDomain
			}
			
			// Check known sites
			for key, domain := range domainMap {
				if strings.Contains(secondPart, key) {
					return domain
				}
			}
		}
	}

	// Priority 3: Check entire title, but exclude browser names and search terms
	// Create a cleaned version without browser names for matching
	cleanedTitle := titleLower
	for _, browser := range browserNames {
		cleanedTitle = strings.ReplaceAll(cleanedTitle, browser, "")
	}
	
	// Check for "google" site in cleaned title (with special handling)
	if googleDomain := matchGoogleSite(cleanedTitle); googleDomain != "" {
		return googleDomain
	}
	
	// Check known sites in cleaned title
	for key, domain := range domainMap {
		// Only match if the key appears in the cleaned title
		if strings.Contains(cleanedTitle, key) {
			return domain
		}
	}

	// If no match found, return empty string (don't guess)
	// This is better than returning a wrong domain
	return ""
}

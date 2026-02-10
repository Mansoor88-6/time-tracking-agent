package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/models"

	"go.uber.org/zap"
)

// APIClient handles communication with the backend API
type APIClient struct {
	baseURL     string
	apiKey      string
	deviceToken string // JWT token for device authentication
	timeout     time.Duration
	httpClient  *http.Client
	logger      *zap.Logger
}

// NewAPIClient creates a new API client
func NewAPIClient(baseURL, apiKey string, timeout time.Duration, logger *zap.Logger) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		timeout: timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

// SetDeviceToken sets the device JWT token
func (c *APIClient) SetDeviceToken(token string) {
	c.deviceToken = token
}

// SendBatch sends a batch of events to the backend
func (c *APIClient) SendBatch(deviceID string, events []models.TrackingEvent) error {
	if len(events) == 0 {
		return fmt.Errorf("cannot send empty batch")
	}

	batch := models.BatchEventRequest{
		Events:        events,
		DeviceID:      deviceID,
		BatchTimestamp: time.Now().UnixMilli(),
	}

	jsonData, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/events/batch", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Prefer device token over API key
	if c.deviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.deviceToken)
	} else if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		c.logger.Error("Failed to send batch",
			zap.Error(err),
			zap.Int("event_count", len(events)),
			zap.Duration("duration", duration),
		)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.logger.Info("Batch sent successfully",
			zap.Int("event_count", len(events)),
			zap.Int("status_code", resp.StatusCode),
			zap.Duration("duration", duration),
		)
		return nil
	}

	// Handle different error status codes
	errMsg := fmt.Sprintf("backend returned status %d: %s", resp.StatusCode, string(body))
	
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		c.logger.Error("Authentication failed",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(body)),
		)
		return &AuthError{Message: errMsg, StatusCode: resp.StatusCode}
	case http.StatusTooManyRequests:
		c.logger.Warn("Rate limited",
			zap.Int("status_code", resp.StatusCode),
		)
		return &RateLimitError{Message: errMsg, StatusCode: resp.StatusCode}
	case http.StatusBadRequest:
		c.logger.Error("Invalid request",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(body)),
		)
		return &BadRequestError{Message: errMsg, StatusCode: resp.StatusCode}
	default:
		c.logger.Error("Backend error",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(body)),
		)
		return &BackendError{Message: errMsg, StatusCode: resp.StatusCode}
	}
}

// HealthCheck checks if the backend is reachable
func (c *APIClient) HealthCheck() error {
	url := fmt.Sprintf("%s/health", c.baseURL)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}

// ExchangeAuthorizationCode exchanges an authorization code for a device token
func (c *APIClient) ExchangeAuthorizationCode(code, deviceID string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/auth/device/token", c.baseURL)

	reqBody := map[string]string{
		"code":     code,
		"deviceId": deviceID,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// Error types
type AuthError struct {
	Message    string
	StatusCode int
}

func (e *AuthError) Error() string {
	return e.Message
}

type RateLimitError struct {
	Message    string
	StatusCode int
}

func (e *RateLimitError) Error() string {
	return e.Message
}

type BadRequestError struct {
	Message    string
	StatusCode int
}

func (e *BadRequestError) Error() string {
	return e.Message
}

type BackendError struct {
	Message    string
	StatusCode int
}

func (e *BackendError) Error() string {
	return e.Message
}

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"Mansoor88-6/time-tracking-agent/internal/platform"

	"go.uber.org/zap"
)

// DeviceAuthService handles device authorization flow
type DeviceAuthService struct {
	platform     platform.Platform
	callbackPort int
	baseURL      string
	logger       *zap.Logger
}

// TokenResponse represents the response from token exchange
type TokenResponse struct {
	AccessToken string `json:"accessToken"`
	DeviceID    string `json:"deviceId"`
	ExpiresIn   int    `json:"expiresIn"` // seconds
}

// NewDeviceAuthService creates a new device authorization service
func NewDeviceAuthService(
	platform platform.Platform,
	callbackPort int,
	baseURL string,
	logger *zap.Logger,
) *DeviceAuthService {
	return &DeviceAuthService{
		platform:     platform,
		callbackPort: callbackPort,
		baseURL:      baseURL,
		logger:       logger,
	}
}

// AuthorizeDevice performs the OAuth-style device authorization flow
func (s *DeviceAuthService) AuthorizeDevice(deviceID, deviceName string) (string, error) {
	// Build authorization URL
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", s.callbackPort)
	authURL := fmt.Sprintf("%s/auth/device/authorize?deviceId=%s&redirectUri=%s",
		s.baseURL,
		url.QueryEscape(deviceID),
		url.QueryEscape(redirectURI),
	)
	if deviceName != "" {
		authURL += "&deviceName=" + url.QueryEscape(deviceName)
	}

	s.logger.Info("Starting device authorization",
		zap.String("device_id", deviceID),
		zap.String("auth_url", authURL),
	)

	// Create callback server
	callbackServer := NewCallbackServer(s.callbackPort, s.logger)

	// Create context with timeout (2 minutes)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start callback server
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		code, err := callbackServer.Start(ctx)
		if err != nil {
			errChan <- err
			return
		}
		codeChan <- code
	}()

	// Open browser
	s.logger.Info("Opening browser for authorization")
	if err := s.platform.OpenBrowser(authURL); err != nil {
		callbackServer.Stop()
		return "", fmt.Errorf("failed to open browser: %w", err)
	}

	// Wait for authorization code
	select {
	case code := <-codeChan:
		s.logger.Info("Authorization code received")
		callbackServer.Stop()
		return code, nil
	case err := <-errChan:
		callbackServer.Stop()
		return "", fmt.Errorf("callback server error: %w", err)
	case <-ctx.Done():
		callbackServer.Stop()
		return "", fmt.Errorf("authorization timeout: %w", ctx.Err())
	}
}

// ExchangeCodeForToken exchanges authorization code for device token
func (s *DeviceAuthService) ExchangeCodeForToken(code, deviceID string) (*TokenResponse, error) {
	url := fmt.Sprintf("%s/auth/device/token", s.baseURL)

	// Create request body
	reqBody := map[string]string{
		"code":     code,
		"deviceId": deviceID,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Accept both 200 OK and 201 Created as success
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("token exchange failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	s.logger.Info("Device token received",
		zap.String("device_id", tokenResp.DeviceID),
		zap.Int("expires_in", tokenResp.ExpiresIn),
	)

	return &tokenResp, nil
}

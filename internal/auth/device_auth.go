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

// AuthorizeDevice performs the OAuth-style device authorization flow.
// It starts a local callback server, opens the browser for login, and waits
// for the backend to redirect with an authorization code.
func (s *DeviceAuthService) AuthorizeDevice(deviceID, deviceName string) (string, error) {
	// Create callback server (will find an available port automatically)
	callbackServer := NewCallbackServer(s.callbackPort, s.logger)

	// Create context with timeout (5 minutes for user to log in)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Start callback server first so we know the actual port
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// We need the server to be listening before we build the URL, so start
	// it in a goroutine and wait for the actual port to become available.
	serverReady := make(chan struct{})
	go func() {
		// The Start method blocks until a code is received or context expires.
		// But first it binds the listener – we signal readiness right after that.
		// To achieve this cleanly, we use a two-phase approach: listen first,
		// then tell the main goroutine the port, then wait for the code.
		mux := http.NewServeMux()
		mux.HandleFunc("/callback", callbackServer.handleCallback)

		listener, actualPort, err := callbackServer.listen()
		if err != nil {
			errChan <- fmt.Errorf("failed to start callback server: %w", err)
			close(serverReady)
			return
		}
		callbackServer.ActualPort = actualPort

		callbackServer.server = &http.Server{Handler: mux}

		// Signal that we know the port
		close(serverReady)

		s.logger.Info("Callback server started",
			zap.Int("requested_port", s.callbackPort),
			zap.Int("actual_port", actualPort),
		)

		if err := callbackServer.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			callbackServer.errChan <- err
		}
	}()

	// Wait for the server to be ready or for an error
	select {
	case <-serverReady:
		// Check if there was an error during listen
		select {
		case err := <-errChan:
			return "", err
		default:
			// OK, server is listening
		}
	case <-ctx.Done():
		return "", fmt.Errorf("timeout waiting for callback server to start")
	}

	// Now we know the actual port – build the auth URL
	actualPort := callbackServer.ActualPort
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", actualPort)
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
		zap.Int("callback_port", actualPort),
		zap.String("auth_url", authURL),
	)

	// Open browser
	s.logger.Info("Opening browser for authorization")
	if err := s.platform.OpenBrowser(authURL); err != nil {
		callbackServer.Stop()
		return "", fmt.Errorf("failed to open browser: %w", err)
	}

	// Wait for authorization code from the callback handler
	go func() {
		select {
		case code := <-callbackServer.codeChan:
			codeChan <- code
		case err := <-callbackServer.errChan:
			errChan <- err
		}
	}()

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
		return "", fmt.Errorf("authorization timeout (5 min): user did not complete login")
	}
}

// ExchangeCodeForToken exchanges authorization code for device token
func (s *DeviceAuthService) ExchangeCodeForToken(code, deviceID string) (*TokenResponse, error) {
	tokenURL := fmt.Sprintf("%s/auth/device/token", s.baseURL)

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
	req, err := http.NewRequest("POST", tokenURL, bytes.NewBuffer(jsonData))
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

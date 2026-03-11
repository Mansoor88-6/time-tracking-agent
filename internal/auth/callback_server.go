package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
)

const (
	callbackHTML = `<!DOCTYPE html>
<html>
<head>
	<title>Device Registration</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			display: flex;
			justify-content: center;
			align-items: center;
			height: 100vh;
			margin: 0;
			background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
		}
		.container {
			background: white;
			padding: 2rem;
			border-radius: 10px;
			box-shadow: 0 10px 25px rgba(0,0,0,0.2);
			text-align: center;
			max-width: 400px;
		}
		.success {
			color: #10b981;
			font-size: 3rem;
			margin-bottom: 1rem;
		}
		h1 {
			color: #1f2937;
			margin: 0 0 1rem 0;
		}
		p {
			color: #6b7280;
			margin: 0.5rem 0;
		}
	</style>
</head>
<body>
	<div class="container">
		<div class="success">&#10003;</div>
		<h1>Device Registered Successfully!</h1>
		<p>Your device has been registered and authorized.</p>
		<p>You can close this window.</p>
	</div>
</body>
</html>`

	errorHTML = `<!DOCTYPE html>
<html>
<head>
	<title>Device Registration Error</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			display: flex;
			justify-content: center;
			align-items: center;
			height: 100vh;
			margin: 0;
			background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);
		}
		.container {
			background: white;
			padding: 2rem;
			border-radius: 10px;
			box-shadow: 0 10px 25px rgba(0,0,0,0.2);
			text-align: center;
			max-width: 400px;
		}
		.error {
			color: #ef4444;
			font-size: 3rem;
			margin-bottom: 1rem;
		}
		h1 {
			color: #1f2937;
			margin: 0 0 1rem 0;
		}
		p {
			color: #6b7280;
			margin: 0.5rem 0;
		}
	</style>
</head>
<body>
	<div class="container">
		<div class="error">&#10007;</div>
		<h1>Registration Failed</h1>
		<p>%s</p>
		<p>Please try again.</p>
	</div>
</body>
</html>`
)

// CallbackServer handles the OAuth callback from the browser
type CallbackServer struct {
	server   *http.Server
	codeChan chan string
	errChan  chan error
	logger   *zap.Logger
	port     int
	// ActualPort is the port the server actually bound to (may differ from
	// the requested port if that port was busy).
	ActualPort int
}

// NewCallbackServer creates a new callback server.
// preferredPort is tried first; if unavailable the OS picks a free port.
func NewCallbackServer(preferredPort int, logger *zap.Logger) *CallbackServer {
	return &CallbackServer{
		codeChan: make(chan string, 1),
		errChan:  make(chan error, 1),
		logger:   logger,
		port:     preferredPort,
	}
}

// Start starts the callback server and waits for the authorization code.
// It tries the preferred port first, then falls back to an OS-assigned port.
func (s *CallbackServer) Start(ctx context.Context) (string, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)

	// Try preferred port first, then fallback to OS-assigned port
	listener, actualPort, err := s.listen()
	if err != nil {
		return "", fmt.Errorf("failed to start callback server: %w", err)
	}
	s.ActualPort = actualPort

	s.server = &http.Server{
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		s.logger.Info("Callback server started",
			zap.Int("requested_port", s.port),
			zap.Int("actual_port", s.ActualPort),
		)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.errChan <- err
		}
	}()

	// Wait for code, error, or context cancellation
	select {
	case code := <-s.codeChan:
		return code, nil
	case err := <-s.errChan:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// listen tries to bind to the preferred port; if that fails it binds to :0
// (OS-assigned free port). Returns the listener and the actual port.
func (s *CallbackServer) listen() (net.Listener, int, error) {
	// Try preferred port
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		return listener, s.port, nil
	}

	s.logger.Warn("Preferred callback port unavailable, trying alternative ports",
		zap.Int("preferred_port", s.port),
		zap.Error(err),
	)

	// Try a small range of nearby ports
	for offset := 1; offset <= 20; offset++ {
		altPort := s.port + offset
		altAddr := fmt.Sprintf("127.0.0.1:%d", altPort)
		listener, err = net.Listen("tcp", altAddr)
		if err == nil {
			s.logger.Info("Using alternative callback port", zap.Int("port", altPort))
			return listener, altPort, nil
		}
	}

	// Last resort: let the OS pick any available port
	listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("could not bind to any port: %w", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	s.logger.Info("Using OS-assigned callback port", zap.Int("port", actualPort))
	return listener, actualPort, nil
}

// Stop stops the callback server
func (s *CallbackServer) Stop() error {
	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return s.server.Shutdown(ctx)
}

// handleCallback handles the OAuth callback request
func (s *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		s.logger.Error("Authorization error", zap.String("error", errorParam))
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf(errorHTML, errorParam)))
		s.errChan <- fmt.Errorf("authorization error: %s", errorParam)
		return
	}

	if code == "" {
		s.logger.Error("No authorization code received")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf(errorHTML, "No authorization code received")))
		s.errChan <- fmt.Errorf("no authorization code received")
		return
	}

	s.logger.Info("Authorization code received")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(callbackHTML))

	// Send code to channel (non-blocking)
	select {
	case s.codeChan <- code:
	default:
	}

	// Shutdown server after a short delay to allow response to be sent
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.Stop()
	}()
}

package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pbaity/lex/internal/logger" // Initialize logger
	"github.com/pbaity/lex/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testInitLogger initializes the logger for test execution, discarding output.
func testInitLogger(t *testing.T) {
	t.Helper()
	settings := models.ApplicationSettings{LogLevel: "error", LogFormat: "text"}
	err := logger.Init(settings, io.Discard)
	require.NoError(t, err, "Failed to initialize logger for test")
}

// --- Mock Queue ---
type mockQueue struct {
	EnqueueFunc func(event models.Event) error
	mu          sync.Mutex // Add mutex for safe concurrent access
	callCount   int
	lastEvent   models.Event
}

func (m *mockQueue) Enqueue(event models.Event) error {
	m.mu.Lock()
	m.callCount++
	m.lastEvent = event
	m.mu.Unlock()
	if m.EnqueueFunc != nil {
		return m.EnqueueFunc(event)
	}
	return nil // Default success
}

// Add methods to get call count and last event safely
func (m *mockQueue) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func (m *mockQueue) GetLastEvent() models.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastEvent
}

// --- Tests ---

func TestNewHTTPServer(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{
		Application: models.ApplicationSettings{ /* Add relevant settings if needed */ },
		// Add listeners if NewHTTPServer uses them during init
	}
	q := &mockQueue{}

	server := NewHTTPServer(cfg, q)

	require.NotNil(t, server)
	require.NotNil(t, server.mux)
	// require.NotNil(t, server.srv) // Cannot access unexported field
	// Cannot assert server.srv.Addr as it's unexported.
	// Default address is checked behaviorally in TestServer_StartStop if needed.
}

// Removed TestNewHTTPServer_WithCustomAddr as ServerAddr config is not implemented

// Helper to find a free port
func getFreePort(t *testing.T) string {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	require.NoError(t, err)
	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().String()
}

func TestServer_StartStop(t *testing.T) {
	testInitLogger(t)
	// Find a free port to avoid conflicts
	freeAddr := getFreePort(t)
	t.Logf("Using free address for test server: %s", freeAddr)

	// Address is hardcoded to :8080 in server.go currently.
	// For testing, we need a free port. We'll use the helper but
	// the server will ignore the config for now.
	// TODO: Update test when server address is configurable.
	cfg := &models.Config{Application: models.ApplicationSettings{}}
	q := &mockQueue{}
	// Create server - it will bind to :8080
	server := NewHTTPServer(cfg, q)
	// Override the address for testing purposes *after* creation
	server.server.Addr = freeAddr

	// Start the server in a goroutine
	server.Start()

	// Give the server a moment to start listening
	// Check if the port is actually listening
	var conn net.Conn
	var err error
	for i := 0; i < 10; i++ {
		conn, err = net.DialTimeout("tcp", freeAddr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break // Port is open
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, err, "Server did not start listening on %s", freeAddr)
	t.Logf("Successfully connected to test server on %s", freeAddr)

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = server.Stop(ctx)
	require.NoError(t, err, "Server stop should not return an error")

	// Verify the server stopped listening
	_, err = net.DialTimeout("tcp", freeAddr, 200*time.Millisecond)
	require.Error(t, err, "Server should not be listening after Stop()")
	assert.Contains(t, err.Error(), "connect: connection refused", "Error should be connection refused")

	// Try stopping again (should be safe)
	err = server.Stop(ctx)
	require.NoError(t, err, "Stopping an already stopped server should not error")
}

func TestServer_Stop_Timeout(t *testing.T) {
	testInitLogger(t)
	freeAddr := getFreePort(t)
	t.Logf("Using free address for test server: %s", freeAddr)

	cfg := &models.Config{Application: models.ApplicationSettings{}} // Address is hardcoded
	q := &mockQueue{}
	server := NewHTTPServer(cfg, q)
	// Override address for test
	server.server.Addr = freeAddr

	// Add a handler that hangs to test shutdown timeout
	server.mux.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Hang longer than shutdown timeout
	})

	server.Start()

	// Ensure server is up
	var conn net.Conn
	var err error
	for i := 0; i < 10; i++ {
		conn, err = net.DialTimeout("tcp", freeAddr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, err, "Server did not start listening on %s", freeAddr)

	// Make a request to the hanging handler (run in background)
	go func() {
		_, _ = http.Get("http://" + freeAddr + "/hang")
	}()
	time.Sleep(50 * time.Millisecond) // Give request time to start

	// Stop the server with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = server.Stop(ctx)
	// Shutdown should return a context deadline exceeded error because the handler hangs
	require.ErrorIs(t, err, context.DeadlineExceeded, "Server stop should return DeadlineExceeded error")
}

// --- Internal Handler Tests ---

func TestServer_HandleTrigger_Success(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{}
	q := &mockQueue{}
	server := NewHTTPServer(cfg, q)
	// No need to Start/Stop the actual HTTP server, just test the handler logic via httptest

	// Prepare request body
	reqBody := `{"action_id": "triggered_action", "parameters": {"p1": "v1"}}`
	req := httptest.NewRequest(http.MethodPost, "/lex/trigger", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	// Get the handler directly from the mux
	handler, pattern := server.mux.Handler(req)
	require.NotNil(t, handler, "Handler should be registered for /lex/trigger")
	assert.Equal(t, "/lex/trigger", pattern, "Pattern should match") // Ensure we got the right handler

	// Serve the request
	handler.ServeHTTP(rr, req)

	// Assertions
	assert.Equal(t, http.StatusAccepted, rr.Code, "Response code should be 202 Accepted")
	assert.Equal(t, 1, q.GetCallCount(), "Queue Enqueue should be called once") // Use getter
	lastEvent := q.GetLastEvent()                                               // Use getter
	assert.Equal(t, "triggered_action", lastEvent.ActionID)
	assert.Equal(t, "cli_trigger", lastEvent.SourceID)
	assert.Equal(t, models.EventTypeManual, lastEvent.Type)
	assert.Equal(t, "v1", lastEvent.Parameters["p1"])
	assert.WithinDuration(t, time.Now(), lastEvent.Timestamp, 5*time.Second)
}

func TestServer_HandleTrigger_BadRequest(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{}
	q := &mockQueue{}
	server := NewHTTPServer(cfg, q)
	handler, _ := server.mux.Handler(&http.Request{URL: &url.URL{Path: "/lex/trigger"}}) // Get handler

	tests := []struct {
		name           string
		method         string
		body           string
		expectedStatus int
		expectedBody   string // Substring to check in response body
	}{
		{
			name:           "Wrong Method",
			method:         http.MethodGet,
			body:           "",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method Not Allowed",
		},
		{
			name:           "Invalid JSON",
			method:         http.MethodPost,
			body:           `{"action_id": "test",`,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Bad Request",
		},
		{
			name:           "Missing ActionID",
			method:         http.MethodPost,
			body:           `{"parameters": {}}`,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "missing action_id",
		},
		{
			name:           "Empty Body",
			method:         http.MethodPost,
			body:           "",
			expectedStatus: http.StatusBadRequest, // Assuming Decode errors on empty body
			expectedBody:   "Bad Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q.callCount = 0 // Reset mock queue count
			req := httptest.NewRequest(tt.method, "/lex/trigger", strings.NewReader(tt.body))
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.expectedBody)
			assert.Equal(t, 0, q.GetCallCount(), "Queue should not be called on bad request")
		})
	}
}

func TestServer_HandleTrigger_EnqueueError(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{}
	// Mock queue that returns an error
	q := &mockQueue{
		EnqueueFunc: func(event models.Event) error {
			return fmt.Errorf("internal queue error")
		},
	}
	server := NewHTTPServer(cfg, q)
	handler, _ := server.mux.Handler(&http.Request{URL: &url.URL{Path: "/lex/trigger"}})

	reqBody := `{"action_id": "test_action"}`
	req := httptest.NewRequest(http.MethodPost, "/lex/trigger", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "Internal Server Error")
	assert.Equal(t, 1, q.GetCallCount(), "Queue Enqueue should still be called once")
}

func TestServer_HandleReload(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{}
	q := &mockQueue{}
	server := NewHTTPServer(cfg, q)
	handler, _ := server.mux.Handler(&http.Request{URL: &url.URL{Path: "/lex/reload"}})

	// Test POST (should be accepted, implementation pending)
	reqPost := httptest.NewRequest(http.MethodPost, "/lex/reload", nil)
	rrPost := httptest.NewRecorder()
	handler.ServeHTTP(rrPost, reqPost)
	assert.Equal(t, http.StatusAccepted, rrPost.Code)
	assert.Contains(t, rrPost.Body.String(), "Reload request received")

	// Test GET (should be rejected)
	reqGet := httptest.NewRequest(http.MethodGet, "/lex/reload", nil)
	rrGet := httptest.NewRecorder()
	handler.ServeHTTP(rrGet, reqGet)
	assert.Equal(t, http.StatusMethodNotAllowed, rrGet.Code)
}

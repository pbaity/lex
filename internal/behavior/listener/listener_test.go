package listener

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	callCount   int
	lastEvent   models.Event
}

func (m *mockQueue) Enqueue(event models.Event) error {
	m.callCount++
	m.lastEvent = event
	if m.EnqueueFunc != nil {
		return m.EnqueueFunc(event)
	}
	return nil // Default success
}
func (m *mockQueue) Start() error { return nil } // No-op for testing listener
func (m *mockQueue) Stop() error  { return nil } // No-op for testing listener

// --- Tests ---

func TestNewService(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{}
	q := &mockQueue{}
	mux := http.NewServeMux()

	service := NewService(cfg, q, mux)

	require.NotNil(t, service)
	assert.Equal(t, cfg, service.config)
	assert.Equal(t, q, service.eventQueue)
	assert.Equal(t, mux, service.mux)
	assert.NotNil(t, service.rateLimiters)
}

func TestService_Start_RegistersRoutes(t *testing.T) {
	testInitLogger(t)
	rateLimit := 5.0
	burst := 10
	cfg := &models.Config{
		Listeners: []models.ListenerConfig{
			{ID: "l1", Path: "/test/hook1", ActionID: "act1", AuthToken: "secret1"},
			{ID: "l2", Path: "/test/hook2", ActionID: "act2", RateLimit: &rateLimit, Burst: &burst},
			{ID: "l3", Path: "/test/hook3", ActionID: "act3"}, // No auth, no rate limit
		},
	}
	q := &mockQueue{}
	mux := http.NewServeMux() // Use a real mux to check registrations
	service := NewService(cfg, q, mux)

	err := service.Start()
	require.NoError(t, err)

	// Verify rate limiters were created
	assert.Contains(t, service.rateLimiters, "l2", "Rate limiter should exist for l2")
	assert.NotContains(t, service.rateLimiters, "l1", "Rate limiter should not exist for l1")
	assert.NotContains(t, service.rateLimiters, "l3", "Rate limiter should not exist for l3")

	// Use httptest to check if routes are handled
	server := httptest.NewServer(mux)
	defer server.Close()

	// Test hook1 (requires auth)
	req1, _ := http.NewRequest("POST", server.URL+"/test/hook1", strings.NewReader(`{"data":"value1"}`))
	resp1, err1 := http.DefaultClient.Do(req1)
	require.NoError(t, err1)
	assert.Equal(t, http.StatusUnauthorized, resp1.StatusCode, "Hook1 should require auth")
	resp1.Body.Close()

	req1Auth, _ := http.NewRequest("POST", server.URL+"/test/hook1", strings.NewReader(`{"data":"value1"}`))
	req1Auth.Header.Set("Authorization", "Bearer secret1")
	resp1Auth, err1Auth := http.DefaultClient.Do(req1Auth)
	require.NoError(t, err1Auth)
	assert.Equal(t, http.StatusAccepted, resp1Auth.StatusCode, "Hook1 should succeed with auth") // Expect 202 Accepted
	resp1Auth.Body.Close()

	// Test hook2 (rate limited, no auth)
	req2, _ := http.NewRequest("POST", server.URL+"/test/hook2", strings.NewReader(`{"data":"value2"}`))
	resp2, err2 := http.DefaultClient.Do(req2)
	require.NoError(t, err2)
	assert.Equal(t, http.StatusAccepted, resp2.StatusCode, "Hook2 should succeed") // Expect 202 Accepted
	resp2.Body.Close()

	// Test hook3 (no auth, no rate limit)
	req3, _ := http.NewRequest("POST", server.URL+"/test/hook3", strings.NewReader(`{"data":"value3"}`))
	resp3, err3 := http.DefaultClient.Do(req3)
	require.NoError(t, err3)
	assert.Equal(t, http.StatusAccepted, resp3.StatusCode, "Hook3 should succeed") // Expect 202 Accepted
	resp3.Body.Close()

	// Check if queue received events (basic check)
	// Note: This only checks the *last* event due to mockQueue simplicity
	assert.Equal(t, 3, q.callCount) // Should have tried to enqueue 3 times (1 auth fail, 2 success)
	assert.Equal(t, "act3", q.lastEvent.ActionID)
	assert.Equal(t, "l3", q.lastEvent.SourceID)
	assert.Equal(t, models.EventTypeListener, q.lastEvent.Type)
}

func TestService_HandleWebhook_Enqueue(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{
		Listeners: []models.ListenerConfig{
			{ID: "l1", Path: "/hook", ActionID: "act1"},
		},
	}
	q := &mockQueue{}
	mux := http.NewServeMux()
	service := NewService(cfg, q, mux)
	err := service.Start()
	require.NoError(t, err)

	// Simulate request
	req := httptest.NewRequest("POST", "/hook", strings.NewReader(`{"key":"val"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req) // Use the mux where the handler was registered

	assert.Equal(t, http.StatusAccepted, rr.Code) // Expect 202 Accepted
	assert.Equal(t, 1, q.callCount)
	assert.Equal(t, "act1", q.lastEvent.ActionID)
	assert.Equal(t, "l1", q.lastEvent.SourceID)
	assert.Equal(t, models.EventTypeListener, q.lastEvent.Type)
	// assert.NotEmpty(t, q.lastEvent.ID) // Remove ID check as mock doesn't generate it
	assert.WithinDuration(t, time.Now(), q.lastEvent.Timestamp, 5*time.Second)
	// Note: Parameter extraction from body is not tested here, would require more complex setup
}

func TestService_HandleWebhook_AuthFail(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{
		Listeners: []models.ListenerConfig{
			{ID: "l1", Path: "/hook", ActionID: "act1", AuthToken: "secret"},
		},
	}
	q := &mockQueue{}
	mux := http.NewServeMux()
	service := NewService(cfg, q, mux)
	err := service.Start()
	require.NoError(t, err)

	// Simulate request without token
	req := httptest.NewRequest("POST", "/hook", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Equal(t, 0, q.callCount) // Should not enqueue

	// Simulate request with wrong token
	reqWrong := httptest.NewRequest("POST", "/hook", strings.NewReader(`{}`))
	reqWrong.Header.Set("Authorization", "Bearer wrongsecret")
	rrWrong := httptest.NewRecorder()
	mux.ServeHTTP(rrWrong, reqWrong)
	assert.Equal(t, http.StatusUnauthorized, rrWrong.Code)
	assert.Equal(t, 0, q.callCount) // Should not enqueue
}

func TestService_HandleWebhook_RateLimit(t *testing.T) {
	testInitLogger(t)
	rateLimit := 1.0 // 1 request per second
	burst := 1
	cfg := &models.Config{
		Listeners: []models.ListenerConfig{
			{ID: "l1", Path: "/hook", ActionID: "act1", RateLimit: &rateLimit, Burst: &burst},
		},
	}
	q := &mockQueue{}
	mux := http.NewServeMux()
	service := NewService(cfg, q, mux)
	err := service.Start()
	require.NoError(t, err)

	// Simulate requests
	req := httptest.NewRequest("POST", "/hook", strings.NewReader(`{}`))
	rr1 := httptest.NewRecorder()
	rr2 := httptest.NewRecorder()

	// First request should succeed
	mux.ServeHTTP(rr1, req.Clone(context.Background()))
	assert.Equal(t, http.StatusAccepted, rr1.Code) // Expect 202 Accepted
	assert.Equal(t, 1, q.callCount)

	// Immediate second request should be rate limited
	mux.ServeHTTP(rr2, req.Clone(context.Background()))
	assert.Equal(t, http.StatusTooManyRequests, rr2.Code)
	assert.Equal(t, 1, q.callCount) // Count should not increase

	// Wait for rate limiter to allow another request
	time.Sleep(time.Second * 11 / 10) // Sleep slightly more than 1 second

	rr3 := httptest.NewRecorder()
	mux.ServeHTTP(rr3, req.Clone(context.Background()))
	assert.Equal(t, http.StatusAccepted, rr3.Code) // Expect 202 Accepted
	assert.Equal(t, 2, q.callCount)                // Count should increase now
}

// TODO: Add test for invalid listener config during Start (e.g., invalid path) if Start validates.
// TODO: Add test for error during queue Enqueue.

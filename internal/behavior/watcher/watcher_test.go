package watcher

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/pbaity/lex/internal/action" // Need executor for tests
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
	mu          sync.Mutex
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

// --- Mock Executor ---
// We need a mock executor because the real one executes actual scripts.
type mockExecutor struct {
	ExecuteFunc func(ctx context.Context, event models.Event, actionCfg models.ActionConfig) (stdout, stderr string, err error)
	callCount   int
	lastEvent   models.Event
	lastAction  models.ActionConfig
}

func (m *mockExecutor) Execute(ctx context.Context, event models.Event, actionCfg models.ActionConfig) (string, string, error) {
	m.callCount++
	m.lastEvent = event
	m.lastAction = actionCfg
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, event, actionCfg)
	}
	// Simulate script execution based on ActionID for testing watchers
	if actionCfg.ID == "script-success" {
		return "stdout success", "stderr success", nil // Exit 0
	}
	if actionCfg.ID == "script-fail" {
		return "stdout fail", "stderr fail", fmt.Errorf("exit status 1") // Exit 1
	}
	return "", "", fmt.Errorf("unknown script mock")
}

// --- Tests ---

func TestNewService(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{}
	q := &mockQueue{}
	exec := &mockExecutor{}

	service := NewService(cfg, q, exec)

	require.NotNil(t, service)
	assert.Equal(t, cfg, service.config)
	assert.Equal(t, q, service.eventQueue)
	assert.Equal(t, exec, service.actionExecutor) // Check the interface field
	assert.NotNil(t, service.cancelFuncs)
}

func TestService_StartStop_NoWatchers(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{} // No watchers
	q := &mockQueue{}
	exec := &mockExecutor{}
	service := NewService(cfg, q, exec)

	err := service.Start()
	require.NoError(t, err)
	err = service.Stop()
	require.NoError(t, err)
}

func TestService_StartStop_WithWatchers(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{
		Watchers: []models.WatcherConfig{
			{ID: "w1", ActionID: "act1", Script: "script-success", Interval: models.Duration{Duration: 1 * time.Hour}},
			{ID: "w2", ActionID: "act2", Script: "script-fail", Interval: models.Duration{Duration: 30 * time.Minute}},
		},
		// Need dummy actions for validation in Start
		Actions: []models.ActionConfig{
			{ID: "act1", Script: "dummy1"},
			{ID: "act2", Script: "dummy2"},
			{ID: "script-success", Script: "dummy3"}, // Action for the script itself
			{ID: "script-fail", Script: "dummy4"},    // Action for the script itself
		},
	}
	q := &mockQueue{}
	// Use real executor here as Start doesn't execute, just schedules
	exec := action.NewExecutor()
	service := NewService(cfg, q, exec)

	err := service.Start()
	require.NoError(t, err)

	// Check if cancel funcs were stored
	service.mu.Lock()
	assert.Len(t, service.cancelFuncs, 2, "Should have created 2 cancel funcs")
	assert.Contains(t, service.cancelFuncs, "w1")
	assert.Contains(t, service.cancelFuncs, "w2")
	service.mu.Unlock()

	// Stop the service
	err = service.Stop()
	require.NoError(t, err)

	// Check if map is cleared after stop
	service.mu.Lock()
	assert.Empty(t, service.cancelFuncs, "cancelFuncs map should be empty after Stop()")
	service.mu.Unlock()
}

func TestService_WatcherTick_Success_EnqueuesEvent(t *testing.T) {
	testInitLogger(t)
	testInterval := 50 * time.Millisecond
	cfg := &models.Config{
		Watchers: []models.WatcherConfig{
			// Watcher 'w-success' runs script 'echo ok' which mock executor simulates as exit 0
			// On success, it should trigger action 'act-success'
			{ID: "w-success", ActionID: "act-success", Script: "echo ok", Interval: models.Duration{Duration: testInterval}},
		},
		Actions: []models.ActionConfig{
			{ID: "act-success", Script: "dummy-action"}, // The action triggered BY the watcher
		},
	}
	q := &mockQueue{}
	// Mock executor simulates script success (exit 0)
	exec := &mockExecutor{
		ExecuteFunc: func(ctx context.Context, event models.Event, actionCfg models.ActionConfig) (string, string, error) {
			// Simulate success for any script execution in this test
			return "stdout", "stderr", nil
		},
	}
	service := NewService(cfg, q, exec)

	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	// Wait for watcher to run and enqueue the event
	assert.Eventually(t, func() bool {
		return q.GetCallCount() >= 1
	}, testInterval*5, testInterval/2, "Queue should have received event after watcher success") // Increased timeout

	// Check the enqueued event details
	lastEvent := q.GetLastEvent()
	assert.Equal(t, "act-success", lastEvent.ActionID) // Should be the action triggered BY the watcher
	assert.Equal(t, "w-success", lastEvent.SourceID)
	assert.Equal(t, models.EventTypeWatcher, lastEvent.Type)
}

func TestService_WatcherTick_Failure_DoesNotEnqueue(t *testing.T) {
	testInitLogger(t)
	testInterval := 50 * time.Millisecond
	cfg := &models.Config{
		Watchers: []models.WatcherConfig{
			// Watcher 'w-fail' runs script 'exit 1' which mock executor simulates as exit 1
			// On failure, it should NOT trigger action 'act-fail'
			{ID: "w-fail", ActionID: "act-fail", Script: "exit 1", Interval: models.Duration{Duration: testInterval}},
		},
		Actions: []models.ActionConfig{
			{ID: "act-fail", Script: "dummy-action"}, // The action that SHOULD NOT be triggered
		},
	}
	q := &mockQueue{}
	// Mock executor simulates script failure (exit 1)
	exec := &mockExecutor{
		ExecuteFunc: func(ctx context.Context, event models.Event, actionCfg models.ActionConfig) (string, string, error) {
			// Simulate failure for any script execution in this test
			return "stdout", "stderr", fmt.Errorf("exit status 1")
		},
	}
	service := NewService(cfg, q, exec)

	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	// Wait for watcher to run
	time.Sleep(testInterval * 3) // Wait longer than interval

	// Assert that the queue was NOT called
	assert.Equal(t, 0, q.GetCallCount(), "Queue should not have been called after watcher failure")
}

// TODO: Add tests for watcher retry logic if implemented.
// TODO: Add test for invalid watcher config (bad interval, missing action).
// TODO: Add test for error during script execution itself (not just exit code).
// TODO: Add test for error during queue Enqueue.

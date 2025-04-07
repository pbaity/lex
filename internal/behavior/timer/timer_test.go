package timer

import (
	"fmt"
	"io"
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

// --- Tests ---

func TestNewService(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{}
	q := &mockQueue{}

	service := NewService(cfg, q)

	require.NotNil(t, service)
	assert.Equal(t, cfg, service.config)
	assert.Equal(t, q, service.eventQueue)
	// Cannot assert unexported fields: service.tickers, service.stopChan
	// Asserting service.cancelFuncs map is initialized instead (based on timer.go)
	assert.NotNil(t, service.cancelFuncs)
}

func TestService_StartStop_NoTimers(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{} // No timers defined
	q := &mockQueue{}
	service := NewService(cfg, q)

	err := service.Start()
	require.NoError(t, err)

	// Stop should also work fine
	err = service.Stop()
	require.NoError(t, err)
}

func TestService_StartStop_WithTimers(t *testing.T) {
	testInitLogger(t)
	cfg := &models.Config{
		Timers: []models.TimerConfig{
			{ID: "t1", ActionID: "act1", Interval: models.Duration{Duration: 1 * time.Hour}},
			{ID: "t2", ActionID: "act2", Interval: models.Duration{Duration: 30 * time.Minute}},
		},
	}
	q := &mockQueue{}
	service := NewService(cfg, q)

	err := service.Start()
	require.NoError(t, err, "Start should succeed")

	// Stop the service - the main check is that Stop completes without error/timeout
	err = service.Stop()
	require.NoError(t, err, "Stop should succeed")

	// We no longer check internal fields like tickers or stopChan.
	// The functionality is verified by TestService_TimerTick_EnqueuesEvent and TestService_Stop_StopsTicks.
}

func TestService_TimerTick_EnqueuesEvent(t *testing.T) {
	testInitLogger(t)
	// Use a very short interval for testing
	testInterval := 50 * time.Millisecond
	cfg := &models.Config{
		Timers: []models.TimerConfig{
			{ID: "t1", ActionID: "act-tick", Interval: models.Duration{Duration: testInterval}},
		},
		// Add dummy action config to satisfy validation in Start()
		Actions: []models.ActionConfig{{ID: "act-tick", Script: "dummy"}},
	}
	q := &mockQueue{}
	service := NewService(cfg, q)

	err := service.Start()
	require.NoError(t, err)
	defer service.Stop() // Ensure stop is called

	// Wait for slightly longer than the interval for the timer to tick at least once
	assert.Eventually(t, func() bool {
		return q.GetCallCount() >= 1
	}, testInterval*5, testInterval/2, "Queue should have received at least one event") // Increased timeout

	// Check the details of the enqueued event
	lastEvent := q.GetLastEvent()
	assert.Equal(t, "act-tick", lastEvent.ActionID)
	assert.Equal(t, "t1", lastEvent.SourceID)
	assert.Equal(t, models.EventTypeTimer, lastEvent.Type)
	// assert.NotEmpty(t, lastEvent.ID) // Remove check; ID generation is queue's responsibility
	assert.WithinDuration(t, time.Now(), lastEvent.Timestamp, 5*time.Second)

	// Wait again to see if it ticks multiple times
	initialCount := q.GetCallCount()
	assert.Eventually(t, func() bool {
		return q.GetCallCount() > initialCount
	}, testInterval*5, testInterval/2, "Queue should have received more events") // Increased timeout
}

func TestService_Stop_StopsTicks(t *testing.T) {
	testInitLogger(t)
	testInterval := 50 * time.Millisecond
	cfg := &models.Config{
		Timers: []models.TimerConfig{
			{ID: "t1", ActionID: "act-stop", Interval: models.Duration{Duration: testInterval}},
		},
		// Add dummy action config
		Actions: []models.ActionConfig{{ID: "act-stop", Script: "dummy"}},
	}
	q := &mockQueue{}
	service := NewService(cfg, q)

	err := service.Start()
	require.NoError(t, err)

	// Wait for at least one tick
	require.Eventually(t, func() bool {
		return q.GetCallCount() >= 1
	}, testInterval*5, testInterval/2, "Queue should have received at least one event before stop") // Increased timeout

	// Stop the service
	err = service.Stop()
	require.NoError(t, err)

	// Record the count after stop
	countAfterStop := q.GetCallCount()

	// Wait for longer than the interval and verify the count doesn't increase
	time.Sleep(testInterval * 3)
	assert.Equal(t, countAfterStop, q.GetCallCount(), "Enqueue count should not increase after Stop()")
}

func TestService_TimerTick_EnqueueError(t *testing.T) {
	testInitLogger(t)
	testInterval := 50 * time.Millisecond
	cfg := &models.Config{
		Timers: []models.TimerConfig{
			{ID: "t-err", ActionID: "act-err", Interval: models.Duration{Duration: testInterval}},
		},
		// Add dummy action config
		Actions: []models.ActionConfig{{ID: "act-err", Script: "dummy"}},
	}
	// Mock queue that returns an error on Enqueue
	q := &mockQueue{
		EnqueueFunc: func(event models.Event) error {
			return fmt.Errorf("queue is full") // Simulate an enqueue error
		},
	}
	service := NewService(cfg, q)

	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	// Wait for slightly longer than the interval for the timer to attempt enqueue
	// We can't easily assert the error was logged without capturing logs,
	// but we can check that the call count increased (meaning enqueue was attempted).
	assert.Eventually(t, func() bool {
		return q.GetCallCount() >= 1
	}, testInterval*5, testInterval/2, "Queue enqueue should have been attempted at least once")

	// The error is logged internally, the service continues running.
}

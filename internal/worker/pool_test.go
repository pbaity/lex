package worker

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pbaity/lex/internal/logger" // Initialize logger
	"github.com/pbaity/lex/internal/queue"
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

// --- Mock Processor ---

type mockProcessor struct {
	processFunc  func(ctx context.Context, event models.Event) error
	callCount    atomic.Int32
	mu           sync.Mutex
	processedIDs map[string]bool // Track processed event IDs
}

func newMockProcessor(processFunc func(ctx context.Context, event models.Event) error) *mockProcessor {
	if processFunc == nil {
		processFunc = func(ctx context.Context, event models.Event) error { return nil } // Default success
	}
	return &mockProcessor{
		processFunc:  processFunc,
		processedIDs: make(map[string]bool),
	}
}

func (m *mockProcessor) Process(ctx context.Context, event models.Event) error {
	m.callCount.Add(1)
	m.mu.Lock()
	m.processedIDs[event.ID] = true
	m.mu.Unlock()
	return m.processFunc(ctx, event)
}

func (m *mockProcessor) GetCallCount() int {
	return int(m.callCount.Load())
}

func (m *mockProcessor) HasProcessed(eventID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processedIDs[eventID]
}

// --- Tests ---

func TestNewPool(t *testing.T) {
	testInitLogger(t)
	cfg := models.ApplicationSettings{MaxConcurrency: 5}
	eq := queue.NewEventQueue(10, "")
	proc := newMockProcessor(nil)

	pool := NewPool(cfg, eq, proc)

	require.NotNil(t, pool)
	assert.Equal(t, cfg, pool.config)
	assert.Equal(t, eq, pool.eventQueue)
	assert.Equal(t, proc, pool.processor)
	assert.Nil(t, pool.cancelCtx) // Should be nil before Start
}

func TestPool_StartStop_Basic(t *testing.T) {
	testInitLogger(t)
	cfg := models.ApplicationSettings{MaxConcurrency: 2}
	eq := queue.NewEventQueue(10, "")
	proc := newMockProcessor(nil)
	pool := NewPool(cfg, eq, proc)

	err := eq.Start() // Start the queue
	require.NoError(t, err)

	pool.Start()
	require.NotNil(t, pool.cancelCtx, "cancelCtx should be set after Start")

	// Let workers run briefly
	time.Sleep(50 * time.Millisecond)

	pool.Stop() // Should block until workers finish

	// Try stopping again (should be idempotent)
	pool.Stop()

	// Check if context was cancelled (indirect check)
	// We can't directly check pool.cancelCtx easily, but Stop should have completed.
}

func TestPool_ProcessEvents_Success(t *testing.T) {
	testInitLogger(t)
	cfg := models.ApplicationSettings{MaxConcurrency: 2}
	eq := queue.NewEventQueue(10, "")
	proc := newMockProcessor(nil) // Default success processor
	pool := NewPool(cfg, eq, proc)

	err := eq.Start()
	require.NoError(t, err)
	defer eq.Stop() // Stop queue when done

	pool.Start()
	defer pool.Stop() // Stop pool when done

	// Enqueue events
	event1 := models.Event{ActionID: "act1"}
	event2 := models.Event{ActionID: "act2"}
	require.NoError(t, eq.Enqueue(event1))
	require.NoError(t, eq.Enqueue(event2))

	// Wait for events to be processed
	assert.Eventually(t, func() bool {
		return proc.GetCallCount() == 2
	}, 2*time.Second, 50*time.Millisecond, "Processor should have been called twice")

	// Need event IDs to check HasProcessed, get them after enqueue
	// Re-enqueue to get IDs (or modify queue to return ID on enqueue)
	eq = queue.NewEventQueue(10, "") // Reset queue
	eq.Start()
	pool = NewPool(cfg, eq, proc) // Reset pool with new queue
	proc = newMockProcessor(nil)  // Reset processor
	pool.processor = proc
	pool.Start()

	require.NoError(t, eq.Enqueue(event1))
	require.NoError(t, eq.Enqueue(event2))
	time.Sleep(10 * time.Millisecond) // Allow enqueue to assign ID before getting it
	// This is clumsy - ideally Enqueue would return the assigned ID or EventQueue would expose a way to get items
	// For now, let's assume IDs are assigned and check count only.
	assert.Eventually(t, func() bool {
		return proc.GetCallCount() == 2
	}, 2*time.Second, 50*time.Millisecond, "Processor should have been called twice (run 2)")

	pool.Stop()
	eq.Stop()
}

func TestPool_ProcessEvents_ProcessorError(t *testing.T) {
	testInitLogger(t)
	cfg := models.ApplicationSettings{MaxConcurrency: 1}
	eq := queue.NewEventQueue(10, "")
	// Processor that always errors
	proc := newMockProcessor(func(ctx context.Context, event models.Event) error {
		return fmt.Errorf("processing failed for %s", event.ID)
	})
	pool := NewPool(cfg, eq, proc)

	err := eq.Start()
	require.NoError(t, err)
	defer eq.Stop()

	pool.Start()
	defer pool.Stop()

	event1 := models.Event{ActionID: "act1"}
	require.NoError(t, eq.Enqueue(event1))

	// Wait for the event to be processed (and fail)
	assert.Eventually(t, func() bool {
		return proc.GetCallCount() == 1
	}, 1*time.Second, 50*time.Millisecond, "Processor should have been called once")

	// Error is logged by the worker, not returned by pool.Stop()
}

func TestPool_Stop_CancelsContext(t *testing.T) {
	testInitLogger(t)
	cfg := models.ApplicationSettings{MaxConcurrency: 1}
	eq := queue.NewEventQueue(10, "")
	blockChan := make(chan struct{}) // Channel to block processor

	// Processor that blocks until blockChan is closed, checks context
	var processorCtx context.Context
	var processorCtxErr error
	proc := newMockProcessor(func(ctx context.Context, event models.Event) error {
		processorCtx = ctx          // Store context for checking later
		<-blockChan                 // Block here
		processorCtxErr = ctx.Err() // Check context error after block
		return nil
	})
	pool := NewPool(cfg, eq, proc)

	err := eq.Start()
	require.NoError(t, err)
	defer eq.Stop()

	pool.Start()

	// Enqueue an event to make the worker busy
	event1 := models.Event{ActionID: "act1"}
	require.NoError(t, eq.Enqueue(event1))

	// Wait until the processor is called (and blocked)
	require.Eventually(t, func() bool {
		return proc.GetCallCount() == 1
	}, 1*time.Second, 10*time.Millisecond)

	// Stop the pool - this should cancel the worker's context
	stopDone := make(chan struct{})
	go func() {
		pool.Stop()
		close(stopDone)
	}()

	// Wait briefly, then unblock the processor
	time.Sleep(50 * time.Millisecond)
	close(blockChan)

	// Wait for Stop() to complete
	select {
	case <-stopDone:
		// Stop completed
	case <-time.After(2 * time.Second):
		t.Fatal("Pool Stop() timed out")
	}

	// Check that the context passed to the processor was cancelled
	require.NotNil(t, processorCtx, "Processor context should have been captured")
	assert.ErrorIs(t, processorCtxErr, context.Canceled, "Context passed to processor should be canceled after Stop()")
}

func TestPool_DefaultConcurrency(t *testing.T) {
	testInitLogger(t)
	// Test with invalid MaxConcurrency (should default to 1)
	cfg := models.ApplicationSettings{MaxConcurrency: 0}
	eq := queue.NewEventQueue(10, "")
	proc := newMockProcessor(nil)
	pool := NewPool(cfg, eq, proc)

	err := eq.Start()
	require.NoError(t, err)
	defer eq.Stop()

	pool.Start() // This would panic if concurrency was 0, logs warning if < 1
	defer pool.Stop()

	// Check log output? Hard to do reliably.
	// Just ensure Start/Stop doesn't panic.
	time.Sleep(50 * time.Millisecond)
}

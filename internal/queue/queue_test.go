package queue

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pbaity/lex/internal/logger" // Initialize logger
	"github.com/pbaity/lex/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testInitLogger initializes the logger for test execution, discarding output.
func testInitLogger(t *testing.T) {
	t.Helper()
	settings := models.ApplicationSettings{LogLevel: "error", LogFormat: "text"}
	// Discard logs during queue tests unless debugging
	err := logger.Init(settings, io.Discard)
	require.NoError(t, err, "Failed to initialize logger for test")
}

// Helper to create a unique temp file path for persistence
func tempPersistPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), fmt.Sprintf("queue_state_%s.json", uuid.NewString()))
}

func TestNewEventQueue(t *testing.T) {
	testInitLogger(t)
	path := tempPersistPath(t)

	// Test with valid capacity
	q := NewEventQueue(50, path)
	require.NotNil(t, q)
	assert.Equal(t, 50, q.capacity)
	assert.Equal(t, path, q.persistPath)
	assert.NotNil(t, q.queue)
	assert.NotNil(t, q.stopChan)
	assert.Equal(t, 50, cap(q.queue))

	// Test with zero capacity (should use default)
	qDefault := NewEventQueue(0, "")
	require.NotNil(t, qDefault)
	assert.Equal(t, defaultQueueCapacity, qDefault.capacity)
	assert.Equal(t, "", qDefault.persistPath)
	assert.Equal(t, defaultQueueCapacity, cap(qDefault.queue))

	// Test with negative capacity (should use default)
	qNeg := NewEventQueue(-10, "")
	require.NotNil(t, qNeg)
	assert.Equal(t, defaultQueueCapacity, qNeg.capacity)
	assert.Equal(t, defaultQueueCapacity, cap(qNeg.queue))
}

func TestEventQueue_EnqueueDequeue_Simple(t *testing.T) {
	testInitLogger(t)
	q := NewEventQueue(10, "") // No persistence for this test
	err := q.Start()           // Need to start for enqueue/dequeue to work properly
	require.NoError(t, err)
	defer q.Stop() // Ensure stop is called

	event1 := models.Event{ActionID: "action1", SourceID: "source1"}
	event2 := models.Event{ActionID: "action2", SourceID: "source2"}

	// Enqueue first event
	err = q.Enqueue(event1)
	require.NoError(t, err)

	// Enqueue second event
	err = q.Enqueue(event2)
	require.NoError(t, err)

	// Dequeue first event
	ctx := context.Background()
	dequeuedEvent1, err := q.Dequeue(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, dequeuedEvent1.ID, "Dequeued event should have an ID")
	assert.Equal(t, event1.ActionID, dequeuedEvent1.ActionID)
	assert.Equal(t, event1.SourceID, dequeuedEvent1.SourceID)

	// Dequeue second event
	dequeuedEvent2, err := q.Dequeue(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, dequeuedEvent2.ID, "Dequeued event should have an ID")
	assert.Equal(t, event2.ActionID, dequeuedEvent2.ActionID)
	assert.Equal(t, event2.SourceID, dequeuedEvent2.SourceID)

	// Ensure IDs are unique
	assert.NotEqual(t, dequeuedEvent1.ID, dequeuedEvent2.ID)
}

func TestEventQueue_Enqueue_AssignsID(t *testing.T) {
	testInitLogger(t)
	q := NewEventQueue(1, "")
	q.Start()
	defer q.Stop()

	event := models.Event{ActionID: "action1"} // No ID assigned
	err := q.Enqueue(event)
	require.NoError(t, err)

	dequeuedEvent, err := q.Dequeue(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, dequeuedEvent.ID)
}

func TestEventQueue_Enqueue_StoppedQueue(t *testing.T) {
	testInitLogger(t)
	q := NewEventQueue(1, "")
	q.Start()
	err := q.Stop() // Stop the queue
	require.NoError(t, err)

	event := models.Event{ActionID: "action1"}
	err = q.Enqueue(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event queue is stopped")
}

func TestEventQueue_Dequeue_ContextCancelled(t *testing.T) {
	testInitLogger(t)
	q := NewEventQueue(1, "")
	q.Start()
	defer q.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := q.Dequeue(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEventQueue_Dequeue_StoppedQueue_Empty(t *testing.T) {
	testInitLogger(t)
	q := NewEventQueue(1, "")
	q.Start()
	err := q.Stop() // Stop the queue while it's empty
	require.NoError(t, err)

	// Attempt to dequeue after stop
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond) // Short timeout
	defer cancel()
	_, err = q.Dequeue(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event queue stopped")
}

func TestEventQueue_Dequeue_StoppedQueue_WithItems(t *testing.T) {
	testInitLogger(t)
	q := NewEventQueue(5, "")
	q.Start()

	event1 := models.Event{ActionID: "action1"}
	err := q.Enqueue(event1)
	require.NoError(t, err)

	// Stop the queue *after* enqueuing
	err = q.Stop()
	require.NoError(t, err)

	// Dequeue should still return the item that was present before stop completed
	ctx := context.Background()
	dequeuedEvent, err := q.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, event1.ActionID, dequeuedEvent.ActionID)

	// Subsequent dequeue should fail as the queue is now drained and stopped
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = q.Dequeue(ctxTimeout)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event queue stopped")
}

func TestEventQueue_Persistence_SaveLoad(t *testing.T) {
	testInitLogger(t)
	persistPath := tempPersistPath(t)
	defer os.Remove(persistPath)          // Clean up persist file
	defer os.Remove(persistPath + ".tmp") // Clean up temp file if it exists

	// --- Phase 1: Create, Enqueue, Stop (Save) ---
	q1 := NewEventQueue(10, persistPath)
	err := q1.Start()
	require.NoError(t, err)

	event1 := models.Event{ActionID: "act1", SourceID: "s1", Parameters: map[string]string{"p1": "v1"}}
	event2 := models.Event{ActionID: "act2", SourceID: "s2"}
	err = q1.Enqueue(event1)
	require.NoError(t, err)
	err = q1.Enqueue(event2)
	require.NoError(t, err)

	// Stop should trigger saveState
	err = q1.Stop()
	require.NoError(t, err)

	// Verify persistence file exists and has content
	_, err = os.Stat(persistPath)
	require.NoError(t, err, "Persistence file should exist after stop")
	data, err := os.ReadFile(persistPath)
	require.NoError(t, err)
	require.NotEmpty(t, data, "Persistence file should not be empty")

	// Basic check: does it look like JSON array?
	assert.True(t, strings.HasPrefix(string(data), "["), "Persistence file should start with '['")

	// --- Phase 2: Create new queue, Start (Load), Dequeue ---
	q2 := NewEventQueue(10, persistPath)
	err = q2.Start() // Should trigger loadState
	require.NoError(t, err)
	defer q2.Stop()

	// Dequeue and verify events (order should be preserved - FIFO)
	ctx := context.Background()
	dq1, err := q2.Dequeue(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, dq1.ID)
	assert.Equal(t, event1.ActionID, dq1.ActionID)
	assert.Equal(t, event1.SourceID, dq1.SourceID)
	assert.Equal(t, event1.Parameters, dq1.Parameters)

	dq2, err := q2.Dequeue(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, dq2.ID)
	assert.Equal(t, event2.ActionID, dq2.ActionID)
	assert.Equal(t, event2.SourceID, dq2.SourceID)
	assert.Nil(t, dq2.Parameters) // Event 2 had no parameters

	// Ensure IDs were preserved/regenerated correctly (they should be unique)
	assert.NotEqual(t, dq1.ID, dq2.ID)

	// Queue should now be empty
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = q2.Dequeue(ctxTimeout)
	assert.Error(t, err) // Expect timeout or stopped error
}

func TestEventQueue_Persistence_Load_FileNotExist(t *testing.T) {
	testInitLogger(t)
	persistPath := tempPersistPath(t) // Path that doesn't exist yet

	q := NewEventQueue(10, persistPath)
	err := q.Start() // Should not error if file doesn't exist
	require.NoError(t, err)
	defer q.Stop()

	// Queue should be empty
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = q.Dequeue(ctx)
	assert.Error(t, err)
}

func TestEventQueue_Persistence_Load_EmptyFile(t *testing.T) {
	testInitLogger(t)
	persistPath := tempPersistPath(t)
	defer os.Remove(persistPath)

	// Create an empty file
	err := os.WriteFile(persistPath, []byte{}, 0644)
	require.NoError(t, err)

	q := NewEventQueue(10, persistPath)
	err = q.Start() // Should not error for empty file
	require.NoError(t, err)
	defer q.Stop()

	// Queue should be empty
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = q.Dequeue(ctx)
	assert.Error(t, err)
}

func TestEventQueue_Persistence_Load_InvalidJson(t *testing.T) {
	testInitLogger(t)
	persistPath := tempPersistPath(t)
	defer os.Remove(persistPath)

	// Create an invalid JSON file
	err := os.WriteFile(persistPath, []byte("[{invalid json"), 0644)
	require.NoError(t, err)

	q := NewEventQueue(10, persistPath)
	err = q.Start()         // Should log an error but start empty
	require.NoError(t, err) // Expecting Start to recover, not return error
	defer q.Stop()

	// Queue should be empty
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = q.Dequeue(ctx)
	assert.Error(t, err)
}

func TestEventQueue_Persistence_Save_EmptyQueue(t *testing.T) {
	testInitLogger(t)
	persistPath := tempPersistPath(t)
	defer os.Remove(persistPath)
	defer os.Remove(persistPath + ".tmp")

	q := NewEventQueue(10, persistPath)
	err := q.Start()
	require.NoError(t, err)
	err = q.Stop() // Stop empty queue, should save empty state
	require.NoError(t, err)

	// Verify file content is an empty JSON array
	data, err := os.ReadFile(persistPath)
	require.NoError(t, err)
	assert.Equal(t, "[]", strings.TrimSpace(string(data)))
}

func TestEventQueue_Persistence_NoPath(t *testing.T) {
	testInitLogger(t)
	// No persist path provided
	q := NewEventQueue(10, "")
	err := q.Start() // Should skip load
	require.NoError(t, err)

	err = q.Enqueue(models.Event{ActionID: "act1"})
	require.NoError(t, err)

	err = q.Stop() // Should skip save
	require.NoError(t, err)
	// No file should have been created
}

// Test potential race condition between Stop and Dequeue
func TestEventQueue_StopDequeueRace(t *testing.T) {
	testInitLogger(t)
	q := NewEventQueue(100, "") // Larger capacity
	q.Start()

	var wg sync.WaitGroup
	ctx := context.Background()
	numItems := 50

	// Start dequeuers
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < numItems/2; i++ {
			_, err := q.Dequeue(ctx)
			if err != nil {
				// Expect "stopped" error eventually, ignore others for simplicity
				if !strings.Contains(err.Error(), "stopped") {
					t.Logf("Dequeuer 1 got unexpected error: %v", err)
				}
				return // Stop dequeuing on error
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < numItems/2; i++ {
			_, err := q.Dequeue(ctx)
			if err != nil {
				if !strings.Contains(err.Error(), "stopped") {
					t.Logf("Dequeuer 2 got unexpected error: %v", err)
				}
				return
			}
		}
	}()

	// Enqueue items
	for i := 0; i < numItems; i++ {
		q.Enqueue(models.Event{ActionID: fmt.Sprintf("action_%d", i)})
	}

	// Stop the queue concurrently
	time.Sleep(10 * time.Millisecond) // Give dequeuers a chance to start
	err := q.Stop()
	require.NoError(t, err)

	wg.Wait() // Wait for dequeuers to finish (they should exit due to stop signal)
}

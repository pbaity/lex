package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid" // Used for generating unique event IDs
	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/pkg/models"
)

const defaultQueueCapacity = 1000

// EventQueue manages the FIFO queue of events waiting for processing.
type EventQueue struct {
	queue       chan models.Event
	capacity    int
	persistPath string
	mu          sync.Mutex // Protects access during persistence operations
	wg          sync.WaitGroup
	stopChan    chan struct{}
}

// NewEventQueue creates and initializes a new event queue.
func NewEventQueue(capacity int, persistPath string) *EventQueue {
	if capacity <= 0 {
		capacity = defaultQueueCapacity
	}
	return &EventQueue{
		queue:       make(chan models.Event, capacity),
		capacity:    capacity,
		persistPath: persistPath,
		stopChan:    make(chan struct{}),
	}
}

// Enqueue adds an event to the queue. Returns an error if the queue is closed.
// Generates a unique ID for the event if it doesn't have one.
func (eq *EventQueue) Enqueue(event models.Event) error {
	if event.ID == "" {
		event.ID = uuid.NewString() // Generate unique ID
	}
	select {
	case eq.queue <- event:
		logger.L().Debug("Event enqueued", "event_id", event.ID, "action_id", event.ActionID, "source_id", event.SourceID)
		return nil
	case <-eq.stopChan:
		return fmt.Errorf("event queue is stopped, cannot enqueue event %s", event.ID)
	}
	// Note: If the channel buffer is full, this will block until space is available
	// or the queue is stopped. Consider adding a timeout or non-blocking option if needed.
}

// Dequeue retrieves the next event from the queue.
// It blocks until an event is available or the context is cancelled.
func (eq *EventQueue) Dequeue(ctx context.Context) (models.Event, error) {
	select {
	case event := <-eq.queue:
		logger.L().Debug("Event dequeued", "event_id", event.ID)
		return event, nil
	case <-ctx.Done():
		return models.Event{}, ctx.Err()
	case <-eq.stopChan:
		// Check if there are still items after stop signal but before channel close
		select {
		case event := <-eq.queue:
			logger.L().Debug("Event dequeued after stop signal", "event_id", event.ID)
			return event, nil
		default:
			return models.Event{}, fmt.Errorf("event queue stopped")
		}
	}
}

// Start initializes the queue, potentially loading from persistence.
func (eq *EventQueue) Start() error {
	if err := eq.loadState(); err != nil {
		// Log error but potentially continue with an empty queue? Or return error?
		// For now, log and continue.
		logger.L().Error("Failed to load queue state, starting empty.", "error", err)
	} else {
		logger.L().Info("Event queue started", "capacity", eq.capacity, "persistence_path", eq.persistPath)
	}
	return nil
}

// Stop signals the queue to stop accepting new events and persists the current state.
func (eq *EventQueue) Stop() error {
	eq.mu.Lock() // Lock to prevent concurrent stop/save operations

	select {
	case <-eq.stopChan:
		// Already stopped
		eq.mu.Unlock()
		return nil
	default:
		// Proceed with stopping
	}

	logger.L().Info("Stopping event queue...")
	close(eq.stopChan) // Signal dequeuers and enqueuers to stop trying indefinitely

	// Wait for any potential long-running operations managed by wg (if any added later)
	eq.wg.Wait()

	// Drain and save remaining items
	err := eq.saveState() // saveState should handle draining internally now

	// Close the channel only after draining and saving is complete.
	// This signals dequeuers that no more items will *ever* come.
	close(eq.queue)
	eq.mu.Unlock() // Unlock after all operations are done

	if err != nil {
		logger.L().Error("Failed to save queue state during stop.", "error", err)
		return fmt.Errorf("failed to save queue state: %w", err)
	}

	logger.L().Info("Event queue stopped successfully.")
	return nil
}

// --- Persistence Logic ---

func (eq *EventQueue) loadState() error {
	eq.mu.Lock()
	defer eq.mu.Unlock()

	if eq.persistPath == "" {
		logger.L().Debug("Queue persistence path not set, skipping load.")
		return nil
	}

	data, err := os.ReadFile(eq.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.L().Info("Queue persistence file not found, starting fresh.", "path", eq.persistPath)
			return nil // Not an error if file doesn't exist yet
		}
		return fmt.Errorf("failed to read queue state file '%s': %w", eq.persistPath, err)
	}

	if len(data) == 0 {
		logger.L().Info("Queue persistence file is empty, starting fresh.", "path", eq.persistPath)
		return nil
	}

	var events []models.Event
	if err := json.Unmarshal(data, &events); err != nil {
		return fmt.Errorf("failed to unmarshal queue state from '%s': %w", eq.persistPath, err)
	}

	count := 0
	for _, event := range events {
		// Use non-blocking enqueue here to avoid deadlock if the queue is somehow full
		// during startup (shouldn't happen with proper Stop logic, but safer).
		select {
		case eq.queue <- event:
			count++
		default:
			logger.L().Error("Failed to enqueue persisted event during load (queue full?)", "event_id", event.ID)
			// Decide whether to return error or just log and skip
			return fmt.Errorf("failed to load event %s, queue likely full", event.ID)
		}
	}

	logger.L().Info("Loaded events from persistence.", "count", count, "path", eq.persistPath)
	return nil
}

func (eq *EventQueue) saveState() error {
	// This function assumes the queue channel is NOT yet closed,
	// but the stopChan IS closed, preventing new items from being enqueued.
	// It also assumes it's called under the mutex lock.

	if eq.persistPath == "" {
		logger.L().Debug("Queue persistence path not set, skipping save.")
		return nil
	}

	// Drain remaining items from the channel
	events := make([]models.Event, 0, len(eq.queue))
DRAIN_LOOP:
	for {
		select {
		case event, ok := <-eq.queue:
			if !ok {
				break DRAIN_LOOP // Channel closed
			}
			events = append(events, event)
		default: // No more items currently in the buffer
			break DRAIN_LOOP
		}
	}

	if len(events) == 0 {
		logger.L().Info("No events in queue to persist.")
		// Optionally remove the persistence file if it exists? Or leave it empty.
		// Let's write an empty array for consistency.
		// return os.Remove(eq.persistPath) // Handle error
	}

	logger.L().Info("Persisting events to disk.", "count", len(events), "path", eq.persistPath)

	data, err := json.MarshalIndent(events, "", "  ") // Use MarshalIndent for readability
	if err != nil {
		return fmt.Errorf("failed to marshal queue state: %w", err)
	}

	// Write atomically: write to temp file, then rename
	tempFile := eq.persistPath + ".tmp"
	err = os.WriteFile(tempFile, data, 0644) // Use appropriate permissions
	if err != nil {
		return fmt.Errorf("failed to write temporary queue state file '%s': %w", tempFile, err)
	}

	err = os.Rename(tempFile, eq.persistPath)
	if err != nil {
		// Attempt to clean up temp file on rename failure
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename temporary queue state file to '%s': %w", eq.persistPath, err)
	}

	logger.L().Info("Successfully persisted queue state.", "count", len(events))
	return nil
}

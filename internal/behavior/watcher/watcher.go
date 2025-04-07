package watcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/internal/retry"
	"github.com/pbaity/lex/pkg/models"
)

// Queue defines the interface required for enqueuing events.
type Queue interface {
	Enqueue(event models.Event) error
}

// Executor defines the interface required for executing actions (watcher scripts).
type Executor interface {
	Execute(ctx context.Context, event models.Event, actionCfg models.ActionConfig) (stdout, stderr string, err error)
}

// WatcherService manages the execution of configured watchers.
type WatcherService struct {
	config         *models.Config
	eventQueue     Queue    // Use interface
	actionExecutor Executor // Use interface
	wg             sync.WaitGroup
	cancelFuncs    map[string]context.CancelFunc // Map watcher ID to its context cancel func
	mu             sync.Mutex                    // Protects cancelFuncs map
}

// NewService creates a new WatcherService.
func NewService(cfg *models.Config, eq Queue, exec Executor) *WatcherService { // Accept interfaces
	return &WatcherService{
		config:         cfg,
		eventQueue:     eq,   // Store interface
		actionExecutor: exec, // Store interface
		cancelFuncs:    make(map[string]context.CancelFunc),
	}
}

// Start initializes and starts goroutines for each configured watcher.
func (s *WatcherService) Start() error {
	l := logger.L()
	if len(s.config.Watchers) == 0 {
		l.Info("No watchers configured.")
		return nil
	}

	l.Info("Starting watcher service...")
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, watcherCfg := range s.config.Watchers {
		// Capture loop variable for closure
		cfg := watcherCfg

		if cfg.Interval.Duration <= 0 {
			l.Error("Watcher configured with invalid interval, skipping.", "watcher_id", cfg.ID, "interval", cfg.Interval.Duration)
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		s.cancelFuncs[cfg.ID] = cancel

		s.wg.Add(1)
		go s.runWatcher(ctx, cfg)
		l.Info("Started watcher", "watcher_id", cfg.ID, "interval", cfg.Interval.Duration.String(), "action_id", cfg.ActionID)
	}
	l.Info("Watcher service started")
	return nil
}

// Stop signals all running watcher goroutines to stop and waits for them.
func (s *WatcherService) Stop() error {
	l := logger.L()
	if len(s.config.Watchers) == 0 {
		l.Info("No watchers configured, skipping stop.")
		return nil
	}

	l.Info("Stopping watcher service...")
	s.mu.Lock()
	for id, cancel := range s.cancelFuncs {
		l.Debug("Cancelling watcher context", "watcher_id", id)
		cancel()
	}
	// Clear the map after cancelling all
	s.cancelFuncs = make(map[string]context.CancelFunc)
	s.mu.Unlock()

	s.wg.Wait() // Wait for all watcher goroutines to finish
	l.Info("Watcher service stopped")
	return nil
}

// runWatcher is the main loop for a single watcher goroutine.
func (s *WatcherService) runWatcher(ctx context.Context, cfg models.WatcherConfig) {
	defer s.wg.Done()
	l := logger.L().With("watcher_id", cfg.ID, "action_id", cfg.ActionID)
	l.Info("Watcher routine started")

	// Ticker for periodic execution
	ticker := time.NewTicker(cfg.Interval.Duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Info("Watcher routine stopping due to context cancellation.")
			return
		case <-ticker.C:
			l.Debug("Watcher triggered by interval")

			// Define the operation to be retried
			operation := func(opCtx context.Context) error {
				// Create a dummy event just to pass context to Execute
				dummyEvent := models.Event{SourceID: cfg.ID, Type: models.EventTypeWatcher}
				// Note: We don't need the action config *here*, only the watcher's script config.

				// Define the watcher's script execution details
				watcherExecutionConfig := models.ActionConfig{
					ID:     fmt.Sprintf("watcher_script_%s", cfg.ID),
					Script: cfg.Script,
					// Watcher scripts typically don't need parameters substituted? Assume not for now.
					Parameters: []models.ActionParameter{}, // Initialize as empty slice
				}

				// Execute the watcher's script using the action executor
				stdout, stderr, execErr := s.actionExecutor.Execute(opCtx, dummyEvent, watcherExecutionConfig)

				// Watcher succeeds if the script exits with code 0 (execErr is nil)
				if execErr != nil {
					// Log the actual stdout/stderr from the watcher script execution
					l.Warn("Watcher script execution failed", "error", execErr, "stdout", stdout, "stderr", stderr)
					return execErr // Return the error to trigger retry
				}

				// Log success, potentially including output if needed at debug level
				l.Info("Watcher script execution succeeded")
				l.Debug("Watcher script output", "stdout", stdout, "stderr", stderr)
				return nil // Success
			}

			// Execute the watcher script with retry logic
			err := retry.Do(ctx, fmt.Sprintf("watcher_script_%s", cfg.ID), cfg.RetryPolicy, operation)

			// If the operation succeeded after retries, enqueue the associated action
			if err == nil {
				l.Info("Watcher condition met (script succeeded), enqueuing action")
				event := models.Event{
					// ID generated by Enqueue
					SourceID:  cfg.ID,
					Type:      models.EventTypeWatcher,
					ActionID:  cfg.ActionID,
					Timestamp: time.Now().UTC(),
					// Parameters could potentially be passed from watcher stdout? For now, none.
					Parameters: make(map[string]string),
				}
				if enqueueErr := s.eventQueue.Enqueue(event); enqueueErr != nil {
					l.Error("Failed to enqueue event from watcher", "error", enqueueErr)
				}
			} else {
				// Log final failure if retries exhausted or context cancelled
				l.Error("Watcher script failed after all retries", "error", err)
			}
		}
	}
}

// findActionConfig is a helper to get the action configuration by ID.
// TODO: Optimize this lookup, perhaps by pre-calculating a map in NewService.
func (s *WatcherService) findActionConfig(actionID string) (*models.ActionConfig, bool) {
	for _, action := range s.config.Actions {
		if action.ID == actionID {
			return &action, true
		}
	}
	return nil, false
}

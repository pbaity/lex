package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	// Need to adjust imports based on moving EventProcessor logic
	"github.com/pbaity/lex/internal/action"
	"github.com/pbaity/lex/internal/behavior/listener"
	"github.com/pbaity/lex/internal/behavior/timer"
	"github.com/pbaity/lex/internal/behavior/watcher"
	"github.com/pbaity/lex/internal/config"
	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/internal/queue"
	"github.com/pbaity/lex/internal/retry"  // Needed for EventProcessor
	"github.com/pbaity/lex/internal/server" // Import the new server package
	"github.com/pbaity/lex/internal/worker"
	"github.com/pbaity/lex/pkg/models"
)

// runForeground contains the main application logic for running Lex in the foreground.
// It's called by the 'start' command's Run function.
func runForeground(configPath string) {
	// --- Configuration Loading ---
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration from '%s': %v\n", configPath, err)
		os.Exit(1)
	}

	// --- Logger Initialization ---
	// Logger might be initialized before this if needed by stop/start,
	// but it's safe to re-init or check if already initialized.
	if err := logger.Init(cfg.Application, nil); err != nil { // Pass nil writer and handle error
		// Use fmt for critical startup errors before logger is confirmed usable
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		// Optionally exit, or continue with default logger if L() handles it
		os.Exit(1)
	}
	log := logger.L()
	log.Info("Lex application running in foreground...")

	// --- PID File Handling (Basic) ---
	// PID file is generally for daemon processes, but can be useful even
	// in foreground to prevent accidental multiple runs or for scripting.
	pidFilePath := cfg.Application.PIDFilePath
	if pidFilePath != "" {
		if _, err := os.Stat(pidFilePath); err == nil {
			pidBytes, errRead := os.ReadFile(pidFilePath)
			if errRead == nil {
				pidStr := strings.TrimSpace(string(pidBytes))
				pid, errConv := strconv.Atoi(pidStr)
				if errConv == nil {
					process, errFind := os.FindProcess(pid)
					if errFind == nil && process.Signal(syscall.Signal(0)) == nil {
						log.Error("PID file exists and process is running. Aborting.", "path", pidFilePath, "pid", pid)
						fmt.Fprintf(os.Stderr, "Error: Process with PID %d found (from %s). Is Lex already running?\n", pid, pidFilePath)
						os.Exit(1)
					}
				}
			}
			log.Warn("Removing stale PID file", "path", pidFilePath)
			_ = os.Remove(pidFilePath)
		}

		currentPid := os.Getpid()
		log.Info("Writing PID file", "path", pidFilePath, "pid", currentPid)
		if err := os.WriteFile(pidFilePath, []byte(fmt.Sprintf("%d", currentPid)), 0644); err != nil {
			log.Error("Failed to write PID file", "error", err)
		}
		defer func() {
			log.Info("Removing PID file on exit", "path", pidFilePath)
			_ = os.Remove(pidFilePath)
		}()
	}

	// --- Service Initialization ---
	log.Debug("Initializing services...")
	actionExecutor := action.NewExecutor()
	eventQueue := queue.NewEventQueue(cfg.Application.MaxConcurrency*2, cfg.Application.QueuePersistPath)
	eventProcessor := NewEventProcessor(cfg, actionExecutor) // Use local NewEventProcessor
	httpServer := server.NewHTTPServer(cfg, eventQueue)      // Initialize shared HTTP server

	// Listener service now needs access to the shared server's mux to register handlers
	listenerService := listener.NewService(cfg, eventQueue, httpServer.Mux()) // Pass Mux
	watcherService := watcher.NewService(cfg, eventQueue, actionExecutor)
	timerService := timer.NewService(cfg, eventQueue)

	workerPool := worker.NewPool(cfg.Application, eventQueue, eventProcessor)
	log.Debug("Services initialized")

	// --- Start Services ---
	log.Info("Starting services...")
	if err := eventQueue.Start(); err != nil {
		log.Error("Failed to start event queue", "error", err)
		os.Exit(1)
	}
	httpServer.Start() // Start the shared HTTP server
	workerPool.Start()
	// Listener service Start() might change - it no longer starts a server, just registers handlers
	if err := listenerService.Start(); err != nil { // Adapt listenerService.Start if needed
		log.Error("Failed to start listener service (register handlers)", "error", err)
		os.Exit(1)
	}
	if err := watcherService.Start(); err != nil {
		log.Error("Failed to start watcher service", "error", err)
		os.Exit(1)
	}
	if err := timerService.Start(); err != nil {
		log.Error("Failed to start timer service", "error", err)
		os.Exit(1)
	}
	log.Info("All services started successfully")

	// --- Signal Handling for Graceful Shutdown ---
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-stopChan
	log.Info("Received shutdown signal", "signal", sig.String())

	// --- Graceful Shutdown ---
	log.Info("Initiating graceful shutdown...")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()

	// Stop order: HTTP server first (accepts new requests), then generators, then workers, then queue
	if err := httpServer.Stop(shutdownCtx); err != nil {
		log.Error("Error stopping HTTP server", "error", err)
	}
	// Listener service Stop() might change/be removed if it doesn't manage the server anymore
	// if err := listenerService.Stop(shutdownCtx); err != nil {
	// 	log.Error("Error stopping listener service", "error", err)
	// }
	if err := watcherService.Stop(); err != nil {
		log.Error("Error stopping watcher service", "error", err)
	}
	if err := timerService.Stop(); err != nil {
		log.Error("Error stopping timer service", "error", err)
	}
	workerPool.Stop()
	if err := eventQueue.Stop(); err != nil {
		log.Error("Error stopping event queue", "error", err)
	}

	log.Info("Lex application shut down gracefully")
}

// --- Event Processor Logic (Moved here) ---

// EventProcessor processes events dequeued by workers.
type EventProcessor struct {
	config   *models.Config
	executor *action.Executor
}

// NewEventProcessor creates a new event processor.
func NewEventProcessor(cfg *models.Config, exec *action.Executor) *EventProcessor {
	return &EventProcessor{
		config:   cfg,
		executor: exec,
	}
}

// Process handles a single event.
func (p *EventProcessor) Process(ctx context.Context, event models.Event) error {
	l := logger.L().With("event_id", event.ID, "action_id", event.ActionID, "source_type", event.Type, "source_id", event.SourceID)
	l.Info("Processing event")

	actionCfg, found := p.findActionConfig(event.ActionID)
	if !found {
		l.Error("Action configuration not found for event")
		return fmt.Errorf("action config '%s' not found", event.ActionID)
	}

	// Determine effective retry policy
	effectiveRetryPolicy := actionCfg.RetryPolicy
	mergedPolicy := retry.MergePolicies(effectiveRetryPolicy, &p.config.Application.DefaultRetry)

	operation := func(opCtx context.Context) error {
		stdout, stderr, err := p.executor.Execute(opCtx, event, *actionCfg)
		if err != nil {
			l.Warn("Action execution attempt failed", "error", err, "stdout", stdout, "stderr", stderr)
			return err
		}
		l.Info("Action execution attempt succeeded")
		return nil
	}

	err := retry.Do(ctx, fmt.Sprintf("action_%s_event_%s", event.ActionID, event.ID), mergedPolicy, operation)
	if err != nil {
		l.Error("Action failed processing after all retries", "error", err)
		return fmt.Errorf("action '%s' failed after retries: %w", event.ActionID, err)
	}

	l.Info("Event processed successfully")
	return nil
}

// findActionConfig helper
func (p *EventProcessor) findActionConfig(actionID string) (*models.ActionConfig, bool) {
	for _, action := range p.config.Actions {
		if action.ID == actionID {
			return &action, true
		}
	}
	return nil, false
}

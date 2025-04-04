package worker

import (
	"context"
	"sync"

	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/internal/queue"
	"github.com/pbaity/lex/pkg/models"
)

// Processor defines the interface for processing a dequeued event.
// This allows decoupling the worker pool from the specific action execution logic.
type Processor interface {
	Process(ctx context.Context, event models.Event) error
}

// Pool manages a pool of worker goroutines that process events from a queue.
type Pool struct {
	config     models.ApplicationSettings
	eventQueue *queue.EventQueue
	processor  Processor
	wg         sync.WaitGroup
	cancelCtx  context.CancelFunc // To signal workers to stop
}

// NewPool creates a new worker pool.
func NewPool(cfg models.ApplicationSettings, eq *queue.EventQueue, proc Processor) *Pool {
	return &Pool{
		config:     cfg,
		eventQueue: eq,
		processor:  proc,
	}
}

// Start launches the worker goroutines.
func (p *Pool) Start() {
	concurrency := p.config.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 1 // Default to at least one worker
		logger.L().Warn("MaxConcurrency not set or invalid, defaulting to 1", "configured_value", p.config.MaxConcurrency)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancelCtx = cancel

	logger.L().Info("Starting worker pool", "concurrency", concurrency)
	p.wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go p.worker(ctx, i)
	}
	logger.L().Info("Worker pool started")
}

// Stop signals all workers to stop and waits for them to finish.
func (p *Pool) Stop() {
	logger.L().Info("Stopping worker pool...")
	if p.cancelCtx != nil {
		p.cancelCtx() // Signal workers to stop by cancelling their context
	}
	p.wg.Wait() // Wait for all worker goroutines to exit
	logger.L().Info("Worker pool stopped")
}

// worker is the main loop for a single worker goroutine.
func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	logger.L().Info("Worker started", "worker_id", id)

	for {
		select {
		case <-ctx.Done():
			logger.L().Info("Worker stopping due to context cancellation", "worker_id", id)
			return
		default:
			// Attempt to dequeue an event. Pass the worker's context.
			event, err := p.eventQueue.Dequeue(ctx)
			if err != nil {
				// Handle dequeue errors. Common errors are context cancellation
				// or queue stopped, which indicate the worker should exit.
				if err == context.Canceled || err == context.DeadlineExceeded {
					logger.L().Info("Worker stopping: context done", "worker_id", id, "error", err)
				} else if err.Error() == "event queue stopped" {
					logger.L().Info("Worker stopping: event queue stopped", "worker_id", id)
				} else {
					logger.L().Error("Worker failed to dequeue event", "worker_id", id, "error", err)
				}
				// In most error cases from Dequeue (context done, queue stopped), we should exit.
				return
			}

			// Process the dequeued event
			l := logger.L().With("worker_id", id, "event_id", event.ID, "action_id", event.ActionID)
			l.Info("Worker processing event")
			if processErr := p.processor.Process(ctx, event); processErr != nil {
				l.Error("Worker failed to process event", "error", processErr)
				// Decide on error handling: retry? dead-letter queue? For now, just log.
			} else {
				l.Info("Worker finished processing event")
			}

			// Check context again after processing, in case stop was requested during processing.
			select {
			case <-ctx.Done():
				logger.L().Info("Worker stopping after processing event due to context cancellation", "worker_id", id)
				return
			default:
				// Continue to next iteration
			}
		}
	}
}

package main

import (
	"context"
	"fmt"

	"github.com/pbaity/lex/internal/action"
	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/internal/retry"
	"github.com/pbaity/lex/pkg/models"
)

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

// Process handles a single event. It finds the corresponding action,
// determines the effective retry policy, and executes the action with retries.
func (p *EventProcessor) Process(ctx context.Context, event models.Event) error {
	l := logger.L().With("event_id", event.ID, "action_id", event.ActionID, "source_type", event.Type, "source_id", event.SourceID)
	l.Info("Processing event")

	// 1. Find the action configuration
	actionCfg, found := p.findActionConfig(event.ActionID)
	if !found {
		l.Error("Action configuration not found for event, cannot process")
		// Consider sending to a dead-letter queue or just logging
		return fmt.Errorf("action config '%s' not found", event.ActionID)
	}

	// 2. Determine the effective retry policy
	// Priority: Event Source (Listener/Timer/Watcher specific) -> Action Specific -> Global Default
	// Note: Event source specific retry policy needs to be looked up based on event.SourceID and event.Type
	// For now, let's simplify and use Action Specific -> Global Default
	// TODO: Implement full retry policy lookup hierarchy
	effectiveRetryPolicy := actionCfg.RetryPolicy // Start with action-specific policy
	// MergePolicies handles nil specific policy correctly
	mergedPolicy := retry.MergePolicies(effectiveRetryPolicy, &p.config.Application.DefaultRetry) // Use exported name
	l.Debug("Determined effective retry policy", "max_retries", *mergedPolicy.MaxRetries, "delay", *mergedPolicy.Delay, "backoff", *mergedPolicy.BackoffFactor)

	// 3. Define the operation to be retried (action execution)
	operation := func(opCtx context.Context) error {
		// Execute the action associated with the event
		stdout, stderr, err := p.executor.Execute(opCtx, event, *actionCfg)
		if err != nil {
			// Log includes details from executor, just return the error
			l.Warn("Action execution attempt failed", "error", err, "stdout", stdout, "stderr", stderr)
			return err // Propagate error to retry logic
		}
		l.Info("Action execution attempt succeeded")
		return nil // Success
	}

	// 4. Execute the operation with retry logic
	err := retry.Do(ctx, fmt.Sprintf("action_%s_event_%s", event.ActionID, event.ID), mergedPolicy, operation)
	if err != nil {
		l.Error("Action failed processing after all retries", "error", err)
		// Event processing failed ultimately
		return fmt.Errorf("action '%s' failed after retries: %w", event.ActionID, err)
	}

	l.Info("Event processed successfully")
	return nil
}

// findActionConfig is a helper to get the action configuration by ID.
// Duplicated from watcher/timer service - consider moving to a shared utility or config helper.
func (p *EventProcessor) findActionConfig(actionID string) (*models.ActionConfig, bool) {
	for _, action := range p.config.Actions {
		if action.ID == actionID {
			return &action, true
		}
	}
	return nil, false
}

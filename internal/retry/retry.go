package retry

import (
	"context"
	"time"

	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/pkg/models"
)

// Default retry constants
const (
	DefaultMaxRetries    = 10
	DefaultDelaySeconds  = 1.0
	DefaultBackoffFactor = 2.0
)

// DefaultRetryPolicy provides sensible defaults if no policy is specified, using the constants.
var DefaultRetryPolicy = models.RetryPolicy{
	MaxRetries:    intPtr(DefaultMaxRetries),
	Delay:         float64Ptr(DefaultDelaySeconds),
	BackoffFactor: float64Ptr(DefaultBackoffFactor),
}

// Operation is a function that performs an action and returns an error if it fails.
type Operation func(ctx context.Context) error

// Do executes the provided operation, retrying according to the policy if it fails.
// It merges the provided policy with the default policy for missing values.
func Do(ctx context.Context, operationName string, policy *models.RetryPolicy, op Operation) error {
	// Check context before doing anything else
	if err := ctx.Err(); err != nil {
		logger.L().Warn("Operation cancelled before first attempt due to context error", "operation", operationName, "error", err)
		return err // Return context error immediately
	}

	effectivePolicy := MergePolicies(policy, &DefaultRetryPolicy) // Use exported name
	l := logger.L().With("operation", operationName)

	maxRetries := *effectivePolicy.MaxRetries
	currentDelay := time.Duration(*effectivePolicy.Delay * float64(time.Second))
	backoffFactor := *effectivePolicy.BackoffFactor

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		l.Debug("Executing operation", "attempt", attempt+1, "max_attempts", maxRetries+1)
		lastErr = op(ctx)
		if lastErr == nil {
			if attempt > 0 {
				l.Info("Operation succeeded after retry", "attempt", attempt+1)
			} else {
				l.Debug("Operation succeeded on first attempt")
			}
			return nil // Success
		}

		l.Warn("Operation failed", "attempt", attempt+1, "max_attempts", maxRetries+1, "error", lastErr)

		// Check if it was the last attempt
		if attempt == maxRetries {
			l.Error("Operation failed after exhausting all retries", "error", lastErr)
			break // Exit loop, return last error
		}

		// Calculate next delay
		delayDuration := currentDelay
		l.Info("Scheduling retry", "delay", delayDuration.String())

		// Wait for the delay duration, respecting context cancellation
		select {
		case <-time.After(delayDuration):
			// Continue to next attempt
			currentDelay = time.Duration(float64(currentDelay) * backoffFactor)
			// Optional: Add jitter here to prevent thundering herd
			// Optional: Add max delay cap here
		case <-ctx.Done():
			l.Warn("Retry cancelled due to context cancellation", "error", ctx.Err())
			return ctx.Err() // Return context error immediately
		}
	}

	return lastErr // Return the last error encountered after all retries
}

// MergePolicies combines a specific policy with a default policy.
// Specific values override defaults. Pointers are used to detect unset fields.
// Uses defined constants for defaults if policies or fields are nil.
func MergePolicies(specific, defaultP *models.RetryPolicy) *models.RetryPolicy {
	// Use the exported DefaultRetryPolicy as the base for defaults if defaultP is nil
	if defaultP == nil {
		// Return a copy of the package-level DefaultRetryPolicy
		// This already uses the constants.
		dpCopy := DefaultRetryPolicy
		defaultP = &dpCopy
	}

	// Handle nil specific policy - return a copy of the (potentially adjusted) default
	if specific == nil {
		dpCopy := *defaultP
		// Ensure the copy has defaults applied if the provided default was incomplete
		if dpCopy.MaxRetries == nil {
			dpCopy.MaxRetries = intPtr(DefaultMaxRetries)
		}
		if dpCopy.Delay == nil {
			dpCopy.Delay = float64Ptr(DefaultDelaySeconds)
		}
		if dpCopy.BackoffFactor == nil {
			dpCopy.BackoffFactor = float64Ptr(DefaultBackoffFactor)
		}
		return &dpCopy
	}

	// Both specific and defaultP are non-nil (or defaultP was set to DefaultRetryPolicy)
	merged := &models.RetryPolicy{}

	// Merge MaxRetries
	if specific.MaxRetries != nil {
		merged.MaxRetries = specific.MaxRetries
	} else if defaultP.MaxRetries != nil {
		merged.MaxRetries = defaultP.MaxRetries
	} else {
		merged.MaxRetries = intPtr(DefaultMaxRetries) // Fallback to constant
	}

	// Merge Delay
	if specific.Delay != nil {
		merged.Delay = specific.Delay
	} else if defaultP.Delay != nil {
		merged.Delay = defaultP.Delay
	} else {
		merged.Delay = float64Ptr(DefaultDelaySeconds) // Fallback to constant
	}

	// Merge BackoffFactor
	if specific.BackoffFactor != nil {
		merged.BackoffFactor = specific.BackoffFactor
	} else if defaultP.BackoffFactor != nil {
		merged.BackoffFactor = defaultP.BackoffFactor
	} else {
		merged.BackoffFactor = float64Ptr(DefaultBackoffFactor) // Fallback to constant
	}

	// Final check: Ensure non-nil pointers using constants as final defaults
	// (Should be redundant given the logic above, but safe)
	if merged.MaxRetries == nil {
		merged.MaxRetries = intPtr(DefaultMaxRetries)
	}
	if merged.Delay == nil {
		merged.Delay = float64Ptr(DefaultDelaySeconds)
	}
	if merged.BackoffFactor == nil {
		merged.BackoffFactor = float64Ptr(DefaultBackoffFactor)
	}

	return merged
}

// Helper functions to create pointers for default values
func intPtr(i int) *int             { return &i }
func float64Ptr(f float64) *float64 { return &f }

// --- Potential Enhancements ---
// - Add jitter to delays
// - Add a maximum delay cap
// - Allow specifying which errors are retryable

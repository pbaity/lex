package retry

import (
	"context"
	"time"

	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/pkg/models"
)

// DefaultRetryPolicy provides sensible defaults if no policy is specified.
var DefaultRetryPolicy = models.RetryPolicy{
	MaxRetries:    intPtr(3),
	Delay:         float64Ptr(1.0), // 1 second
	BackoffFactor: float64Ptr(2.0), // Exponential backoff
}

// Operation is a function that performs an action and returns an error if it fails.
type Operation func(ctx context.Context) error

// Do executes the provided operation, retrying according to the policy if it fails.
// It merges the provided policy with the default policy for missing values.
func Do(ctx context.Context, operationName string, policy *models.RetryPolicy, op Operation) error {
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
func MergePolicies(specific, defaultPolicy *models.RetryPolicy) *models.RetryPolicy {
	merged := &models.RetryPolicy{}

	if specific != nil && specific.MaxRetries != nil {
		merged.MaxRetries = specific.MaxRetries
	} else {
		merged.MaxRetries = defaultPolicy.MaxRetries
	}

	if specific != nil && specific.Delay != nil {
		merged.Delay = specific.Delay
	} else {
		merged.Delay = defaultPolicy.Delay
	}

	if specific != nil && specific.BackoffFactor != nil {
		merged.BackoffFactor = specific.BackoffFactor
	} else {
		merged.BackoffFactor = defaultPolicy.BackoffFactor
	}

	// Ensure non-nil pointers for easier use later, defaulting missing values
	if merged.MaxRetries == nil {
		merged.MaxRetries = intPtr(0) // Default to 0 retries if somehow still nil
	}
	if merged.Delay == nil {
		merged.Delay = float64Ptr(0.0) // Default to 0 delay
	}
	if merged.BackoffFactor == nil {
		merged.BackoffFactor = float64Ptr(1.0) // Default to no backoff
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

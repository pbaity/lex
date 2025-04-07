package retry

import (
	"context"
	"fmt"
	"io" // Add io import
	"testing"
	"time"

	"github.com/pbaity/lex/internal/logger" // Add logger import
	"github.com/pbaity/lex/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require" // Add require import
)

// testInitLogger initializes the logger for test execution, discarding output.
func testInitLogger(t *testing.T) {
	t.Helper()
	// Use basic settings, output is discarded
	settings := models.ApplicationSettings{LogLevel: "error", LogFormat: "text"}
	err := logger.Init(settings, io.Discard)
	require.NoError(t, err, "Failed to initialize logger for test")
}

// Helper to create pointers for test values
func ptr[T any](v T) *T {
	return &v
}

func TestMergePolicies(t *testing.T) {
	defaultPolicy := &models.RetryPolicy{
		MaxRetries:    ptr(5),
		Delay:         ptr(1.0),
		BackoffFactor: ptr(2.0),
	}
	specificPolicy := &models.RetryPolicy{
		MaxRetries: ptr(3),
		Delay:      ptr(0.5),
		// BackoffFactor intentionally nil
	}
	specificPolicyOnlyFactor := &models.RetryPolicy{
		BackoffFactor: ptr(1.5),
	}
	emptySpecificPolicy := &models.RetryPolicy{} // All fields nil

	tests := []struct {
		name            string
		specific        *models.RetryPolicy
		defaultP        *models.RetryPolicy
		expectedRetries int
		expectedDelay   float64
		expectedFactor  float64
	}{
		{
			name:            "Specific overrides Default",
			specific:        specificPolicy,
			defaultP:        defaultPolicy,
			expectedRetries: 3,   // From specific
			expectedDelay:   0.5, // From specific
			expectedFactor:  2.0, // From default (specific was nil)
		},
		{
			name:            "Default used when Specific is nil",
			specific:        nil,
			defaultP:        defaultPolicy,
			expectedRetries: 5,
			expectedDelay:   1.0,
			expectedFactor:  2.0,
		},
		{
			name:            "Default used when Specific fields are nil",
			specific:        emptySpecificPolicy,
			defaultP:        defaultPolicy,
			expectedRetries: 5,   // From default
			expectedDelay:   1.0, // From default
			expectedFactor:  2.0, // From default
		},
		{
			name:            "Specific overrides only one field",
			specific:        specificPolicyOnlyFactor,
			defaultP:        defaultPolicy,
			expectedRetries: 5,   // From default
			expectedDelay:   1.0, // From default
			expectedFactor:  1.5, // From specific
		},
		{
			name:            "Nil Specific and Nil Default",
			specific:        nil,
			defaultP:        nil,
			expectedRetries: DefaultMaxRetries,    // Expect default constants
			expectedDelay:   DefaultDelaySeconds,  // Expect default constants
			expectedFactor:  DefaultBackoffFactor, // Expect default constants
		},
		{
			name:            "Empty Specific and Nil Default",
			specific:        emptySpecificPolicy,
			defaultP:        nil,
			expectedRetries: DefaultMaxRetries,    // Expect default constants
			expectedDelay:   DefaultDelaySeconds,  // Expect default constants
			expectedFactor:  DefaultBackoffFactor, // Expect default constants
		},
		{
			name:            "Nil Specific and Empty Default",
			specific:        nil,
			defaultP:        &models.RetryPolicy{}, // Empty default policy
			expectedRetries: DefaultMaxRetries,     // Expect default constants (MergePolicies applies them)
			expectedDelay:   DefaultDelaySeconds,   // Expect default constants
			expectedFactor:  DefaultBackoffFactor,  // Expect default constants
		},
		{
			name:            "Specific zero values override Default",
			specific:        &models.RetryPolicy{MaxRetries: ptr(0), Delay: ptr(0.0), BackoffFactor: ptr(1.0)}, // Use valid zero/min values
			defaultP:        defaultPolicy,
			expectedRetries: 0,
			expectedDelay:   0.0,
			expectedFactor:  1.0, // Factor >= 1.0 is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := MergePolicies(tt.specific, tt.defaultP)

			// Check merged values, handling potential nil pointers in the result
			if merged.MaxRetries != nil {
				assert.Equal(t, tt.expectedRetries, *merged.MaxRetries)
			} else {
				assert.Equal(t, tt.expectedRetries, 0) // Expect 0 if nil
			}

			if merged.Delay != nil {
				assert.Equal(t, tt.expectedDelay, *merged.Delay)
			} else {
				assert.Equal(t, tt.expectedDelay, 0.0) // Expect 0.0 if nil
			}

			if merged.BackoffFactor != nil {
				assert.Equal(t, tt.expectedFactor, *merged.BackoffFactor)
			} else {
				// If merged.BackoffFactor is nil, the expected factor should be the default constant.
				assert.Equal(t, tt.expectedFactor, DefaultBackoffFactor)
			}
			// Removed duplicate else block
		})
	}
}

// --- Tests for Do function ---

// Mock operation for testing Do
type mockOperation struct {
	attemptsNeeded int // How many times it needs to be called to succeed
	callCount      int
	failForever    bool // If true, never succeeds
	lastContext    context.Context
}

func (m *mockOperation) execute(ctx context.Context) error {
	m.callCount++
	m.lastContext = ctx // Store context for cancellation checks
	if m.failForever {
		return fmt.Errorf("operation failed permanently (call %d)", m.callCount)
	}
	if m.callCount >= m.attemptsNeeded {
		return nil // Success
	}
	return fmt.Errorf("operation failed temporarily (call %d)", m.callCount)
}

func TestDo_SuccessFirstTry(t *testing.T) {
	testInitLogger(t) // Initialize logger
	op := &mockOperation{attemptsNeeded: 1}
	policy := &models.RetryPolicy{MaxRetries: ptr(3), Delay: ptr(0.01)} // Small delay for test speed
	ctx := context.Background()

	err := Do(ctx, "test_success_first", policy, op.execute)

	assert.NoError(t, err)
	assert.Equal(t, 1, op.callCount)
}

func TestDo_SuccessAfterRetries(t *testing.T) {
	testInitLogger(t) // Initialize logger
	op := &mockOperation{attemptsNeeded: 3}
	policy := &models.RetryPolicy{MaxRetries: ptr(5), Delay: ptr(0.01), BackoffFactor: ptr(1.0)} // No backoff
	ctx := context.Background()

	err := Do(ctx, "test_success_retry", policy, op.execute)

	assert.NoError(t, err)
	assert.Equal(t, 3, op.callCount)
}

func TestDo_FailureAfterMaxRetries(t *testing.T) {
	testInitLogger(t) // Initialize logger
	op := &mockOperation{failForever: true}
	policy := &models.RetryPolicy{MaxRetries: ptr(2), Delay: ptr(0.01), BackoffFactor: ptr(1.0)}
	ctx := context.Background()

	startTime := time.Now()
	err := Do(ctx, "test_fail_retry", policy, op.execute)
	duration := time.Since(startTime)

	assert.Error(t, err)
	// Check that the returned error is the error from the last attempt
	assert.Contains(t, err.Error(), "operation failed permanently (call 3)")
	// Do not assert the specific wrapper message "operation failed after X attempts" as Do doesn't add it.
	assert.Equal(t, 3, op.callCount) // Initial call + 2 retries = 3 calls
	// Check that some delay occurred (2 delays expected)
	assert.GreaterOrEqual(t, duration, 2*time.Duration(0.01*float64(time.Second)))
}

func TestDo_ZeroRetries(t *testing.T) {
	testInitLogger(t) // Initialize logger
	op := &mockOperation{failForever: true}
	policy := &models.RetryPolicy{MaxRetries: ptr(0), Delay: ptr(0.01)}
	ctx := context.Background()

	err := Do(ctx, "test_zero_retry", policy, op.execute)

	assert.Error(t, err)
	// Check that the returned error is the error from the last attempt
	assert.Contains(t, err.Error(), "operation failed permanently (call 1)")
	// Do not assert the specific wrapper message "operation failed after X attempts" as Do doesn't add it.
	assert.Equal(t, 1, op.callCount) // Only the initial call
}

func TestDo_ContextCancellationDuringWait(t *testing.T) {
	testInitLogger(t) // Initialize logger
	op := &mockOperation{failForever: true}
	// Long delay to ensure cancellation happens during wait
	policy := &models.RetryPolicy{MaxRetries: ptr(3), Delay: ptr(1.0)}
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context shortly after the first attempt fails
	go func() {
		time.Sleep(100 * time.Millisecond) // Wait a bit longer than the first potential delay starts
		cancel()
	}()

	err := Do(ctx, "test_cancel_wait", policy, op.execute)

	assert.Error(t, err)
	// The exact error might vary slightly depending on timing,
	// but it should indicate context cancellation.
	assert.Contains(t, err.Error(), context.Canceled.Error())
	assert.Equal(t, 1, op.callCount) // Should only make the first call
	// Check the context passed to the operation was indeed cancelled
	assert.ErrorIs(t, op.lastContext.Err(), context.Canceled)
}

func TestDo_ContextCancellationBeforeFirstTry(t *testing.T) {
	testInitLogger(t) // Initialize logger
	op := &mockOperation{failForever: true}
	policy := &models.RetryPolicy{MaxRetries: ptr(3), Delay: ptr(0.01)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := Do(ctx, "test_cancel_before", policy, op.execute)

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled) // Should return context error directly
	assert.Equal(t, 0, op.callCount)         // Should not even attempt the operation
}

func TestDo_NilPolicy(t *testing.T) {
	testInitLogger(t) // Initialize logger
	// Modify op to succeed after 1 retry to avoid timeout with default policy
	op := &mockOperation{attemptsNeeded: 2}
	ctx := context.Background()

	// Nil policy should use DefaultRetryPolicy (10 retries, 1s delay, 2.0 factor)
	// Operation should succeed on the 2nd attempt (1st retry)
	err := Do(ctx, "test_nil_policy", nil, op.execute)

	// Assert success after the first retry (2nd call)
	// Removed the incorrect assert.Error(t, err) line
	assert.NoError(t, err)
	assert.Equal(t, 2, op.callCount)
}

func TestDo_BackoffFactor(t *testing.T) {
	testInitLogger(t) // Initialize logger
	op := &mockOperation{failForever: true}
	// Use a noticeable delay and backoff factor
	policy := &models.RetryPolicy{MaxRetries: ptr(2), Delay: ptr(0.1), BackoffFactor: ptr(2.0)}
	ctx := context.Background()

	startTime := time.Now()
	err := Do(ctx, "test_backoff", policy, op.execute)
	duration := time.Since(startTime)

	assert.Error(t, err)
	assert.Equal(t, 3, op.callCount) // Initial + 2 retries

	// Expected delays: 0.1s, 0.2s. Total delay >= 0.3s
	expectedMinDuration := time.Duration((0.1 + 0.2) * float64(time.Second))
	assert.GreaterOrEqual(t, duration, expectedMinDuration)

	// Allow some buffer for execution time
	expectedMaxDuration := time.Duration((0.1 + 0.2 + 0.1) * float64(time.Second)) // Add buffer
	assert.LessOrEqual(t, duration, expectedMaxDuration)
}

package config

import (
	"testing"
	"time"

	"github.com/pbaity/lex/pkg/models"
	"github.com/stretchr/testify/assert"
)

// Helper to create a basic valid config for modification in tests
func createValidTestConfig() *models.Config {
	retryDelay := 1.0
	retryFactor := 2.0
	retryCount := 3
	rateLimit := 10.0
	burst := 20

	return &models.Config{
		Application: models.ApplicationSettings{
			LogLevel:  "info",
			LogFormat: "text",
			DefaultRetry: models.RetryPolicy{
				MaxRetries:    &retryCount,
				Delay:         &retryDelay,
				BackoffFactor: &retryFactor,
			},
			MaxConcurrency: 10,
		},
		Listeners: []models.ListenerConfig{
			{ID: "listener1", Path: "/l1", ActionID: "action1", RateLimit: &rateLimit, Burst: &burst},
		},
		Watchers: []models.WatcherConfig{
			{ID: "watcher1", Script: "s1", ActionID: "action2", Interval: models.Duration{Duration: 1 * time.Minute}},
		},
		Timers: []models.TimerConfig{
			{ID: "timer1", ActionID: "action1", Interval: models.Duration{Duration: 1 * time.Hour}},
		},
		Actions: []models.ActionConfig{
			{ID: "action1", Script: "script1"},
			{ID: "action2", Script: "script2"},
		},
	}
}

func TestValidateConfig_Valid(t *testing.T) {
	cfg := createValidTestConfig()
	err := ValidateConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateConfig_NilConfig(t *testing.T) {
	err := ValidateConfig(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestValidateConfig_DuplicateIDs(t *testing.T) {
	testCases := []struct {
		name        string
		modify      func(cfg *models.Config)
		expectedErr string
	}{
		{
			name: "Duplicate Listener ID",
			modify: func(cfg *models.Config) {
				cfg.Listeners = append(cfg.Listeners, models.ListenerConfig{ID: "listener1", Path: "/l2", ActionID: "action1"})
			},
			expectedErr: "duplicate listener ID found: listener1",
		},
		{
			name: "Duplicate Watcher ID",
			modify: func(cfg *models.Config) {
				cfg.Watchers = append(cfg.Watchers, models.WatcherConfig{ID: "watcher1", Script: "s2", ActionID: "action1", Interval: models.Duration{Duration: 1 * time.Minute}})
			},
			expectedErr: "duplicate watcher ID found: watcher1",
		},
		{
			name: "Duplicate Timer ID",
			modify: func(cfg *models.Config) {
				cfg.Timers = append(cfg.Timers, models.TimerConfig{ID: "timer1", ActionID: "action2", Interval: models.Duration{Duration: 1 * time.Hour}})
			},
			expectedErr: "duplicate timer ID found: timer1",
		},
		{
			name: "Duplicate Action ID",
			modify: func(cfg *models.Config) {
				cfg.Actions = append(cfg.Actions, models.ActionConfig{ID: "action1", Script: "script3"})
			},
			expectedErr: "duplicate action ID found: action1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createValidTestConfig()
			tc.modify(cfg)
			err := ValidateConfig(cfg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedErr)
		})
	}
}

func TestValidateConfig_MissingActionReference(t *testing.T) {
	testCases := []struct {
		name        string
		modify      func(cfg *models.Config)
		expectedErr string
	}{
		{
			name: "Listener references missing action",
			modify: func(cfg *models.Config) {
				cfg.Listeners[0].ActionID = "nonexistent_action"
			},
			expectedErr: "invalid listener config at index 0 (ID: listener1): action ID 'nonexistent_action' not found in defined actions", // Updated expected error
		},
		{
			name: "Watcher references missing action",
			modify: func(cfg *models.Config) {
				cfg.Watchers[0].ActionID = "nonexistent_action"
			},
			expectedErr: "invalid watcher config at index 0 (ID: watcher1): action ID 'nonexistent_action' not found in defined actions", // Updated expected error
		},
		{
			name: "Timer references missing action",
			modify: func(cfg *models.Config) {
				cfg.Timers[0].ActionID = "nonexistent_action"
			},
			expectedErr: "invalid timer config at index 0 (ID: timer1): action ID 'nonexistent_action' not found in defined actions", // Updated expected error
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createValidTestConfig()
			tc.modify(cfg)
			err := ValidateConfig(cfg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedErr)
		})
	}
}

func TestValidateConfig_MissingRequiredFields(t *testing.T) {
	testCases := []struct {
		name        string
		modify      func(cfg *models.Config)
		expectedErr string
	}{
		{
			name:        "Listener missing ID",
			modify:      func(cfg *models.Config) { cfg.Listeners[0].ID = "" },
			expectedErr: "invalid listener config at index 0 (ID: ): listener at index 0 must have an ID", // Updated expected error
		},
		{
			name:        "Listener missing Path",
			modify:      func(cfg *models.Config) { cfg.Listeners[0].Path = "" },
			expectedErr: "invalid listener config at index 0 (ID: listener1): path cannot be empty", // Updated expected error
		},
		{
			name:        "Listener missing ActionID",
			modify:      func(cfg *models.Config) { cfg.Listeners[0].ActionID = "" },
			expectedErr: "invalid listener config at index 0 (ID: listener1): action cannot be empty", // Updated expected error
		},
		{
			name:        "Watcher missing ID",
			modify:      func(cfg *models.Config) { cfg.Watchers[0].ID = "" },
			expectedErr: "invalid watcher config at index 0 (ID: ): watcher at index 0 must have an ID", // Updated expected error
		},
		{
			name:        "Watcher missing Script",
			modify:      func(cfg *models.Config) { cfg.Watchers[0].Script = "" },
			expectedErr: "invalid watcher config at index 0 (ID: watcher1): script cannot be empty", // Updated expected error
		},
		{
			name:        "Watcher missing ActionID",
			modify:      func(cfg *models.Config) { cfg.Watchers[0].ActionID = "" },
			expectedErr: "invalid watcher config at index 0 (ID: watcher1): action cannot be empty", // Updated expected error
		},
		{
			name:        "Watcher missing Interval",
			modify:      func(cfg *models.Config) { cfg.Watchers[0].Interval = models.Duration{} },
			expectedErr: "invalid watcher config at index 0 (ID: watcher1): interval must be a positive duration", // Updated expected error
		},
		{
			name:        "Timer missing ID",
			modify:      func(cfg *models.Config) { cfg.Timers[0].ID = "" },
			expectedErr: "invalid timer config at index 0 (ID: ): timer at index 0 must have an ID", // Updated expected error
		},
		{
			name:        "Timer missing ActionID",
			modify:      func(cfg *models.Config) { cfg.Timers[0].ActionID = "" },
			expectedErr: "invalid timer config at index 0 (ID: timer1): action cannot be empty", // Updated expected error
		},
		{
			name:        "Timer missing Interval",
			modify:      func(cfg *models.Config) { cfg.Timers[0].Interval = models.Duration{} },
			expectedErr: "invalid timer config at index 0 (ID: timer1): interval must be a positive duration", // Updated expected error
		},
		{
			name:        "Action missing ID",
			modify:      func(cfg *models.Config) { cfg.Actions[0].ID = "" },
			expectedErr: "invalid action config at index 0 (ID: ): action at index 0 must have an ID", // Updated expected error
		},
		{
			name:        "Action missing Script",
			modify:      func(cfg *models.Config) { cfg.Actions[0].Script = "" },
			expectedErr: "invalid action config at index 0 (ID: action1): script cannot be empty", // Updated expected error
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createValidTestConfig()
			tc.modify(cfg)
			err := ValidateConfig(cfg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedErr)
		})
	}
}

func TestValidateConfig_InvalidValues(t *testing.T) {
	negRetry := -1
	negDelay := -1.0
	negFactor := -1.0
	zeroFactor := 0.0
	negRateLimit := -10.0
	negBurst := -5

	testCases := []struct {
		name        string
		modify      func(cfg *models.Config)
		expectedErr string
	}{
		{
			name:        "Invalid Log Level",
			modify:      func(cfg *models.Config) { cfg.Application.LogLevel = "trace" },
			expectedErr: "invalid application settings: invalid log_level: trace", // Updated expected error
		},
		{
			name:        "Invalid Log Format",
			modify:      func(cfg *models.Config) { cfg.Application.LogFormat = "xml" },
			expectedErr: "invalid application settings: invalid log_format: xml", // Updated expected error
		},
		{
			name:        "Invalid Default MaxRetries",
			modify:      func(cfg *models.Config) { cfg.Application.DefaultRetry.MaxRetries = &negRetry },
			expectedErr: "invalid application settings: default_retry: max_retries cannot be negative", // Updated expected error
		},
		{
			name:        "Invalid Default Delay",
			modify:      func(cfg *models.Config) { cfg.Application.DefaultRetry.Delay = &negDelay },
			expectedErr: "invalid application settings: default_retry: delay cannot be negative", // Updated expected error
		},
		{
			name:        "Invalid Default BackoffFactor",
			modify:      func(cfg *models.Config) { cfg.Application.DefaultRetry.BackoffFactor = &negFactor },
			expectedErr: "invalid application settings: default_retry: backoff_factor cannot be less than 1.0", // Updated expected error
		},
		{
			name:        "Zero Default BackoffFactor", // Zero is invalid, must be >= 1
			modify:      func(cfg *models.Config) { cfg.Application.DefaultRetry.BackoffFactor = &zeroFactor },
			expectedErr: "invalid application settings: default_retry: backoff_factor cannot be less than 1.0", // Updated expected error
		},
		{
			name:        "Invalid Listener RateLimit",
			modify:      func(cfg *models.Config) { cfg.Listeners[0].RateLimit = &negRateLimit },
			expectedErr: "invalid listener config at index 0 (ID: listener1): rate_limit must be positive if set", // Updated expected error
		},
		{
			name:        "Invalid Listener Burst",
			modify:      func(cfg *models.Config) { cfg.Listeners[0].Burst = &negBurst },
			expectedErr: "invalid listener config at index 0 (ID: listener1): burst must be positive if set", // Updated expected error
		},
		// Add similar checks for specific retry policies on listeners, watchers, timers, actions
		{
			name: "Watcher Invalid Interval",
			modify: func(cfg *models.Config) {
				cfg.Watchers[0].Interval = models.Duration{Duration: -5 * time.Second}
			},
			expectedErr: "invalid watcher config at index 0 (ID: watcher1): interval must be a positive duration", // Updated expected error
		},
		{
			name: "Timer Invalid Interval",
			modify: func(cfg *models.Config) {
				cfg.Timers[0].Interval = models.Duration{Duration: 0} // Zero interval is invalid
			},
			expectedErr: "invalid timer config at index 0 (ID: timer1): interval must be a positive duration", // Updated expected error
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createValidTestConfig()
			tc.modify(cfg)
			err := ValidateConfig(cfg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedErr)
		})
	}
}

// Test cases for when specific retry policies have invalid values
func TestValidateConfig_InvalidSpecificRetryPolicies(t *testing.T) {
	negRetry := -1
	negDelay := -0.5
	invalidFactor := 0.5 // Invalid, must be >= 1.0
	validDelay := 1.0
	validRetries := 1

	tests := []struct {
		name        string
		policy      *models.RetryPolicy
		expectedErr string
	}{
		{
			name: "Invalid MaxRetries",
			policy: &models.RetryPolicy{
				MaxRetries: &negRetry, // Invalid
			},
			expectedErr: "invalid listener config at index 0 (ID: listener1): retry_policy: max_retries cannot be negative",
		},
		{
			name: "Invalid Delay",
			policy: &models.RetryPolicy{
				MaxRetries: &validRetries, // Valid
				Delay:      &negDelay,     // Invalid
			},
			expectedErr: "invalid listener config at index 0 (ID: listener1): retry_policy: delay cannot be negative",
		},
		{
			name: "Invalid BackoffFactor",
			policy: &models.RetryPolicy{
				MaxRetries:    &validRetries,  // Valid
				Delay:         &validDelay,    // Valid
				BackoffFactor: &invalidFactor, // Invalid
			},
			expectedErr: "invalid listener config at index 0 (ID: listener1): retry_policy: backoff_factor cannot be less than 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createValidTestConfig()
			// Apply the specific invalid policy to the first listener
			cfg.Listeners[0].RetryPolicy = tt.policy
			err := ValidateConfig(cfg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

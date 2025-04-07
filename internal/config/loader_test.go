package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pbaity/lex/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a temporary config file
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(filePath, []byte(content), 0644)
	require.NoError(t, err, "Failed to write temp config file")
	return filePath
}

func TestLoadConfig_Success(t *testing.T) {
	validYAML := `
application:
  log_level: debug
  log_format: json
  max_concurrency: 10
  queue_persist_path: /tmp/lex_queue.json
  pid_file_path: /var/run/lex.pid
  default_retry:
    max_retries: 3
    delay: 1.5
    backoff_factor: 2.0
listeners:
  - id: listener1
    path: /webhook1
    action: action1
    auth_token: secret1
    rate_limit: 100.5
    burst: 200
    retry_policy:
      max_retries: 2
      delay: 0.5
watchers:
  - id: watcher1
    script: check_something.sh
    action: action2
    interval: "5m"
timers:
  - id: timer1
    action: action3
    interval: "24h"
actions:
  - id: action1
    script: do_something.sh
  - id: action2
    script: do_another_thing.sh
  - id: action3
    script: final_task.sh
`
	filePath := createTempConfigFile(t, validYAML)
	cfg, err := LoadConfig(filePath)

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Assert Application Settings
	assert.Equal(t, "debug", cfg.Application.LogLevel)
	assert.Equal(t, "json", cfg.Application.LogFormat)
	assert.Equal(t, 10, cfg.Application.MaxConcurrency)
	assert.Equal(t, "/tmp/lex_queue.json", cfg.Application.QueuePersistPath)
	assert.Equal(t, "/var/run/lex.pid", cfg.Application.PIDFilePath)
	require.NotNil(t, cfg.Application.DefaultRetry.MaxRetries)
	assert.Equal(t, 3, *cfg.Application.DefaultRetry.MaxRetries)
	require.NotNil(t, cfg.Application.DefaultRetry.Delay)
	assert.Equal(t, 1.5, *cfg.Application.DefaultRetry.Delay)
	require.NotNil(t, cfg.Application.DefaultRetry.BackoffFactor)
	assert.Equal(t, 2.0, *cfg.Application.DefaultRetry.BackoffFactor)

	// Assert Listeners
	require.Len(t, cfg.Listeners, 1)
	listener := cfg.Listeners[0]
	assert.Equal(t, "listener1", listener.ID)
	assert.Equal(t, "/webhook1", listener.Path)
	assert.Equal(t, "action1", listener.ActionID)
	assert.Equal(t, "secret1", listener.AuthToken)
	require.NotNil(t, listener.RateLimit)
	assert.Equal(t, 100.5, *listener.RateLimit)
	require.NotNil(t, listener.Burst)
	assert.Equal(t, 200, *listener.Burst)
	require.NotNil(t, listener.RetryPolicy)
	require.NotNil(t, listener.RetryPolicy.MaxRetries)
	assert.Equal(t, 2, *listener.RetryPolicy.MaxRetries)
	require.NotNil(t, listener.RetryPolicy.Delay)
	assert.Equal(t, 0.5, *listener.RetryPolicy.Delay)
	assert.Nil(t, listener.RetryPolicy.BackoffFactor) // Not specified, should be nil

	// Assert Watchers
	require.Len(t, cfg.Watchers, 1)
	watcher := cfg.Watchers[0]
	assert.Equal(t, "watcher1", watcher.ID)
	assert.Equal(t, "check_something.sh", watcher.Script)
	assert.Equal(t, "action2", watcher.ActionID)
	assert.Equal(t, 5*time.Minute, watcher.Interval.Duration)
	assert.Nil(t, watcher.RetryPolicy) // Not specified

	// Assert Timers
	require.Len(t, cfg.Timers, 1)
	timer := cfg.Timers[0]
	assert.Equal(t, "timer1", timer.ID)
	assert.Equal(t, "action3", timer.ActionID)
	assert.Equal(t, 24*time.Hour, timer.Interval.Duration)
	assert.Nil(t, timer.RetryPolicy) // Not specified

	// Assert Actions
	require.Len(t, cfg.Actions, 3)
	assert.Equal(t, "action1", cfg.Actions[0].ID)
	assert.Equal(t, "do_something.sh", cfg.Actions[0].Script)
	assert.Nil(t, cfg.Actions[0].RetryPolicy) // Not specified
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistent/path/to/config.yaml")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to read config file")
	assert.ErrorIs(t, err, os.ErrNotExist) // Check for the specific underlying error
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	invalidYAML := `
application:
  log_level: debug
listeners:
  - id: listener1
   path: /webhook1 # Incorrect indentation
`
	filePath := createTempConfigFile(t, invalidYAML)
	_, err := LoadConfig(filePath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to unmarshal config file")
	// The specific yaml error might vary, but check it's a yaml error
	assert.ErrorContains(t, err, "yaml:")
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	filePath := createTempConfigFile(t, "")
	cfg, err := LoadConfig(filePath)
	// Loading an empty file might be considered valid, resulting in a default/empty config struct
	// Or it might be an error. Let's assume it's valid and returns a zeroed Config struct.
	// If the desired behavior is an error, this test needs adjustment.
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, models.Config{}, *cfg) // Check if it's the zero value
}

func TestLoadConfig_DefaultsApplied(t *testing.T) {
	// Test if defaults are applied correctly when sections are missing
	minimalYAML := `
application:
  log_level: info
actions:
  - id: action1
    script: run.sh
`
	filePath := createTempConfigFile(t, minimalYAML)
	cfg, err := LoadConfig(filePath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check that defaults are zero/nil where appropriate
	assert.Equal(t, "info", cfg.Application.LogLevel)
	assert.Empty(t, cfg.Application.LogFormat) // No default specified in struct/yaml
	assert.Zero(t, cfg.Application.MaxConcurrency)
	assert.Nil(t, cfg.Application.DefaultRetry.MaxRetries) // No default retry specified
	assert.Empty(t, cfg.Listeners)
	assert.Empty(t, cfg.Watchers)
	assert.Empty(t, cfg.Timers)
	require.Len(t, cfg.Actions, 1)
	assert.Equal(t, "action1", cfg.Actions[0].ID)
}

// Add more tests as needed, e.g., for specific field validation errors if LoadConfig does that,
// or for more complex merging scenarios if applicable.

package config

import (
	"errors" // Add errors import
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pbaity/lex/pkg/models"
)

// ValidateConfig checks the entire configuration for logical consistency and required fields.
func ValidateConfig(cfg *models.Config) error {
	// Add nil check at the beginning
	if cfg == nil {
		return errors.New("config cannot be nil")
	}

	if err := validateApplicationSettings(&cfg.Application); err != nil {
		return fmt.Errorf("invalid application settings: %w", err)
	}

	// Keep track of defined behavior and action IDs for validation
	actionIDs := make(map[string]bool)
	listenerIDs := make(map[string]bool)
	watcherIDs := make(map[string]bool)
	timerIDs := make(map[string]bool)

	for i, action := range cfg.Actions {
		if err := validateActionConfig(&action, i); err != nil {
			return fmt.Errorf("invalid action config at index %d (ID: %s): %w", i, action.ID, err)
		}
		if _, exists := actionIDs[action.ID]; exists {
			return fmt.Errorf("duplicate action ID found: %s", action.ID)
		}
		actionIDs[action.ID] = true
	}

	// Keep track of listener paths and IDs to prevent duplicates
	listenerPaths := make(map[string]bool)
	for i, listener := range cfg.Listeners {
		if err := validateListenerConfig(&listener, i, actionIDs); err != nil {
			return fmt.Errorf("invalid listener config at index %d (ID: %s): %w", i, listener.ID, err)
		}
		// Check for duplicate ID
		if _, exists := listenerIDs[listener.ID]; exists {
			return fmt.Errorf("duplicate listener ID found: %s", listener.ID)
		}
		listenerIDs[listener.ID] = true
		// Check for duplicate path
		if _, exists := listenerPaths[listener.Path]; exists {
			return fmt.Errorf("duplicate listener path found: %s", listener.Path)
		}
		listenerPaths[listener.Path] = true
	}

	for i, watcher := range cfg.Watchers {
		if err := validateWatcherConfig(&watcher, i, actionIDs); err != nil {
			return fmt.Errorf("invalid watcher config at index %d (ID: %s): %w", i, watcher.ID, err)
		}
		// Check for duplicate ID
		if _, exists := watcherIDs[watcher.ID]; exists {
			return fmt.Errorf("duplicate watcher ID found: %s", watcher.ID)
		}
		watcherIDs[watcher.ID] = true
	}

	for i, timer := range cfg.Timers {
		if err := validateTimerConfig(&timer, i, actionIDs); err != nil {
			return fmt.Errorf("invalid timer config at index %d (ID: %s): %w", i, timer.ID, err)
		}
		// Check for duplicate ID
		if _, exists := timerIDs[timer.ID]; exists {
			return fmt.Errorf("duplicate timer ID found: %s", timer.ID)
		}
		timerIDs[timer.ID] = true
	}

	// TODO: Add validation for merging default settings (e.g., ensure retry policies are valid)

	return nil
}

func validateApplicationSettings(app *models.ApplicationSettings) error {
	if app.LogLevel != "" {
		level := strings.ToLower(app.LogLevel)
		if level != "debug" && level != "info" && level != "warn" && level != "error" {
			return fmt.Errorf("invalid log_level: %s (must be debug, info, warn, or error)", app.LogLevel)
		}
	}
	if app.LogFormat != "" {
		format := strings.ToLower(app.LogFormat)
		if format != "text" && format != "json" {
			return fmt.Errorf("invalid log_format: %s (must be text or json)", app.LogFormat)
		}
	}
	if app.MaxConcurrency < 0 {
		return fmt.Errorf("max_concurrency cannot be negative: %d", app.MaxConcurrency)
	}
	if err := validateRetryPolicy(&app.DefaultRetry, "default_retry"); err != nil {
		return err
	}
	// PIDFilePath and QueuePersistPath validity might depend on runtime permissions,
	// but we can check if they are absolute paths if needed, or leave it to runtime checks.
	return nil
}

func validateActionConfig(action *models.ActionConfig, index int) error {
	if action.ID == "" {
		return fmt.Errorf("action at index %d must have an ID", index)
	}
	if action.Script == "" {
		return fmt.Errorf("script cannot be empty")
	}
	// Basic check: if script looks like a path, ensure it's not obviously invalid.
	// More robust checks (existence, permissions) happen at runtime.
	if strings.Contains(action.Script, "/") || strings.Contains(action.Script, "\\") {
		if !filepath.IsAbs(action.Script) {
			// Could warn about relative paths, but might be intended.
		}
	} else if strings.ContainsAny(action.Script, "\n\r") {
		// It's likely an inline script, which is fine.
	} else {
		// It might be a command name - validation depends on PATH lookup at runtime.
	}

	if action.RetryPolicy != nil {
		if err := validateRetryPolicy(action.RetryPolicy, "retry_policy"); err != nil {
			return err
		}
	}
	return nil
}

func validateListenerConfig(listener *models.ListenerConfig, index int, actionIDs map[string]bool) error {
	if listener.ID == "" {
		return fmt.Errorf("listener at index %d must have an ID", index)
	}
	if listener.Path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !strings.HasPrefix(listener.Path, "/") {
		return fmt.Errorf("path must start with '/'")
	}
	// Basic URL path validation
	if _, err := url.Parse(listener.Path); err != nil {
		return fmt.Errorf("invalid path format: %w", err)
	}

	if listener.ActionID == "" {
		return fmt.Errorf("action cannot be empty")
	}
	if _, exists := actionIDs[listener.ActionID]; !exists {
		return fmt.Errorf("action ID '%s' not found in defined actions", listener.ActionID)
	}

	if listener.RateLimit != nil && *listener.RateLimit <= 0 {
		return fmt.Errorf("rate_limit must be positive if set")
	}
	if listener.Burst != nil && *listener.Burst <= 0 {
		return fmt.Errorf("burst must be positive if set")
	}
	if listener.RateLimit == nil && listener.Burst != nil {
		return fmt.Errorf("burst cannot be set without rate_limit")
	}

	if listener.RetryPolicy != nil {
		if err := validateRetryPolicy(listener.RetryPolicy, "retry_policy"); err != nil {
			return err
		}
	}
	return nil
}

func validateWatcherConfig(watcher *models.WatcherConfig, index int, actionIDs map[string]bool) error {
	if watcher.ID == "" {
		return fmt.Errorf("watcher at index %d must have an ID", index)
	}
	if watcher.Script == "" {
		return fmt.Errorf("script cannot be empty")
	}
	if watcher.ActionID == "" {
		return fmt.Errorf("action cannot be empty")
	}
	if _, exists := actionIDs[watcher.ActionID]; !exists {
		return fmt.Errorf("action ID '%s' not found in defined actions", watcher.ActionID)
	}
	if watcher.Interval.Duration <= 0 {
		return fmt.Errorf("interval must be a positive duration")
	}

	if watcher.RetryPolicy != nil {
		if err := validateRetryPolicy(watcher.RetryPolicy, "retry_policy"); err != nil {
			return err
		}
	}
	return nil
}

func validateTimerConfig(timer *models.TimerConfig, index int, actionIDs map[string]bool) error {
	if timer.ID == "" {
		return fmt.Errorf("timer at index %d must have an ID", index)
	}
	if timer.ActionID == "" {
		return fmt.Errorf("action cannot be empty")
	}
	if _, exists := actionIDs[timer.ActionID]; !exists {
		return fmt.Errorf("action ID '%s' not found in defined actions", timer.ActionID)
	}
	if timer.Interval.Duration <= 0 {
		return fmt.Errorf("interval must be a positive duration")
	}

	if timer.RetryPolicy != nil {
		if err := validateRetryPolicy(timer.RetryPolicy, "retry_policy"); err != nil {
			return err
		}
	}
	return nil
}

func validateRetryPolicy(policy *models.RetryPolicy, fieldName string) error {
	if policy == nil {
		return nil // Optional policy is not present, valid.
	}
	if policy.MaxRetries != nil && *policy.MaxRetries < 0 {
		return fmt.Errorf("%s: max_retries cannot be negative", fieldName)
	}
	if policy.Delay != nil && *policy.Delay < 0 {
		return fmt.Errorf("%s: delay cannot be negative", fieldName)
	}
	if policy.BackoffFactor != nil && *policy.BackoffFactor < 1.0 {
		// Allow 1.0 (no backoff), but not less.
		return fmt.Errorf("%s: backoff_factor cannot be less than 1.0", fieldName)
	}
	return nil
}

// Helper function to check if a file path looks like a script file
// This is a basic check and might need refinement.
func isScriptPath(path string) bool {
	// Check if it contains directory separators or known script extensions
	return strings.Contains(path, string(os.PathSeparator)) ||
		strings.HasSuffix(path, ".sh") ||
		strings.HasSuffix(path, ".py") ||
		strings.HasSuffix(path, ".js") // Add other common script extensions if needed
}

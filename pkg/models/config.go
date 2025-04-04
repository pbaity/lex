package models

import "time"

// Config is the root configuration structure for the Lex application.
type Config struct {
	Application ApplicationSettings `yaml:"application"`
	Listeners   []ListenerConfig    `yaml:"listeners"`
	Watchers    []WatcherConfig     `yaml:"watchers"`
	Timers      []TimerConfig       `yaml:"timers"`
	Actions     []ActionConfig      `yaml:"actions"`
}

// ApplicationSettings holds global configuration settings for the application.
type ApplicationSettings struct {
	LogLevel         string      `yaml:"log_level"`          // e.g., "debug", "info", "warn", "error"
	LogFormat        string      `yaml:"log_format"`         // e.g., "text", "json"
	DefaultRetry     RetryPolicy `yaml:"default_retry"`      // Default retry policy for actions/watchers
	MaxConcurrency   int         `yaml:"max_concurrency"`    // Max number of actions to run concurrently
	QueuePersistPath string      `yaml:"queue_persist_path"` // Path to save queue state
	PIDFilePath      string      `yaml:"pid_file_path"`      // Path to store the process ID
}

// RetryPolicy defines the parameters for retrying failed operations.
// Pointers are used to distinguish between a value being explicitly set (even to 0 or 0.0)
// and not being set at all, allowing for proper merging with default policies.
type RetryPolicy struct {
	MaxRetries    *int     `yaml:"max_retries"`    // Max number of retries
	Delay         *float64 `yaml:"delay"`          // Initial delay in seconds
	BackoffFactor *float64 `yaml:"backoff_factor"` // Multiplier for exponential backoff (e.g., 2.0)
}

// ListenerConfig defines the configuration for a single webhook listener.
type ListenerConfig struct {
	ID          string       `yaml:"id"`           // Unique identifier for the listener
	Path        string       `yaml:"path"`         // HTTP path to listen on (e.g., "/webhook/github")
	ActionID    string       `yaml:"action"`       // ID of the action to trigger
	AuthToken   string       `yaml:"auth_token"`   // Optional bearer token for authentication
	RateLimit   *float64     `yaml:"rate_limit"`   // Optional requests per second limit (token bucket rate)
	Burst       *int         `yaml:"burst"`        // Optional burst size for rate limiting (token bucket capacity)
	RetryPolicy *RetryPolicy `yaml:"retry_policy"` // Optional specific retry policy override for the triggered action
	Description string       `yaml:"description"`  // Optional description
}

// WatcherConfig defines the configuration for a single watcher.
type WatcherConfig struct {
	ID          string       `yaml:"id"`           // Unique identifier for the watcher
	Script      string       `yaml:"script"`       // Path to the script or the script content itself
	ActionID    string       `yaml:"action"`       // ID of the action to trigger if script succeeds (exit code 0)
	Interval    Duration     `yaml:"interval"`     // How often to run the watcher script (e.g., "60s", "5m", "1h")
	RetryPolicy *RetryPolicy `yaml:"retry_policy"` // Optional specific retry policy for the watcher script execution itself
	Description string       `yaml:"description"`  // Optional description
}

// TimerConfig defines the configuration for a single timer.
type TimerConfig struct {
	ID          string       `yaml:"id"`           // Unique identifier for the timer
	ActionID    string       `yaml:"action"`       // ID of the action to trigger
	Interval    Duration     `yaml:"interval"`     // How often to trigger the action (e.g., "24h", "1w")
	RetryPolicy *RetryPolicy `yaml:"retry_policy"` // Optional specific retry policy override for the triggered action
	Description string       `yaml:"description"`  // Optional description
}

// ActionParameter defines a single parameter for an action.
type ActionParameter struct {
	Name        string      `yaml:"name"`        // Parameter name (used in {{placeholder}})
	Type        string      `yaml:"type"`        // Data type (e.g., "string", "int", "bool" - for validation/parsing later)
	Description string      `yaml:"description"` // Optional description
	Default     interface{} `yaml:"default"`     // Optional default value (needs type assertion later)
	Required    bool        `yaml:"required"`    // Is this parameter required? (Validation needed)
}

// ActionConfig defines the configuration for a single executable action.
type ActionConfig struct {
	ID          string            `yaml:"id"`           // Unique identifier for the action
	Description string            `yaml:"description"`  // Description of what the action does
	Script      string            `yaml:"script"`       // Path to the script or the script content itself
	Parameters  []ActionParameter `yaml:"parameters"`   // List of defined parameters for the action
	RetryPolicy *RetryPolicy      `yaml:"retry_policy"` // Optional specific retry policy override for this action
}

// Duration is a wrapper around time.Duration to allow parsing from YAML strings
// like "10s", "5m", "1h".
type Duration struct {
	time.Duration
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Duration.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	var err error
	d.Duration, err = time.ParseDuration(s)
	return err
}

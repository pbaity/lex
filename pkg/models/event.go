package models

import "time"

// EventType indicates the source of the event.
type EventType string

const (
	EventTypeListener EventType = "listener"
	EventTypeWatcher  EventType = "watcher"
	EventTypeTimer    EventType = "timer"
	EventTypeManual   EventType = "manual" // For CLI triggers
)

// Event represents a task to be processed by a worker.
type Event struct {
	ID         string            `json:"id"`         // Unique ID for this event instance (e.g., UUID)
	SourceID   string            `json:"source_id"`  // ID of the Listener, Watcher, Timer, or "manual"
	Type       EventType         `json:"type"`       // Type of the event source
	ActionID   string            `json:"action_id"`  // ID of the action to execute
	Timestamp  time.Time         `json:"timestamp"`  // When the event was generated
	Parameters map[string]string `json:"parameters"` // Optional parameters specific to this event instance, potentially overriding action defaults
	// Add other relevant context if needed, e.g., request details for listeners
}

package listener

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/pkg/models"
	"golang.org/x/time/rate"
)

// Queue defines the interface required for enqueuing events.
// This allows decoupling the listener service from the concrete queue implementation.
type Queue interface {
	Enqueue(event models.Event) error
}

// ListenerService manages the registration of listener handlers.
type ListenerService struct {
	config       *models.Config
	eventQueue   Queue          // Use the interface type
	mux          *http.ServeMux // Use the mux passed in
	rateLimiters map[string]*rate.Limiter
	// wg and server fields removed - lifecycle managed externally
}

// NewService creates a new ListenerService, using the provided shared mux.
func NewService(cfg *models.Config, eq Queue, mux *http.ServeMux) *ListenerService { // Accept interface
	return &ListenerService{
		config:       cfg,
		eventQueue:   eq,  // Store interface
		mux:          mux, // Use the provided mux
		rateLimiters: make(map[string]*rate.Limiter),
	}
}

// Start registers listener handlers on the shared mux. It no longer starts a server.
func (s *ListenerService) Start() error {
	l := logger.L()
	if len(s.config.Listeners) == 0 {
		l.Info("No listeners configured.")
		return nil
	}

	l.Info("Registering listener handlers...")
	for _, listenerCfg := range s.config.Listeners {
		// Capture loop variable for closure
		cfg := listenerCfg

		// Initialize rate limiter if configured
		if cfg.RateLimit != nil {
			limit := rate.Limit(*cfg.RateLimit) // requests per second
			burst := 1                          // default burst
			if cfg.Burst != nil {
				burst = *cfg.Burst
			}
			s.rateLimiters[cfg.ID] = rate.NewLimiter(limit, burst)
			l.Info("Initialized rate limiter for listener", "listener_id", cfg.ID, "rate", limit, "burst", burst)
		}

		// Register handler for the listener path
		s.mux.HandleFunc(cfg.Path, s.createHandler(cfg))
		l.Info("Registered listener handler", "listener_id", cfg.ID, "path", cfg.Path, "action_id", cfg.ActionID)
	}

	// Server starting is handled elsewhere now.
	l.Info("Listener handlers registered.")
	return nil
}

// Stop is now potentially a no-op for the listener service itself,
// as the server lifecycle is managed externally.
// We might need it later if listeners need specific cleanup.
func (s *ListenerService) Stop(ctx context.Context) error {
	l := logger.L()
	if len(s.config.Listeners) == 0 {
		l.Debug("No listeners were configured, listener stop is a no-op.")
		return nil
	}
	l.Info("Listener service stopping (currently a no-op).")
	// If listeners had specific resources to clean up (e.g., external connections),
	// that logic would go here.
	return nil
}

// createHandler creates an HTTP handler closure for a specific listener configuration.
func (s *ListenerService) createHandler(cfg models.ListenerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logger.L().With("listener_id", cfg.ID, "path", r.URL.Path, "method", r.Method, "remote_addr", r.RemoteAddr)
		l.Debug("Received request for listener")

		// 1. Rate Limiting Check
		if limiter, ok := s.rateLimiters[cfg.ID]; ok {
			if !limiter.Allow() {
				l.Warn("Request rejected due to rate limiting")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			l.Debug("Rate limit check passed")
		}

		// 2. Authentication Check
		if cfg.AuthToken != "" {
			authHeader := r.Header.Get("Authorization")
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == "" || token != cfg.AuthToken {
				l.Warn("Request rejected due to invalid or missing auth token")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			l.Debug("Authentication check passed")
		}

		// 3. TODO: Parse Request Body/Query Parameters
		// For now, we'll just use the action's default parameters.
		// Later, we could parse JSON body, query params, headers etc.
		// and add them to the event.Parameters map.
		eventParams := make(map[string]string)

		// 4. Create Event
		event := models.Event{
			// ID will be generated by Enqueue
			SourceID:   cfg.ID,
			Type:       models.EventTypeListener,
			ActionID:   cfg.ActionID,
			Timestamp:  time.Now().UTC(),
			Parameters: eventParams, // Use parsed params here eventually
		}

		// 5. Enqueue Event
		if err := s.eventQueue.Enqueue(event); err != nil {
			l.Error("Failed to enqueue event", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		l.Info("Event successfully generated and enqueued from listener")
		w.WriteHeader(http.StatusAccepted) // 202 Accepted is appropriate for async processing
		fmt.Fprintln(w, "Event accepted")
	}
}

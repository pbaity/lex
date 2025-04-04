package logger

import (
	"log/slog"
	"os"
	"strings"

	"github.com/pbaity/lex/pkg/models"
)

var globalLogger *slog.Logger

// Init initializes the global logger based on application settings.
// It should be called once during application startup.
func Init(settings models.ApplicationSettings) {
	var level slog.Level
	switch strings.ToLower(settings.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo // Default to Info level
	}

	opts := &slog.HandlerOptions{
		Level: level,
		// AddSource: true, // Uncomment to include source file and line number
	}

	var handler slog.Handler
	switch strings.ToLower(settings.LogFormat) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		fallthrough // Fallthrough to default text handler
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	globalLogger = slog.New(handler)
	slog.SetDefault(globalLogger) // Set as default for convenience
	globalLogger.Info("Logger initialized", "level", level.String(), "format", settings.LogFormat)
}

// L returns the initialized global logger instance.
// It panics if Init has not been called.
func L() *slog.Logger {
	if globalLogger == nil {
		// Fallback to a default logger if not initialized, though Init should always be called.
		// This prevents nil pointer panics but indicates a setup issue.
		slog.Error("Global logger accessed before initialization. Using default.")
		return slog.Default()
		// Alternatively, panic:
		// panic("logger.Init must be called before accessing the global logger")
	}
	return globalLogger
}

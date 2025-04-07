package logger

import (
	// Add errors import
	"fmt" // Add fmt import
	"io"  // Add io import
	"log/slog"
	"os"
	"strings"

	"github.com/pbaity/lex/pkg/models"
)

var globalLogger *slog.Logger

// Init initializes the global logger based on application settings and an output writer.
// If writer is nil, os.Stdout is used.
// It should be called once during application startup. Returns an error on invalid settings.
func Init(settings models.ApplicationSettings, writer io.Writer) error {
	var level slog.Level
	logLevelLower := strings.ToLower(settings.LogLevel)
	switch logLevelLower {
	case "debug":
		level = slog.LevelDebug
	case "info", "": // Default to Info level if empty
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		// Return error for invalid level
		return fmt.Errorf("invalid log level specified: %s", settings.LogLevel)
	}

	opts := &slog.HandlerOptions{
		Level: level,
		// AddSource: true, // Uncomment to include source file and line number
	}

	// Default to os.Stdout if writer is nil
	if writer == nil {
		writer = os.Stdout
	}

	var handler slog.Handler
	logFormatLower := strings.ToLower(settings.LogFormat)
	switch logFormatLower {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	case "text", "": // Default to text format if empty
		handler = slog.NewTextHandler(writer, opts)
	default:
		// Return error for invalid format
		return fmt.Errorf("invalid log format specified: %s", settings.LogFormat)
	}

	globalLogger = slog.New(handler)
	slog.SetDefault(globalLogger) // Set as default for convenience
	globalLogger.Info("Logger initialized", "level", level.String(), "format", logFormatLower)

	return nil // Success
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

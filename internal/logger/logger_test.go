package logger

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"

	// Add models import
	"github.com/pbaity/lex/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: Removed captureOutput and captureInitOutput helpers as they relied on
// unexported/non-existent functions and the old InitLogger signature.
// Tests will now pass a buffer directly to the new Init function.

func TestInit_Levels(t *testing.T) { // Renamed test
	tests := []struct {
		levelName     string
		expectedLevel slog.Level
		expectDebug   bool // Whether a debug message should be logged
	}{
		{"debug", slog.LevelDebug, true},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"INFO", slog.LevelInfo, false}, // Case-insensitivity check
	}

	// Keep track of the original default logger to restore it later
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger) // Restore default logger after test

	for _, tt := range tests {
		t.Run(tt.levelName, func(t *testing.T) {
			var buf bytes.Buffer
			// Use the new Init function with ApplicationSettings and the buffer
			settings := models.ApplicationSettings{LogLevel: tt.levelName, LogFormat: "text"}
			err := Init(settings, &buf)
			require.NoError(t, err)

			// Log messages at different levels and check the output buffer
			L().Info("Info message")   // Should always appear if level <= INFO
			L().Debug("Debug message") // Should only appear if level <= DEBUG

			output := buf.String()
			// Check for Info message only if level is Info or Debug
			if tt.levelName == "info" || tt.levelName == "debug" || tt.levelName == "INFO" {
				assert.Contains(t, output, "Info message")
			} else {
				assert.NotContains(t, output, "Info message") // Expect no Info message for Warn/Error levels
			}

			// Check for Debug message based on expectDebug flag
			if tt.expectDebug {
				assert.Contains(t, output, "Debug message")
			} else {
				assert.NotContains(t, output, "Debug message")
			}
		})
	}
}

func TestInit_Formats(t *testing.T) { // Renamed test
	tests := []struct {
		formatName   string
		expectJSON   bool   // True if JSON format expected, false for text
		expectedText string // Substring expected in the output
	}{
		{"text", false, "level=INFO msg=\"Test message\""},
		{"json", true, `"level":"INFO","msg":"Test message"`},
		{"TEXT", false, "level=INFO msg=\"Test message\""}, // Case-insensitivity check
	}

	// Keep track of the original default logger to restore it later
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger) // Restore default logger after test

	for _, tt := range tests {
		t.Run(tt.formatName, func(t *testing.T) {
			var buf bytes.Buffer
			// Use the new Init function with ApplicationSettings and the buffer
			settings := models.ApplicationSettings{LogLevel: "info", LogFormat: tt.formatName}
			err := Init(settings, &buf)
			require.NoError(t, err)

			// Log a message to check the format
			L().Info("Test message")
			output := buf.String()

			if tt.expectJSON {
				// Basic check for JSON structure
				assert.True(t, strings.HasPrefix(output, "{"), "Expected JSON start")
				assert.True(t, strings.HasSuffix(strings.TrimSpace(output), "}"), "Expected JSON end")
				assert.Contains(t, output, tt.expectedText)
			} else {
				// Basic check for Text structure
				assert.False(t, strings.HasPrefix(output, "{"), "Expected Text format, not JSON")
				assert.Contains(t, output, tt.expectedText)
			}
			// Ensure debug message is not present (since level is info)
			assert.NotContains(t, output, "Debug message")
		})
	}
}

func TestInit_InvalidLevel(t *testing.T) { // Renamed test
	var buf bytes.Buffer
	settings := models.ApplicationSettings{LogLevel: "invalid_level", LogFormat: "text"}
	err := Init(settings, &buf) // Use new Init
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log level specified") // Match new error message
}

func TestInit_InvalidFormat(t *testing.T) { // Renamed test
	var buf bytes.Buffer
	settings := models.ApplicationSettings{LogLevel: "info", LogFormat: "invalid_format"}
	err := Init(settings, &buf) // Use new Init
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log format specified") // Match new error message
}

func TestInit_DefaultWriter(t *testing.T) { // Renamed test
	// Keep track of the original default logger to restore it later
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger) // Restore default logger after test

	// Test that os.Stdout is used if nil writer is provided
	settings := models.ApplicationSettings{LogLevel: "info", LogFormat: "text"}
	err := Init(settings, nil) // Pass nil writer
	require.NoError(t, err)

	// We can't easily assert it wrote to os.Stdout, but we know Init succeeded.
	// We also check that the global logger was set.
	assert.NotNil(t, L())

	// Restore default logger explicitly for safety in case other tests run in parallel
	// (though defer should handle it)
	err = Init(models.ApplicationSettings{LogLevel: "debug", LogFormat: "text"}, os.Stderr) // Re-init with stderr for safety
	require.NoError(t, err)
}

func TestL_ReturnsLogger(t *testing.T) { // Renamed test
	// Keep track of the original default logger to restore it later
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger) // Restore default logger after test

	// Ensure L() returns the configured logger instance after Init
	settings := models.ApplicationSettings{LogLevel: "debug", LogFormat: "text"}
	var buf bytes.Buffer // Discard output for this test
	err := Init(settings, &buf)
	require.NoError(t, err)

	loggerInstance := L()
	assert.NotNil(t, loggerInstance)
	// We can't directly compare globalLogger as it's unexported,
	// but we can check if L() returns the currently set default logger.
	assert.Equal(t, slog.Default(), loggerInstance)
}

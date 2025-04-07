package action

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pbaity/lex/internal/logger" // Initialize logger for tests
	"github.com/pbaity/lex/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testInitLogger initializes the logger for test execution, discarding output.
func testInitLogger(t *testing.T) {
	t.Helper()
	settings := models.ApplicationSettings{LogLevel: "error", LogFormat: "text"}
	err := logger.Init(settings, io.Discard) // Use io.Discard
	require.NoError(t, err, "Failed to initialize logger for test")
}

// Helper to create a temporary script file
func createTempScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	fileName := "test_script"
	if runtime.GOOS == "windows" {
		fileName += ".bat"
	} else {
		fileName += ".sh"
	}
	filePath := filepath.Join(dir, fileName)

	// Ensure script has execute permissions on non-Windows
	perm := os.FileMode(0644)
	if runtime.GOOS != "windows" {
		perm = 0755
	}

	err := os.WriteFile(filePath, []byte(content), perm)
	require.NoError(t, err, "Failed to write temp script file")
	return filePath
}

func TestNewExecutor(t *testing.T) {
	executor := NewExecutor()
	assert.NotNil(t, executor)
}

func TestExecutor_Execute_Success(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()
	scriptContent := `#!/bin/sh
echo "Success output"
>&2 echo "Success error output"
exit 0
`
	if runtime.GOOS == "windows" {
		scriptContent = `@echo off
echo Success output
echo Success error output 1>&2
exit 0`
	}
	scriptPath := createTempScript(t, scriptContent)

	event := models.Event{ID: "evt1", ActionID: "act1"}
	actionCfg := models.ActionConfig{ID: "act1", Script: scriptPath}
	ctx := context.Background()

	stdout, stderr, err := executor.Execute(ctx, event, actionCfg)

	require.NoError(t, err)
	assert.Contains(t, stdout, "Success output")
	assert.Contains(t, stderr, "Success error output")
}

func TestExecutor_Execute_Failure(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()
	scriptContent := `#!/bin/sh
echo "Failure output"
>&2 echo "Failure error output"
exit 1
`
	if runtime.GOOS == "windows" {
		scriptContent = `@echo off
echo Failure output
echo Failure error output 1>&2
exit 1`
	}
	scriptPath := createTempScript(t, scriptContent)

	event := models.Event{ID: "evt2", ActionID: "act2"}
	actionCfg := models.ActionConfig{ID: "act2", Script: scriptPath}
	ctx := context.Background()

	stdout, stderr, err := executor.Execute(ctx, event, actionCfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 1") // Check for exit code error
	assert.Contains(t, stdout, "Failure output")
	assert.Contains(t, stderr, "Failure error output")
}

func TestExecutor_Execute_CommandNotFound(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()

	event := models.Event{ID: "evt3", ActionID: "act3"}
	// Use a command that's highly unlikely to exist
	actionCfg := models.ActionConfig{ID: "act3", Script: "nonexistent_command_xyz123"}
	ctx := context.Background()

	_, _, err := executor.Execute(ctx, event, actionCfg)

	require.Error(t, err)
	// When using sh -c or cmd /c, the shell often exits with 127 if command not found
	assert.Contains(t, err.Error(), "exit status 127")
}

func TestExecutor_Execute_ParameterSubstitution(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()
	// Script now echoes its arguments
	scriptContent := `#!/bin/sh
echo "$@"
exit 0
`
	if runtime.GOOS == "windows" {
		scriptContent = `@echo off
echo %*
exit 0`
	}
	scriptPath := createTempScript(t, scriptContent)

	event := models.Event{
		ID:       "evt4",
		ActionID: "act4",
		Parameters: map[string]string{
			"event_param": "EventValue",
			// "action_param" is missing here, should use ActionConfig's
		},
	}
	// Define the command string with placeholders for arguments
	commandString := fmt.Sprintf("%s --event={{event_param}} --action={{action_param}} --default={{default_param}}", scriptPath)

	actionCfg := models.ActionConfig{
		ID:     "act4",
		Script: commandString, // Use the command string with placeholders
		Parameters: []models.ActionParameter{
			{Name: "action_param", Default: "ActionDefault"},
			{Name: "default_param", Default: "DefaultValue"},
			{Name: "event_param"}, // Defined but overridden by event
		},
	}
	ctx := context.Background()

	stdout, _, err := executor.Execute(ctx, event, actionCfg)

	require.NoError(t, err)
	// Check that the echoed arguments contain the substituted values
	assert.Contains(t, stdout, "--event=EventValue")
	assert.Contains(t, stdout, "--action=ActionDefault") // Should use default from ActionConfig
	assert.Contains(t, stdout, "--default=DefaultValue")
}

func TestExecutor_Execute_ContextCancellation(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()
	// Use the sleep command directly, not via a script file,
	// to test cancellation of a command run via the shell wrapper.
	commandString := "sleep 60"
	if runtime.GOOS == "windows" {
		// Use timeout command on Windows, /nobreak prevents interruption by Ctrl+C
		// Redirect output to nul to avoid polluting test output.
		commandString = "timeout /t 60 /nobreak > nul"
	}

	event := models.Event{ID: "evt5", ActionID: "act5"}
	// Action config uses the direct command string
	actionCfg := models.ActionConfig{ID: "act5", Script: commandString}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond) // Slightly longer timeout
	defer cancel()

	_, _, err := executor.Execute(ctx, event, actionCfg) // Use blank identifiers for stdout/stderr

	require.Error(t, err)
	// Check if the error indicates context cancellation (killed process or deadline exceeded)
	errStr := err.Error()
	isKilled := strings.Contains(errStr, "signal: killed") || strings.Contains(errStr, "exit status -1") // Windows might report -1
	isDeadlineExceeded := strings.Contains(errStr, context.DeadlineExceeded.Error())
	assert.True(t, isKilled || isDeadlineExceeded, "Error should indicate context cancellation (killed [-1] or deadline exceeded), got: %v", err)
	// Depending on OS/timing, stdout might be empty or contain partial output from the shell/command.
	// Avoid asserting specific stdout content here.
	// fmt.Println("Stdout:", stdout) // Keep for debugging if needed
	// fmt.Println("Stderr:", stderr) // Keep for debugging if needed
}

// Test case for inline script content instead of a file path
func TestExecutor_Execute_InlineScript(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()
	scriptContent := `
echo "Inline script success"
>&2 echo "Inline stderr"
exit 0
`
	// No #! needed for inline if executed via sh -c or cmd /c

	event := models.Event{ID: "evt6", ActionID: "act6"}
	actionCfg := models.ActionConfig{ID: "act6", Script: scriptContent}
	ctx := context.Background()

	stdout, stderr, err := executor.Execute(ctx, event, actionCfg)

	require.NoError(t, err)
	assert.Contains(t, stdout, "Inline script success")
	assert.Contains(t, stderr, "Inline stderr")
}

func TestExecutor_Execute_MissingRequiredParam(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()

	event := models.Event{
		ID:         "evt7",
		ActionID:   "act7",
		Parameters: map[string]string{}, // No parameters provided
	}
	actionCfg := models.ActionConfig{
		ID:     "act7",
		Script: "echo 'should not run'",
		Parameters: []models.ActionParameter{
			{Name: "required_param", Required: true}, // This parameter is required
		},
	}
	ctx := context.Background()

	_, _, err := executor.Execute(ctx, event, actionCfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required parameters: required parameter 'required_param' is missing")
}

func TestExecutor_Execute_MissingPlaceholder(t *testing.T) {
	testInitLogger(t)
	executor := NewExecutor()

	event := models.Event{ID: "evt8", ActionID: "act8"}
	// Script uses a placeholder that is not defined in parameters or event
	actionCfg := models.ActionConfig{
		ID:     "act8",
		Script: "echo {{undefined_placeholder}}",
	}
	ctx := context.Background()

	_, _, err := executor.Execute(ctx, event, actionCfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parameter substitution failed")
	assert.Contains(t, err.Error(), "placeholder '{{undefined_placeholder}}' not found")
}

// --- Helper Function Tests ---

func TestIsLikelyFilePathOrCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"Empty String", "", false},                 // Handled by explicit check
		{"Simple Command", "echo", true},            // No separators/newlines/spaces -> treated as command path
		{"Command with Space", "echo hello", false}, // Contains space -> not likely a path itself
		{"Absolute Path Linux", "/usr/bin/script.sh", true},
		{"Relative Path Linux", "./scripts/run.sh", true},
		{"Absolute Path Windows", `C:\Tools\run.exe`, true},
		{"Relative Path Windows", `scripts\run.bat`, true},
		{"Path with .sh", "my_script.sh", true},
		{"Path with .py", "process.py", true},
		{"Path with .exe", "tool.exe", true},
		{"Inline Script with Newline", "echo hello\necho world", false},
		{"Inline Script with CR", "echo hello\recho world", false},
		{"String with spaces only", "   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isLikelyFilePathOrCommand(tt.input))
		})
	}
}

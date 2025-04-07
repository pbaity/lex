package action

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/pbaity/lex/internal/logger"
	"github.com/pbaity/lex/pkg/models"
)

// PlaceholderRegex matches the {{placeholder}} syntax.
var PlaceholderRegex = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

// Executor handles the execution of configured actions.
type Executor struct {
	// Potential future dependencies: config access, secrets manager?
}

// NewExecutor creates a new action executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// Execute runs the specified action based on the event and action configuration.
// It handles parameter substitution and captures output.
func (e *Executor) Execute(ctx context.Context, event models.Event, actionCfg models.ActionConfig) (stdout, stderr string, err error) {
	l := logger.L().With("event_id", event.ID, "action_id", actionCfg.ID)
	l.Info("Executing action")

	// 1. Prepare Parameters: Process defined parameters, apply defaults, override with event params.
	finalParams := make(map[string]string)
	paramErrors := []string{}

	// Process defined parameters and apply defaults
	for _, paramDef := range actionCfg.Parameters {
		// Check if provided in the event first
		eventValue, eventProvided := event.Parameters[paramDef.Name]
		if eventProvided {
			// TODO: Add type validation/conversion based on paramDef.Type
			finalParams[paramDef.Name] = eventValue
		} else if paramDef.Default != nil {
			// Apply default value if not provided in event
			// TODO: Add type validation/conversion for default value
			finalParams[paramDef.Name] = fmt.Sprintf("%v", paramDef.Default) // Simple conversion for now
		} else if paramDef.Required {
			// Parameter is required but not provided and has no default
			paramErrors = append(paramErrors, fmt.Sprintf("required parameter '%s' is missing", paramDef.Name))
		}
		// If not required, not in event, and no default, it's simply omitted.
	}

	// Add any parameters from the event that were *not* explicitly defined in the action config?
	// For now, let's only allow explicitly defined parameters + event context.
	// for k, v := range event.Parameters {
	// 	if _, defined := finalParams[k]; !defined {
	// 		// Maybe log a warning about unexpected parameters?
	// 	}
	// }

	// Inject standard event context parameters (prefixed with 'event_')
	// These cannot be overridden by user parameters for consistency.
	finalParams["event_id"] = event.ID
	finalParams["event_source_id"] = event.SourceID
	finalParams["event_action_id"] = event.ActionID // Action ID being executed
	finalParams["event_source_type"] = string(event.Type)
	finalParams["event_timestamp"] = event.Timestamp.Format(time.RFC3339) // Use a standard format

	// Check for missing required parameters before proceeding
	if len(paramErrors) > 0 {
		errMsg := "missing required parameters: " + strings.Join(paramErrors, ", ")
		l.Error("Parameter preparation failed", "error", errMsg)
		return "", "", fmt.Errorf("%s", errMsg)
	}

	l.Debug("Final parameters prepared (with event context)", "params", finalParams) // Be cautious logging parameters

	// 2. Substitute Parameters in the script content/path
	scriptContent, err := substitutePlaceholders(actionCfg.Script, finalParams)
	if err != nil {
		l.Error("Parameter substitution failed", "error", err)
		return "", "", fmt.Errorf("parameter substitution failed: %w", err)
	}

	l.Debug("Command string after substitution", "command", scriptContent) // Log substituted command string

	// 3. Determine execution method based on *original* script content
	var cmd *exec.Cmd
	isOriginalInline := strings.ContainsAny(actionCfg.Script, "\n\r") // Check original script for newlines

	if isOriginalInline {
		l.Debug("Executing as inline script (via temp file)")
		// Create a temporary script file for the *substituted* inline script
		tmpFile, err := os.CreateTemp("", fmt.Sprintf("lex_action_%s_*.sh", actionCfg.ID))
		if err != nil {
			l.Error("Failed to create temporary script file", "error", err)
			return "", "", fmt.Errorf("failed to create temp script file: %w", err)
		}
		defer os.Remove(tmpFile.Name()) // Clean up the temp file

		// Write the *substituted* script content to the temp file
		// No need to add shebang here, as we'll execute the temp file directly.
		// The OS will use the shebang if present in the original (and thus substituted) content.
		if _, err := tmpFile.WriteString(scriptContent); err != nil {
			l.Error("Failed to write substituted content to temporary script file", "error", err)
			tmpFile.Close() // Close before removing
			return "", "", fmt.Errorf("failed to write temp script: %w", err)
		}
		tmpFile.Close() // Close the file so it can be executed

		// Make the temporary script executable
		if err := os.Chmod(tmpFile.Name(), 0700); err != nil {
			l.Error("Failed to make temporary script executable", "error", err)
			return "", "", fmt.Errorf("failed to chmod temp script: %w", err)
		}

		l.Debug("Executing inline script via temporary file using sh", "temp_file", tmpFile.Name())
		// Explicitly use 'sh' to execute the temporary script for consistency
		cmd = exec.CommandContext(ctx, "sh", tmpFile.Name())

	} else {
		l.Debug("Executing as command/path via OS shell")
		// Treat the *substituted* scriptContent as a command string to be executed by the shell.
		// This handles arguments and potential quoting correctly after substitution.
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/C", scriptContent)
		} else {
			// Use 'sh -c' for POSIX shells
			cmd = exec.CommandContext(ctx, "sh", "-c", scriptContent)
		}
	}

	// 4. Execute the command and capture output
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	startTime := time.Now()
	// Use Start() and Wait() for better cancellation handling
	if err := cmd.Start(); err != nil {
		l.Error("Failed to start action command", "error", err)
		return "", "", fmt.Errorf("failed to start command: %w", err)
	}

	// Goroutine to wait for context cancellation and kill the process
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var runErr error
	select {
	case <-ctx.Done():
		// Context was cancelled, attempt to kill the process
		l.Warn("Context cancelled during action execution, attempting to kill process", "pid", cmd.Process.Pid)
		if killErr := cmd.Process.Kill(); killErr != nil {
			l.Error("Failed to kill process after context cancellation", "pid", cmd.Process.Pid, "error", killErr)
		} else {
			l.Info("Process killed successfully due to context cancellation", "pid", cmd.Process.Pid)
		}
		// Wait for the cmd.Wait() goroutine to finish, capturing its error (likely "signal: killed")
		runErr = <-done
		// Ensure the error reflects the context cancellation
		if runErr == nil {
			runErr = ctx.Err() // If Wait() didn't error, use the context error
		} else if !strings.Contains(runErr.Error(), "killed") {
			// If Wait() errored but not due to being killed, wrap with context error
			runErr = fmt.Errorf("%w (underlying error: %s)", ctx.Err(), runErr)
		}
		l.Warn("Action terminated due to context cancellation", "error", runErr)

	case runErr = <-done:
		// Command completed (successfully or with an error) before context cancellation
		l.Debug("Command finished naturally")
	}

	duration := time.Since(startTime)
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if runErr != nil {
		// Log error details, including exit code if available
		exitCode := -1
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		l.Error("Action execution failed", "error", runErr, "exit_code", exitCode, "duration", duration.String(), "stdout", stdout, "stderr", stderr)
		// Return the captured output along with the error
		return stdout, stderr, fmt.Errorf("action command failed with exit code %d: %w", exitCode, runErr)
	}

	l.Info("Action executed successfully", "duration", duration.String(), "stdout_len", len(stdout), "stderr_len", len(stderr))
	l.Debug("Action output", "stdout", stdout, "stderr", stderr) // Log full output only at debug level

	return stdout, stderr, nil
}

// substitutePlaceholders replaces {{key}} patterns in a string with values from a map.
func substitutePlaceholders(template string, params map[string]string) (string, error) {
	var firstError error
	result := PlaceholderRegex.ReplaceAllStringFunc(template, func(match string) string {
		// Extract key from {{key}}
		key := PlaceholderRegex.FindStringSubmatch(match)[1]
		value, ok := params[key]
		if !ok {
			err := fmt.Errorf("placeholder '{{%s}}' not found in provided parameters", key)
			if firstError == nil {
				firstError = err // Capture the first error encountered
			}
			return match // Return the original placeholder if key not found
		}
		return value
	})
	return result, firstError
}

// isLikelyFilePathOrCommand checks if a string looks more like a path or command
// rather than an inline script block. Very basic heuristic.
func isLikelyFilePathOrCommand(s string) bool {
	// Explicitly handle empty string as not a path/command
	if s == "" {
		return false
	}
	// If it contains directory separators or common executable extensions, likely a path/command.
	return strings.ContainsAny(s, "/\\") ||
		strings.HasSuffix(s, ".sh") ||
		strings.HasSuffix(s, ".py") ||
		strings.HasSuffix(s, ".exe") ||
		!strings.ContainsAny(s, "\n\r ") // Simple command without spaces/newlines?
}

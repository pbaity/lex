package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/pbaity/lex/internal/config" // Need config to find PID path
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Lex daemon",
	Long:  `Stops the running Lex daemon process by sending a SIGTERM signal based on the configured PID file.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Stopping Lex daemon...")
		configPath := getConfigPath() // Get config path from root flag

		// Load config just to get the PID file path
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading configuration from '%s' to find PID file: %v\n", configPath, err)
			os.Exit(1)
		}

		pidFilePath := cfg.Application.PIDFilePath
		if pidFilePath == "" {
			fmt.Fprintln(os.Stderr, "Error: PID file path not configured in application settings. Cannot stop daemon.")
			os.Exit(1)
		}

		pidBytes, err := os.ReadFile(pidFilePath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Error: PID file not found at '%s'. Is the daemon running?\n", pidFilePath)
			} else {
				fmt.Fprintf(os.Stderr, "Error reading PID file '%s': %v\n", pidFilePath, err)
			}
			os.Exit(1) // Exit even if not found, as we can't stop it
		}

		pidStr := strings.TrimSpace(string(pidBytes))
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing PID from file '%s': %v\n", pidFilePath, err)
			os.Exit(1)
		}

		if pid <= 0 {
			fmt.Fprintf(os.Stderr, "Error: Invalid PID %d found in file '%s'\n", pid, pidFilePath)
			os.Exit(1)
		}

		// Find the process
		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding process with PID %d (from %s): %v. Maybe it already stopped?\n", pid, pidFilePath, err)
			// Consider removing the stale PID file here?
			// _ = os.Remove(pidFilePath)
			os.Exit(1)
		}

		// Send SIGTERM signal
		fmt.Printf("Sending SIGTERM to process with PID %d...\n", pid)
		err = process.Signal(syscall.SIGTERM)
		if err != nil {
			// Check if the error is because the process doesn't exist
			if err == os.ErrProcessDone || strings.Contains(err.Error(), "process already finished") { // Handle different OS messages
				fmt.Fprintf(os.Stderr, "Process with PID %d already exited.\n", pid)
				// Consider removing the stale PID file here
				// _ = os.Remove(pidFilePath)
				os.Exit(0) // Not an error in this context
			}
			fmt.Fprintf(os.Stderr, "Error sending SIGTERM to process %d: %v\n", pid, err)
			os.Exit(1)
		}

		fmt.Printf("Signal sent successfully to PID %d. Check logs for shutdown status.\n", pid)
		// Note: PID file is removed by the running process during its graceful shutdown.
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

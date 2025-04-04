package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag" // Import pflag for Flag type
	// "github.com/pbaity/lex/internal/config" // Might need config later for PID check
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Lex application",
	Long:  `Starts the Lex application in the foreground.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Get config path from the persistent flag
		configPath := getConfigPath()
		// Run the main application logic in the foreground
		runForeground(configPath)

		// --- Old daemon start logic (commented out) ---
		// fmt.Println("Starting Lex daemon...")
		// // --- Check if already running (Basic PID check) ---
		// configPath := getConfigPath() // Get config path from root flag
		// cfg, err := config.LoadConfig(configPath)
		// if err == nil && cfg.Application.PIDFilePath != "" {
		// 	if _, errRead := os.Stat(cfg.Application.PIDFilePath); errRead == nil {
		// 		// Basic check: PID file exists. A more robust check would verify the process ID.
		// 		fmt.Fprintf(os.Stderr, "Error: PID file found at %s. Is Lex already running?\n", cfg.Application.PIDFilePath)
		// 		os.Exit(1)
		// 	}
		// } else if err != nil {
		// 	fmt.Fprintf(os.Stderr, "Warning: Could not load config to check PID file: %v\n", err)
		// // }
		// // --- End Basic PID check ---
		//
		// // --- Prepare arguments for background process ---
		// Relaunch the current executable without the 'start' command/flag.
		bgArgs := []string{}
		// startCmdFound := false // Unused variable
		// Iterate through original args, skipping the 'start' command itself
		// Cobra might change os.Args, so relying on it directly can be tricky.
		// A safer way might be to reconstruct args based on flags *not* being 'start'.
		// For now, assume 'start' is the first arg after the executable name if present.
		// This logic might be fragile if cobra rearranges os.Args or if flags come before command.
		// A more robust way is needed if this proves unreliable.
		if len(os.Args) > 1 && os.Args[1] == "start" {
			// Assume flags came *after* 'start' command in the original invocation
			bgArgs = os.Args[2:] // Use args after 'start'
		} else {
			// Fallback: Reconstruct args from parsed flags if 'start' wasn't detected as os.Args[1]
			// This covers cases like `lex --config file.yaml start`
			// Cobra should handle this - if this Run func is called, 'start' was the command.
			// We just need to ensure flags are passed correctly. Cobra handles flag parsing.
			// The main challenge is relaunching *without* the 'start' command but *with* flags.
			// Let's just pass the flags Cobra parsed.
			bgArgs = []string{}                     // Start with empty args for the background process
			cmd.Flags().Visit(func(f *pflag.Flag) { // Use pflag.Flag type
				// Reconstruct the flag argument
				bgArgs = append(bgArgs, fmt.Sprintf("--%s=%s", f.Name, f.Value.String()))
			})

		}
		// Ensure config path is passed if not default (Cobra handles default values)
		if cfgFile != "config.yaml" { // Check if the global cfgFile was changed from default
			foundCfgFlag := false
			for _, arg := range bgArgs {
				if strings.HasPrefix(arg, "--config=") {
					foundCfgFlag = true
					break
				}
			}
			if !foundCfgFlag {
				bgArgs = append(bgArgs, "--config="+cfgFile)
			}
		}

		// --- Instruct user on backgrounding ---
		// NOTE: True daemonization is complex. This version just prints instructions.
		fmt.Println("---------------------------------------------------------------------")
		fmt.Println("NOTE: True background daemonization is not implemented yet.")
		fmt.Println("Please run the following command manually in your terminal:")
		fmt.Printf("\nnohup %s %s > lex_daemon.log 2>&1 &\n\n", os.Args[0], strings.Join(bgArgs, " "))
		fmt.Println("This will run Lex in the background, detached from your terminal,")
		fmt.Println("and redirect its output to 'lex_daemon.log'.")
		fmt.Println("---------------------------------------------------------------------")
		// // Parent process exits immediately after instructing user.
		// os.Exit(0) // Exit the 'lex start' process
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

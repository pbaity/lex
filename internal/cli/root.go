package cli

import (
	"github.com/spf13/cobra"
)

var (
	// cfgFile will hold the path to the config file, bound to the persistent flag
	cfgFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "lex",
	Short: "Lex is a lightweight automation tool",
	Long: `Lex responds to events (listeners, watchers, timers)
by executing user-defined actions based on a YAML configuration.

Run 'lex help <command>' for more information on a specific command.
If no command is specified, Lex attempts to run in foreground mode.`,
	// If Run is defined, it will be executed if no subcommand is provided.
	// We want the default behavior (no subcommand) to be running the daemon/app.
	// This logic will be handled in main.go based on whether a command was executed.
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Define the persistent --config flag on the root command
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "Path to the configuration file")

	// Add subcommands here (will be created in separate files)
	// rootCmd.AddCommand(startCmd)
	// rootCmd.AddCommand(stopCmd)
	// rootCmd.AddCommand(restartCmd)
	// rootCmd.AddCommand(reloadCmd)
	// rootCmd.AddCommand(listActionsCmd)
	// rootCmd.AddCommand(listBehaviorsCmd)
	// rootCmd.AddCommand(triggerCmd)
}

// Helper function to get the config file path (used by commands)
func getConfigPath() string {
	// Cobra automatically parses the flag into the cfgFile variable
	return cfgFile
}

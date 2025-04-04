package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Lex daemon",
	Long:  `Stops the running Lex daemon process and then starts it again.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Restarting Lex daemon...")

		// Execute the stop command's logic first
		// We pass nil for args as stopCmd doesn't expect specific args
		fmt.Println("\n--- Stopping ---")
		stopCmd.Run(cmd, nil) // Call stopCmd's Run directly

		// Execute the start command's logic next
		// We pass nil for args as startCmd doesn't expect specific args
		fmt.Println("\n--- Starting ---")
		startCmd.Run(cmd, nil) // Call startCmd's Run directly

		fmt.Println("\nRestart sequence initiated.")
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}

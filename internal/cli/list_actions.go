package cli

import (
	"fmt"
	"os"

	"github.com/pbaity/lex/internal/config" // Need config loading
	"github.com/spf13/cobra"
)

var listActionsCmd = &cobra.Command{
	Use:   "list-actions",
	Short: "List configured actions",
	Long:  `Displays a summary of all actions defined in the configuration file.`,
	Run: func(cmd *cobra.Command, args []string) {
		configPath := getConfigPath() // Get config path from root flag
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading configuration from '%s': %v\n", configPath, err)
			os.Exit(1)
		}

		fmt.Println("--- Configured Actions ---")
		if len(cfg.Actions) == 0 {
			fmt.Println("No actions configured.")
			return
		}
		for i, action := range cfg.Actions {
			fmt.Printf("[%d] ID: %s\n", i, action.ID)
			if action.Description != "" {
				fmt.Printf("    Description: %s\n", action.Description)
			}
			fmt.Printf("    Script: (omitted for brevity)\n") // Avoid printing potentially long scripts
			if len(action.Parameters) > 0 {
				fmt.Printf("    Parameters Defined (%d):\n", len(action.Parameters))
				for _, p := range action.Parameters {
					defaultValStr := ""
					if p.Default != nil {
						defaultValStr = fmt.Sprintf(" (Default: %v)", p.Default)
					}
					requiredStr := ""
					if p.Required {
						requiredStr = " (Required)"
					}
					fmt.Printf("      - %s [%s]%s%s\n", p.Name, p.Type, requiredStr, defaultValStr)
					if p.Description != "" {
						fmt.Printf("        > %s\n", p.Description)
					}
				}
			} else {
				fmt.Println("    Parameters Defined: 0")
			}
			fmt.Println("---")
		}
	},
}

func init() {
	rootCmd.AddCommand(listActionsCmd)
}

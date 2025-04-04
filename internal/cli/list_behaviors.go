package cli

import (
	"fmt"
	"os"

	"github.com/pbaity/lex/internal/config" // Need config loading
	"github.com/spf13/cobra"
)

var listBehaviorsCmd = &cobra.Command{
	Use:   "list-behaviors",
	Short: "List configured behaviors (listeners, watchers, timers)",
	Long:  `Displays a summary of all listeners, watchers, and timers defined in the configuration file.`,
	Run: func(cmd *cobra.Command, args []string) {
		configPath := getConfigPath() // Get config path from root flag
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading configuration from '%s': %v\n", configPath, err)
			os.Exit(1)
		}

		fmt.Println("--- Configured Listeners ---")
		if len(cfg.Listeners) == 0 {
			fmt.Println("No listeners configured.")
		} else {
			for i, l := range cfg.Listeners {
				fmt.Printf("[%d] ID: %s\n", i, l.ID)
				if l.Description != "" {
					fmt.Printf("    Description: %s\n", l.Description)
				}
				fmt.Printf("    Path: %s\n", l.Path)
				fmt.Printf("    ActionID: %s\n", l.ActionID)
				fmt.Printf("    Auth Required: %t\n", l.AuthToken != "")
				rateLimitStr := "N/A"
				if l.RateLimit != nil {
					burstStr := ""
					if l.Burst != nil {
						burstStr = fmt.Sprintf(" (Burst: %d)", *l.Burst)
					}
					rateLimitStr = fmt.Sprintf("%.2f req/s%s", *l.RateLimit, burstStr)
				}
				fmt.Printf("    Rate Limit: %s\n", rateLimitStr)
				fmt.Println("---")
			}
		}

		fmt.Println("\n--- Configured Watchers ---")
		if len(cfg.Watchers) == 0 {
			fmt.Println("No watchers configured.")
		} else {
			for i, w := range cfg.Watchers {
				fmt.Printf("[%d] ID: %s\n", i, w.ID)
				if w.Description != "" {
					fmt.Printf("    Description: %s\n", w.Description)
				}
				fmt.Printf("    Interval: %s\n", w.Interval.Duration)
				fmt.Printf("    ActionID: %s\n", w.ActionID)
				fmt.Printf("    Script: (omitted for brevity)\n")
				fmt.Println("---")
			}
		}

		fmt.Println("\n--- Configured Timers ---")
		if len(cfg.Timers) == 0 {
			fmt.Println("No timers configured.")
		} else {
			for i, t := range cfg.Timers {
				fmt.Printf("[%d] ID: %s\n", i, t.ID)
				if t.Description != "" {
					fmt.Printf("    Description: %s\n", t.Description)
				}
				fmt.Printf("    Interval: %s\n", t.Interval.Duration)
				fmt.Printf("    ActionID: %s\n", t.ActionID)
				fmt.Println("---")
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(listBehaviorsCmd)
}

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	// Use StringArray to capture multiple --param flags
	paramFlags []string
)

// triggerCmd represents the trigger command
var triggerCmd = &cobra.Command{
	Use:   "trigger <action_id>",
	Short: "Manually trigger a configured action",
	Long: `Sends a request to the running Lex daemon to trigger the specified action ID.
Parameters for the action can be provided using one or more --param flags in key=value format.
Example: lex trigger my-action --param user=admin --param target=db1`,
	Args: cobra.ExactArgs(1), // Requires exactly one argument: the action_id
	Run: func(cmd *cobra.Command, args []string) {
		actionID := args[0]
		fmt.Printf("Attempting to trigger action '%s'...\n", actionID)

		// Parse --param flags (key=value) into a map
		params := make(map[string]string)
		for _, p := range paramFlags {
			parts := strings.SplitN(p, "=", 2)
			if len(parts) != 2 || parts[0] == "" {
				fmt.Fprintf(os.Stderr, "Error: Invalid parameter format '%s'. Use key=value.\n", p)
				os.Exit(1)
			}
			params[parts[0]] = parts[1]
		}
		fmt.Printf("With parameters: %v\n", params)

		// --- Send HTTP request to daemon ---
		// TODO: Make daemon address/port configurable
		daemonURL := "http://localhost:8080/lex/trigger" // Assumed endpoint

		// Prepare request body
		requestBody := map[string]interface{}{
			"action_id":  actionID,
			"parameters": params,
		}
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding request body: %v\n", err)
			os.Exit(1)
		}

		// Make the POST request
		resp, err := http.Post(daemonURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error sending trigger request to daemon at %s: %v\n", daemonURL, err)
			fmt.Fprintln(os.Stderr, "Is the Lex daemon running?")
			os.Exit(1)
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode == http.StatusAccepted {
			fmt.Println("Trigger request accepted by daemon. Event enqueued.")
		} else {
			// Try to read body for more info
			var bodyBytes []byte
			limitReader := http.MaxBytesReader(nil, resp.Body, 1024) // Limit response size
			bodyBytes, errRead := io.ReadAll(limitReader)

			fmt.Fprintf(os.Stderr, "Error: Daemon returned status %s\n", resp.Status)
			if errRead == nil && len(bodyBytes) > 0 {
				fmt.Fprintf(os.Stderr, "Response: %s\n", string(bodyBytes))
			} else if errRead != nil {
				fmt.Fprintf(os.Stderr, "(Could not read response body: %v)\n", errRead)
			}
			os.Exit(1)
		}
	},
}

func init() {
	// Add the repeatable --param flag
	triggerCmd.Flags().StringArrayVarP(&paramFlags, "param", "p", []string{}, "Parameter for the action in key=value format (can be repeated)")
	rootCmd.AddCommand(triggerCmd)
}

package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

// reloadCmd represents the reload command
var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload the configuration in the running Lex daemon",
	Long: `Sends a request to the running Lex daemon to reload its configuration
from the file specified by the --config flag (or the default).
This attempts to apply configuration changes without restarting the daemon.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Requesting daemon to reload configuration...")

		// --- Send HTTP request to daemon ---
		// TODO: Make daemon address/port configurable
		daemonURL := "http://localhost:8080/lex/reload" // Assumed endpoint

		// Make the POST request (POST is often used for actions like reload)
		// We don't need to send a body for this simple reload request.
		resp, err := http.Post(daemonURL, "application/json", nil) // No request body
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error sending reload request to daemon at %s: %v\n", daemonURL, err)
			fmt.Fprintln(os.Stderr, "Is the Lex daemon running?")
			os.Exit(1)
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
			fmt.Println("Reload request accepted by daemon.")
			// Optionally print response body if daemon sends one
			bodyBytes, errRead := io.ReadAll(resp.Body)
			if errRead == nil && len(bodyBytes) > 0 {
				fmt.Printf("Daemon response: %s\n", string(bodyBytes))
			}
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
	rootCmd.AddCommand(reloadCmd)
}

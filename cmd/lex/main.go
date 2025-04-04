package main

import (
	"fmt"
	"os"

	"github.com/pbaity/lex/internal/cli" // Import the new cli package
)

// Imports no longer needed in main.go after moving runApp:
// "context"
// "os/signal"
// "strconv"
// "strings"
// "syscall"
// "time"
// "github.com/pbaity/lex/internal/action"
// "github.com/pbaity/lex/internal/behavior/listener"
// "github.com/pbaity/lex/internal/behavior/timer"
// "github.com/pbaity/lex/internal/behavior/watcher"
// "github.com/pbaity/lex/internal/logger"
// "github.com/pbaity/lex/internal/queue"
// "github.com/pbaity/lex/internal/worker"
// "github.com/pbaity/lex/pkg/models"

// These flags are now defined within the cli package using cobra/pflag
// var (
// 	configPath = flag.String("config", "config.yaml", "Path to the configuration file")
// 	startCmd   = flag.Bool("start", false, "Start the application as a daemon")
// 	stopCmd    = flag.Bool("stop", false, "Stop the running application daemon")
// )

func main() {
	// Cobra handles parsing flags and executing the appropriate command's Run function.
	// If no subcommand is specified, rootCmd.Execute() will run, but we haven't defined
	// a Run function for the root command itself. We need to handle the default case
	// (running the app in the foreground) slightly differently.

	// One way is to check if a command other than the root was executed.
	// Cobra doesn't make this super easy directly, but we can check os.Args.
	// A cleaner way is often to make the foreground run mode its *own* command,
	// maybe `lex run` or make it the default action of the root command.

	// Let's try making `runApp` the default action if no subcommand is given.
	// We'll need to modify cli/root.go later to add the Run function.
	// For now, just call Execute.

	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error executing command: %v\n", err)
		os.Exit(1)
	}

	// If cli.Execute() returns without error, it means a subcommand ran successfully
	// (like start, stop, list) and the main function should exit.
	// If no subcommand was specified AND we configure the root command to run `runApp`,
	// then `runApp` would have been executed by Cobra.
}

// runApp function has been moved to internal/cli/run.go

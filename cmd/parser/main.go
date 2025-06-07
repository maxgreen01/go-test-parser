// Main application entry point
package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/maxgreen01/golang-test-parser/pkg/parser"

	"github.com/jessevdk/go-flags"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-colorable"
)

func main() {
	//
	// =========== define and parse command-line flags ===========
	//
	var opts struct {
		// Application Options
		Task       string `long:"task" short:"t" description:"Task to perform: 'statistics' or 'analyze'"`
		ProjectDir string `long:"project" short:"p" description:"Path to the Go project directory to be parsed"`
		SplitByDir bool   `long:"splitByDir" description:"Whether to parse each top-level directory separately (and ignore top-level Go files)"`
		LogLevel   string `long:"logLevel" short:"l" description:"Log level: 'debug', 'info', 'warn', 'error'" default:"info"`
		Timer      bool   `long:"timer" description:"Whether to print the total execution time of the specified task"`
	}

	_, err := flags.NewParser(&opts, flags.Default|flags.AllowBoolValues).Parse()
	if err != nil {
		// Exit successfully when printing the help menu, but with a failure code otherwise
		if flags.WroteHelp(err) {
			os.Exit(0)
		}
		os.Exit(1)
	}

	// Validate flag values

	taskName := strings.TrimSpace(opts.Task)
	if !slices.Contains([]string{"statistics", "analyze"}, taskName) {
		fmt.Printf("Invalid task %q. Must be one of: `statistics`, `analyze`\n", taskName)
		os.Exit(1)
	}

	projectDir := strings.Trim(opts.ProjectDir, "\t\n\v\f\r \"") // Trim whitespace and quotes
	if projectDir == "" {
		fmt.Printf("You must provide a path to a Go project (e.g., ./myproject)!\n")
		os.Exit(1)
	}

	logLevel := strings.ToLower(strings.TrimSpace(opts.LogLevel))
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, logLevel) {
		fmt.Printf("Invalid logLevel %q. Must be one of: 'debug', 'info', 'warn', 'error'\n", logLevel)
		os.Exit(1)
	}

	splitByDir := opts.SplitByDir
	timer := opts.Timer
	// no validation needed for boolean options

	// Map string flag to slog.Level
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	//
	// =========== Set up the logger (with tint for colored output) ===========
	//
	slog.SetDefault(slog.New(
		tint.NewHandler(colorable.NewColorableStderr(), &tint.Options{
			Level:      level,
			TimeFormat: time.DateTime,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				// Write `error` values in red
				if a.Value.Kind() == slog.KindAny {
					if _, ok := a.Value.Any().(error); ok {
						return tint.Attr(9, a)
					}
				}
				return a
			},
		}),
	))

	//
	// ===========  start the parser with the selected task ===========
	//
	fmt.Println()
	slog.Info("Starting the parser with parameters:", "task", taskName, "project", projectDir, "splitByDir", splitByDir, "logLevel", logLevel)

	if timer {
		startTime := time.Now()
		defer func() {
			slog.Info("Total execution time:", "duration", time.Since(startTime))
		}()
	}

	// Actually run the parser
	if err := parser.Parse(projectDir, taskName, splitByDir); err != nil {
		slog.Error("Error parsing project", "err", err, "task", taskName, "project", projectDir)
		os.Exit(1)
	}

	fmt.Println()
	slog.Info("Finished running the parser!", "task", taskName, "project", projectDir)
	fmt.Println()
}

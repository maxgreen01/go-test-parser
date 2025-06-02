// Main application entry point
package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/maxgreen01/golang-test-parser/internal/tasks"
	"github.com/maxgreen01/golang-test-parser/pkg/parser"

	"github.com/jessevdk/go-flags"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-colorable"
)

func main() {
	//
	// =========== define and parse command-line flags ===========
	//
	type Options struct {
		Task       string `long:"task" short:"t" description:"Task to perform: 'statistics' or 'analyze'"`
		ProjectDir string `long:"project" short:"p" description:"Path to the Go project directory to be parsed"`
		LogLevel   string `long:"logLevel" short:"l" description:"Log level: 'debug', 'info', 'warn', 'error'" default:"info"`
	}

	var opts Options
	_, err := flags.Parse(&opts)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Validate flag values

	taskName := opts.Task
	if !slices.Contains([]string{"statistics", "analyze"}, taskName) {
		fmt.Printf("Invalid task %q. Must be one of: `statistics`, `analyze`\n", taskName)
		os.Exit(1)
	}

	projectDir := opts.ProjectDir
	if projectDir == "" {
		fmt.Printf("You must provide a path to a Go project (e.g., ./myproject)!")
		os.Exit(1)
	}

	logLevel := strings.ToLower(opts.LogLevel)
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, logLevel) {
		fmt.Printf("Invalid logLevel %q. Must be one of: 'debug', 'info', 'warn', 'error'\n", logLevel)
		os.Exit(1)
	}

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
	slog.Info("Starting the parser with parameters:", "task", taskName, "project", projectDir, "logLevel", logLevel)

	var task parser.Task
	switch taskName {
	case "statistics":
		task = &tasks.StatisticsTask{}
	case "analyze":
		task = &tasks.AnalyzeTask{}
	}

	// Actually run the parser
	if err := parser.Parse(projectDir, task); err != nil {
		slog.Error("Error parsing project", "err", err, "project", projectDir, "task", taskName)
		os.Exit(1)
	}
	fmt.Println()
}

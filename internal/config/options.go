package config

// Definitions for global command-line flags used across the entire application
type GlobalOptions struct {
	ProjectDir   string `long:"project" short:"p" description:"Path to the Go project directory to be parsed"`
	OutputPath   string `long:"output" short:"o" description:"Path to report output file"`
	AppendOutput bool   `long:"append" description:"Whether to append to the output file instead of overwriting it if the file already exists"`
	SplitByDir   bool   `long:"splitByDir" description:"Whether to parse each top-level directory separately (and ignore top-level Go files)"`
	Threads      int    `long:"threads" description:"The number of concurrent threads (goroutines) to use for parsing when splitting by directory" default:"4"`

	LogLevel string `long:"logLevel" short:"l" description:"The minimum severity of log message that should be displayed" choice:"debug" choice:"info" choice:"warn" choice:"error" default:"info"`
	Timer    bool   `long:"timer" description:"Whether to print the total execution time of the specified task"`
}

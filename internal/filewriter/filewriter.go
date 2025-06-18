// Utility package for writing data to files in different formats.
// To synchronize multiple places that can write to the same file, pass around a reference to the same `FileWriter` instance.
package filewriter

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// todo maybe add util functions for quickly opening/closing a file and writing a single piece of data (especially JSON)

// Provides a simple, thread-safe way to write data to files in different formats.
// File format is automatically detected based on the file extension.
// This struct provides thread-safe methods for writing data to a file concurrently using shared references
// to a FileWriter instance, but only one distinct `FileWriter` instance should refer to any particular file at a time.
type FileWriter struct {
	// Path to the output file. If the path does not contain a directory, (e.g. "output.txt"),
	// the file it will be placed in the default output directory (which is determined at runtime).
	path string

	// The detected format of the output file, based on the file extension of the provided path.
	format FileFormat

	// Whether to append to the output file instead of overwriting it if the file already exists.
	append bool

	// Reference to the file being written to, or `nil` if it has not been opened yet.
	file *os.File

	// Optional reference to an additional helper for the file (e.g. a csv.Writer or json.Encoder), or`nil` if one is not needed.
	appender appender

	// Synchronization tools for accessing the output file and struct fields.
	mu sync.Mutex
}

// Creates a new FileWriter instance with the specified fields.
// If the path does not contain a directory, the file will be placed in the default output directory (which is determined at runtime).
func NewFileWriter(path string, append bool) *FileWriter {
	// initialize simple fields
	writer := &FileWriter{}
	writer.append = append

	// Validate the path, set the format, and actually open the file.
	// This also initializes an `appender` instance based on the detected file format
	if err := writer.SetPath(path); err != nil {
		slog.Error("Error constructing FileWriter", "err", err)
		return nil
	}

	return writer
}

// Gets the output file path for this FileWriter instance in a thread-safe manner.
func (writer *FileWriter) GetPath() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.path
}

// Sets the output file path and format for this FileWriter instance in a thread-safe manner,
// then opens the file and initializes related fields.
// Prepends the default output directory (determined at runtime) if the provided path does not contain a directory.
func (writer *FileWriter) SetPath(path string) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	// Close the the opened file and any related resources (if they already exist) before modifying them
	if writer.file != nil {
		slog.Debug("Closing existing FileWriter resources before updating them", "oldPath", writer.path, "newPath", path)
		writer.Close()
	}

	// If the path doesn't have a directory, prepend the output directory
	if filepath.Dir(path) == "." {
		outputDir, err := GetDefaultOutputDir()
		if err != nil {
			return fmt.Errorf("setting output file path: %w", err)
		}
		path = filepath.Join(outputDir, path)
	}

	writer.path = path
	writer.format = DetectFormat(path)
	if writer.format == FormatUnknown {
		return fmt.Errorf("unsupported output file format (file %q)", path)
	}

	// Open the file and initialize related fields
	if err := writer.openFile(); err != nil {
		return err
	}

	return nil
}

// Write data to the file associated with this FileWriter instance, with file format automatically detected.
// The provided arguments will have a different form depending on the file format:
//   - For text files, each string in `data` is a line of text, and `otherData` is ignored.
//   - For CSV files, `data` represents a single record with each string being a field, and
//     `otherData[0]` represents CSV headers that will be written if the file is empty.
func (writer *FileWriter) Write(data []string, otherData ...[]string) error {
	if len(data) == 0 {
		return nil // Nothing to write
	}

	// Only allow one write operation at a time per FileWriter instance
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.file == nil || writer.appender == nil {
		return errors.New("writer is not properly initialized - try calling SetPath() first")
	}

	// Write the data to the file based on the detected format
	err := writer.appender.append(data, otherData...)
	if err != nil {
		return fmt.Errorf("writing data to output file %q: %w", writer.path, err)
	}

	slog.Info("Data written successfully to file", "outputPath", writer.path)
	return nil
}

// Close the output file and any associated resources, or do nothing if they are already closed.
// Sets the `file` field to `nil` to indicate that the file is no longer open.
// This should only be called when the FileWriter is no longer needed and all data has been written.
func (writer *FileWriter) Close() {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	slog.Debug("Closing FileWriter resources", "outputPath", writer.path)

	if writer.file == nil {
		slog.Debug("Output file is already closed or was never opened", "outputPath", writer.path)
		return // Nothing to close
	}

	if writer.appender != nil {
		if err := writer.appender.close(); err != nil {
			slog.Error("Error closing FileWriter appender", "err", err, "outputPath", writer.path)
		}
	}
	writer.appender = nil

	if writer.file != nil {
		if err := writer.file.Close(); err != nil {
			slog.Error("Error closing FileWriter output file", "err", err, "outputPath", writer.path)
		}
	}
	writer.file = nil
}

//
// =============== Utility functions ===============
//

// Open the output file for writing, creating it if it doesn't exist and respecting the `append` flag.
// Also populates the `appender` field based on the detected file format.
// This operation is not inherently thread-safe, and should be synchronized by the caller.
func (writer *FileWriter) openFile() error {
	if writer.file != nil {
		slog.Warn("Output file is already open, skipping re-opening", "outputPath", writer.path)
		return nil // todo maybe make this an error
	}

	path := writer.path

	// Create the path's directory (including parents) if it doesn't already exist.
	// This is checked before every write operation in case the directory was deleted or moved since the last write.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Clear the existing file if it already exists (unless the `append` flag is set)
	flag := os.O_CREATE | os.O_RDWR
	if writer.append {
		slog.Debug("Appending to output file in case it already exists", "outputPath", path)
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
		slog.Debug("Truncating output file in case it already exists", "outputPath", path)
	}

	// Actually open the file
	f, err := os.OpenFile(writer.path, flag, 0644)
	if err != nil {
		return fmt.Errorf("opening output file %q: %w", path, err)
	}
	writer.file = f

	// Initialize the `appender` based on the detected file format
	writer.appender = newAppender(writer.format, writer.file)
	if writer.appender == nil {
		return fmt.Errorf("appender not supported for output file %q", writer.path)
	}

	return nil
}

// Default directory name for output files, relative to the program root (which is determined at runtime).
const defaultOutputDirName = "output"

// Get the default output directory relative to the project root (determined at runtime)
func GetDefaultOutputDir() (string, error) {
	root, err := getProgramRoot()
	if err != nil {
		return "", fmt.Errorf("getting default output directory: %w", err)
	}
	return filepath.Join(root, defaultOutputDirName), nil
}

// Get the project root directory based on the executable path, or using the current working directory
// if the executable's directory is considered "bad" based on heuristics.
//
// Fall back to the current working directory if:
//   - exe is in a temp dir (was run via `go run`)
//   - exe name starts with `__debug_bin` (is a `dlv` debugger binary)
//   - exe is cached in the `GOCACHE` dir
func getProgramRoot() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exePath)

	// Helper function to fall back to the current working directory
	fallbackToCWD := func() (string, error) {
		dir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting current working directory: %w", err)
		}
		slog.Debug("Falling back to current working directory as project root", "path", dir)
		return dir, nil
	}

	// ======= Check if the executable's directory is "bad" =======
	if strings.Contains(exeDir, os.TempDir()) || strings.HasPrefix(filepath.Base(exePath), "__debug_bin") {
		return fallbackToCWD()
	}

	cacheDir, err := getGoBuildCacheDir()
	if err != nil {
		slog.Warn("Failed to get Go build cache directory", "err", err)
	} else if strings.HasPrefix(exeDir, cacheDir) {
		return fallbackToCWD()
	}

	//
	// Executable's dir is fine
	slog.Debug("Using executable's directory as project root", "path", exeDir)
	return exeDir, nil
}

// Return the Go build cache directory by running 'go env GOCACHE'
func getGoBuildCacheDir() (string, error) {
	cmd := exec.Command("go", "env", "GOCACHE")
	result, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result)), nil
}

// Utility package for writing output to files in different formats.
package outputwriter

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Represents the type of output file.
type OutputFormat int

// Default directory name for output files, relative to the program root (which is determined at runtime).
const defaultOutputDirName = "output"

const (
	FormatUnknown OutputFormat = iota
	FormatText
	FormatCSV
)

// Determine the file format based on file extension
func DetectFormat(path string) OutputFormat {
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".csv":
		return FormatCSV
	case ".txt":
		return FormatText
	default:
		return FormatUnknown
	}
}

// Write output data to a file (assumed to be already validated), automatically detecting the format based on the file extension.
// The provided arguments will have a different form depending on the output format:
//   - For text files, each string in `data` is a line of text, and `headers` should be `nil`.
//   - For CSV files, `data` represents a single record with each string being a field, and `headers` will be written if the file is empty.
//
// Files are created in the default output directory (relative to the determined program root) if a directory is not specified.
func WriteOutput(path string, data []string, headers []string) error {
	if len(data) == 0 {
		return nil // Nothing to write
	}

	// If the path doesn't have a directory, prepend the output directory
	if filepath.Dir(path) == "." {
		outputDir, err := getDefaultOutputDir()
		if err != nil {
			return fmt.Errorf("writing to output file %q: %w", path, err)
		}
		path = filepath.Join(outputDir, path)
	}

	format := DetectFormat(path)
	var err error
	switch format {
	case FormatText:
		content := strings.Join(data, "\n") + "\n"
		err = appendText(path, content)
	case FormatCSV:
		err = appendCSV(path, data, headers)
	default:
		return fmt.Errorf("invalid output format (file %q)", path)
	}

	if err != nil {
		return fmt.Errorf("writing to output file %q: %w", path, err)
	} else {
		slog.Info("Output written successfully", "output", path)
		return nil
	}
}

// Get the output directory relative to the project root, creating it if it doesn't exist
func getDefaultOutputDir() (string, error) {
	root, err := getProgramRoot()
	if err != nil {
		return "", fmt.Errorf("getting default output directory: %w", err)
	}
	outputDir := filepath.Join(root, defaultOutputDirName) // FIXME doesn't seem to be working
	// Create the directory (including parents) if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("creating default output directory: %w", err)
	}
	return outputDir, nil
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
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Append a string of plain text to a file, creating the file if it doesn't exist.
func appendText(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Actually write the text to the file
	_, err = f.WriteString(text)
	return err
}

// Append a single row to a CSV file, also writing headers if the file is empty.
func appendCSV(path string, row []string, headers []string) error {
	// make sure the number of provided row fields matches the number of headers
	if len(row) != len(headers) {
		return fmt.Errorf("provided CSV row field count (%d) does not match header count (%d)", len(row), len(headers))
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)

	// Prepare to check if the file is empty
	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}

	// Check if row field length matches header length (read from first line of the file if already present),
	// and write headers if the file is empty
	if fileInfo.Size() == 0 {
		// File is empty, so write headers
		if err := w.Write(headers); err != nil {
			return err
		}
	} else {
		// File already exists, so read the headers from the first line
		r := csv.NewReader(f)
		existingHeaders, err := r.Read()
		if err != nil {
			return fmt.Errorf("reading CSV headers: %w", err)
		}

		// Check that the provided headers match the existing ones
		if len(headers) != len(existingHeaders) {
			return fmt.Errorf("provided CSV header count (%d) does not match existing header count (%d)", len(row), len(existingHeaders))
		}
		for i, existing := range existingHeaders {
			if headers[i] != existing {
				return fmt.Errorf("provided CSV header %q does not match existing header %q at index %d", headers[i], existing, i)
			}
		}

		// Move file pointer back to end of content for appending
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
	}

	// Actually write the row to the file
	if err := w.Write(row); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

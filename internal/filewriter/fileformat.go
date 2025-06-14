// Includes functions for actually writing data to files of various formats.
package filewriter

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Represents the format of an output file.
type FileFormat int

// Represents the different file file formats supported by the writer.
// Other packages that use this may choose to only support a subset of these formats.
const (
	FormatUnknown FileFormat = iota
	FormatTxt
	FormatCSV
)

// Determine a file's format based on file extension.
// Returns FormatUnknown if the file extension is not recognized or not supported.
// todo consider making this more robust, maybe with a dynamic String() method
func DetectFormat(path string) FileFormat {
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".txt":
		return FormatTxt
	case ".csv":
		return FormatCSV
	default:
		return FormatUnknown
	}
}

// Alias for DetectFormat function using the FileWriter's path.
func (writer *FileWriter) DetectFormat() FileFormat {
	return DetectFormat(writer.GetPath())
}

//
// =============== File Append Operations ===============
//

// Represents a generic way to append data to a file. Used by `FileWriter` to prepare and write data to files.
// The `append` method should format, check, and write data as needed for the specific file format.
// The `close` method should close any resources associated with the appender, but NOT the file itself.
// Appenders are not designed to be thread-safe, so references to them  should not be shared between multiple `FileWriter` instances.
// Implementations of this interface should also have a constructor that takes any necessary parameters (e.g. a file handle).
type appender interface {
	// Append data to the file
	append(data []string, otherData ...[]string) error

	// Close any resources associated with the appender, but NOT the file itself
	close() error
}

// Return an `appender` for the given file format using the specified file, or nil if the format is not supported.
func newAppender(format FileFormat, file *os.File) appender {
	switch format {
	case FormatTxt:
		return newTextAppender(file)
	case FormatCSV:
		return newCSVAppender(file)
	default:
		return nil
	}
}

//
// ~~~~~~~ `appender` implementation for FormatTxt files ~~~~~~~
//

type textAppender struct {
	file *os.File
}

func newTextAppender(file *os.File) *textAppender {
	return &textAppender{file: file}
}

// Print each string as as a line, appending a newline character at the end. `otherData` is ignored.
func (a *textAppender) append(data []string, _ ...[]string) error {
	content := strings.Join(data, "\n") + "\n"
	_, err := a.file.WriteString(content)
	return err
}

func (a *textAppender) close() error { return nil }

//
// ~~~~~~~ `appender` implementation for FormatCSV files ~~~~~~~
//

type csvAppender struct {
	file            *os.File
	writer          *csv.Writer
	existingHeaders []string  // the headers already written to the file, used to ensure data consistency
	once            sync.Once // ensure headers are only initialized (written or read) once
}

func newCSVAppender(file *os.File) *csvAppender {
	return &csvAppender{
		file:   file,
		writer: csv.NewWriter(file),
	}
}

// Append a single row to a CSV file, with headers provided in `otherData[0]`.
// Ensures that the provided headers match any existing ones, or writes headers if the file is initially empty.
func (a *csvAppender) append(data []string, otherData ...[]string) error {
	if len(otherData) == 0 {
		return fmt.Errorf("writing to CSV requires headers in otherData[0]")
	}

	// make sure the number of provided row fields matches the number of provided headers
	headers := otherData[0]
	if len(headers) == 0 {
		return fmt.Errorf("writing to CSV requires non-empty headers")
	}
	if len(data) != len(headers) {
		return fmt.Errorf("provided CSV row field count (%d) does not match header count (%d)", len(data), len(headers))
	}

	// Only initialize headers once
	var headerErr error
	a.once.Do(func() {
		// Get file stats to check if the file is empty
		fileInfo, err := a.file.Stat()
		if err != nil {
			headerErr = err
			return
		}

		// Check if row field length matches header length (read from first line of the file if already present),
		// and write headers if the file is empty
		if fileInfo.Size() == 0 {
			// File is empty, so write headers
			if err := a.writer.Write(headers); err != nil {
				headerErr = err
				return
			}
			a.existingHeaders = headers
			slog.Debug("wrote CSV headers", "headers", headers)
		} else {
			// File already exists, so read the headers from the first line

			// Reopen the file because `Seek` behavior is undefined on files opened with `O_APPEND`
			f, err := os.Open(a.file.Name())
			if err != nil {
				headerErr = err
				return
			}
			defer f.Close()

			// Save headers
			r := csv.NewReader(f)
			existingHeaders, err := r.Read()
			if err != nil {
				headerErr = fmt.Errorf("reading CSV headers: %w", err)
				return
			}
			a.existingHeaders = existingHeaders
		}
	})
	if headerErr != nil {
		return headerErr
	}

	// Check that the provided headers exactly match the existing ones
	if len(headers) != len(a.existingHeaders) {
		return fmt.Errorf("provided CSV header count (%d) does not match existing header count (%d)", len(headers), len(a.existingHeaders))
	}
	for i, existing := range a.existingHeaders {
		if headers[i] != existing {
			return fmt.Errorf("provided CSV header %q does not match existing header %q (index %d)", headers[i], existing, i)
		}
	}

	// Actually write the row to the file
	defer a.writer.Flush()
	if err := a.writer.Write(data); err != nil {
		return err
	}
	return a.writer.Error()
}

func (a *csvAppender) close() error {
	a.writer.Flush()
	return a.writer.Error()
}

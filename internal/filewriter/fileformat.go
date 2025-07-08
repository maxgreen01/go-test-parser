// Includes functions for actually writing data to files of various formats.
package filewriter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	FormatJSON
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
	case ".json":
		return FormatJSON
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
	append(data any, otherData ...any) error

	// Close any resources associated with the appender, but NOT the file itself
	close() error
}

// Return an `appender` for the given file format using the specified file, or nil if the format is not supported.
func newAppender(format FileFormat, file *os.File) appender {
	switch format {
	case FormatTxt:
		return newTextAppender(file)
	case FormatCSV:
		return newCsvAppender(file)
	case FormatJSON:
		return newJsonAppender(file)
	default:
		return nil
	}
}

// Ensure that some data is of type []T (or optionally just T), always returning the data as []T.
// `prefix` and `dataName` are used to create error messages like "<prefix> requires <dataName> to be of []<T>, got %T".
func enforceTypeSlice[T any](data any, nonSliceAllowed bool, prefix, dataName string) ([]T, error) {
	if slice, ok := data.([]T); ok {
		return slice, nil
	}
	if nonSliceAllowed {
		if val, ok := data.(T); ok {
			return []T{val}, nil
		}
		return nil, fmt.Errorf("%s requires %s to be of type %T or []%[3]T, but got %T", prefix, dataName, *new(T), data)
	}
	return nil, fmt.Errorf("%s requires %s to be of type []%T, but got %T", prefix, dataName, *new(T), data)
}

//
// ~~~~~~~ `appender` implementation for text files ~~~~~~~
//

type textAppender struct {
	file *os.File
}

func newTextAppender(file *os.File) *textAppender {
	return &textAppender{file: file}
}

// Print strings with each on its own line, appending a newline character at the end.
// Expects `data` to be a slice of strings. `otherData` is ignored.
func (a *textAppender) append(data any, _ ...any) error {
	// Validate input data as string slice
	strSlice, err := enforceTypeSlice[string](data, true, "writing to text", "data")
	if err != nil {
		return err
	}

	content := strings.Join(strSlice, "\n") + "\n"
	_, err = a.file.WriteString(content)
	return err
}

func (a *textAppender) close() error { return nil }

//
// ~~~~~~~ `appender` implementation for CSV files ~~~~~~~
//

type csvAppender struct {
	file            *os.File
	writer          *csv.Writer
	existingHeaders []string  // the headers already written to the file, used to ensure data consistency
	once            sync.Once // ensure headers are only initialized (written or read) once
}

func newCsvAppender(file *os.File) *csvAppender {
	return &csvAppender{
		file:   file,
		writer: csv.NewWriter(file),
	}
}

// Append a single row to a CSV file, with headers provided in `otherData[0]`.
// Ensures that the provided headers match any existing ones, or writes headers if the file is initially empty.
// Expects `data` and `otherData[0]` to each be a slice of strings.
func (a *csvAppender) append(data any, otherData ...any) error {
	if len(otherData) == 0 {
		return fmt.Errorf("writing to CSV requires headers in otherData[0]")
	}

	// Validate input data as string slices
	row, err := enforceTypeSlice[string](data, false, "writing to CSV", "data")
	if err != nil {
		return err
	}
	headers, err := enforceTypeSlice[string](otherData[0], false, "writing to CSV", "headers")
	if err != nil {
		return err
	}

	// make sure the number of provided row fields matches the number of provided headers
	if len(headers) == 0 {
		return fmt.Errorf("writing to CSV requires non-empty headers")
	}
	if len(row) != len(headers) {
		return fmt.Errorf("provided CSV row field count (%d) does not match header count (%d)", len(row), len(headers))
	}

	// Only initialize headers once, before the first write
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
	if err := a.writer.Write(row); err != nil {
		return err
	}
	return a.writer.Error()
}

func (a *csvAppender) close() error {
	a.writer.Flush()
	return a.writer.Error()
}

//
// ~~~~~~~ `appender` implementation for JSON files ~~~~~~~
//

type jsonAppender struct {
	file           *os.File
	encoder        *json.Encoder
	alreadyWritten []any     // in-memory representation of all the data that's already written to the file
	once           sync.Once // only read from the file once to get existing data
}

// todo maybe add encoder params if needed
func newJsonAppender(file *os.File) *jsonAppender {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")  // Set indentation for pretty printing
	encoder.SetEscapeHTML(false) // Retain characters like '<', '>', '&' in the output
	return &jsonAppender{
		file:    file,
		encoder: encoder,
	}
}

// Write some data element (of any type) to a JSON file using `json.Encode`.
// If the file is empty, the new data is encoded as a standalone element.
// If the file already contains JSON data, the new element is appended to the existing one(s) as an array,
// and the file is rewritten with the updated array.
// If the new data is a slice, `otherData[0]` (expected to be a boolean) indicates whether
// to flatten the slice elements (by one level, to avoid a nested array) if appending to any existing data.
func (a *jsonAppender) append(data any, otherData ...any) error {
	// Read any existing data before the first write
	var readingErr error
	a.once.Do(func() {
		// Seek to start and read existing data
		a.file.Seek(0, io.SeekStart)
		var existing any
		dec := json.NewDecoder(a.file)
		if err := dec.Decode(&existing); err != nil {
			if err == io.EOF {
				return // Nothing to read
			}
			readingErr = fmt.Errorf("reading existing JSON data: %w", err)
			return
		}

		// If the existing data is already an array (decoded as `[]any`), save it directly.
		// Otherwise, wrap it in a slice to ensure we can append to it later.
		switch v := existing.(type) {
		case []any:
			a.alreadyWritten = v
		default:
			a.alreadyWritten = []any{existing}
		}
	})
	if readingErr != nil {
		return readingErr
	}

	// Add the new object to the existing data as an array

	slice := reflect.ValueOf(data)
	// If the data is a slice, check whether to flatten the elements before appending (not as a nested slice)
	if slice.Kind() == reflect.Slice {
		flatten, isBoolean := false, false
		if len(otherData) > 0 {
			if flag, ok := otherData[0].(bool); ok {
				flatten = flag
				isBoolean = true
			}
		}
		if !isBoolean { // Print warning if the flag isn't specified at all or is specified but not a boolean
			slog.Warn("Writing a slice to JSON without flattening because `otherData[0]` is not a boolean; this will result in a nested array")
		}

		if flatten {
			// Use reflection to append each data element individually, retaining the original types
			n := slice.Len()
			a.alreadyWritten = slices.Grow(a.alreadyWritten, n)
			for i := range n {
				a.alreadyWritten = append(a.alreadyWritten, slice.Index(i).Interface())
			}
		} else {
			// Don't flatten, so append the entire slice as a single element
			a.alreadyWritten = append(a.alreadyWritten, data)
		}
	} else {
		// Non-slice data is appended directly as a single element
		a.alreadyWritten = append(a.alreadyWritten, data)
	}

	// Clear the file and write the updated data
	a.file.Truncate(0)
	a.file.Seek(0, io.SeekStart)

	// If there's only one element, don't wrap it in the array from `alreadyWritten`
	if len(a.alreadyWritten) == 1 {
		return a.encoder.Encode(a.alreadyWritten[0])
	} else {
		return a.encoder.Encode(a.alreadyWritten)
	}
}

func (a *jsonAppender) close() error { return nil }

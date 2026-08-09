package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// LineError reports a malformed JSONL line.
type LineError struct {
	Line int
	Err  error
}

// ReadResult contains valid records and any malformed lines skipped while
// reading them.
type ReadResult struct {
	Records   []Record
	Malformed []LineError
}

// ReadFile reads a usage JSONL file without allowing a malformed line to hide
// later valid records.
func ReadFile(path string) (ReadResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return ReadResult{}, err
	}
	defer file.Close()
	return Read(file)
}

// Read decodes usage records from a JSONL stream.
func Read(input io.Reader) (ReadResult, error) {
	reader := bufio.NewReader(input)
	var result ReadResult
	for lineNumber := 1; ; lineNumber++ {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			var record Record
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				result.Malformed = append(result.Malformed, LineError{Line: lineNumber, Err: err})
			} else {
				result.Records = append(result.Records, record)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return result, nil
			}
			return result, readErr
		}
	}
}

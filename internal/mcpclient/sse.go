package mcpclient

import (
	"bufio"
	"io"
	"strings"
)

// sseScanner reads Server-Sent Events from a stream.
// Each call to Scan() reads one complete event (delimited by blank lines).
// Data() returns the concatenated "data:" field values.
type sseScanner struct {
	scanner *bufio.Scanner
	data    []byte
	err     error
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{
		scanner: bufio.NewScanner(r),
	}
}

// Scan reads the next SSE event. Returns false when no more events.
func (s *sseScanner) Scan() bool {
	var dataLines []string

	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Blank line = end of event
		if line == "" {
			if len(dataLines) > 0 {
				s.data = []byte(strings.Join(dataLines, "\n"))
				return true
			}
			continue
		}

		// Skip comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, line[6:])
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, line[5:])
		}
		// Ignore other fields (id:, event:, retry:) for now
	}

	s.err = s.scanner.Err()

	// Handle trailing event without final blank line
	if len(dataLines) > 0 {
		s.data = []byte(strings.Join(dataLines, "\n"))
		return true
	}

	return false
}

// Data returns the data from the last scanned event.
func (s *sseScanner) Data() []byte {
	return s.data
}

// Err returns any error from scanning.
func (s *sseScanner) Err() error {
	return s.err
}

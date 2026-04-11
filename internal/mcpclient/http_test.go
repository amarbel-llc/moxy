package mcpclient

import (
	"strings"
	"testing"
)

// NOTE: HTTP integration tests (TestHTTPTransport*) are disabled because
// go-mcp removed its HTTP/Streamable transport in v0.0.5+. See
// amarbel-llc/purse-first#21 for the plan to restore it.

func TestSSEScanner(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		events []string
	}{
		{
			name:   "single event",
			input:  "data: {\"id\":1}\n\n",
			events: []string{`{"id":1}`},
		},
		{
			name:   "multiple events",
			input:  "data: {\"id\":1}\n\ndata: {\"id\":2}\n\n",
			events: []string{`{"id":1}`, `{"id":2}`},
		},
		{
			name:   "multiline data",
			input:  "data: line1\ndata: line2\n\n",
			events: []string{"line1\nline2"},
		},
		{
			name:   "with id and event fields",
			input:  "id: 1\nevent: message\ndata: hello\n\n",
			events: []string{"hello"},
		},
		{
			name:   "with comments",
			input:  ": comment\ndata: hello\n\n",
			events: []string{"hello"},
		},
		{
			name:   "trailing event without blank line",
			input:  "data: hello",
			events: []string{"hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := newSSEScanner(strings.NewReader(tt.input))
			var got []string
			for scanner.Scan() {
				got = append(got, string(scanner.Data()))
			}
			if scanner.Err() != nil {
				t.Fatalf("scanner error: %v", scanner.Err())
			}
			if len(got) != len(tt.events) {
				t.Fatalf("got %d events, want %d: %v", len(got), len(tt.events), got)
			}
			for i := range got {
				if got[i] != tt.events[i] {
					t.Errorf("event %d: got %q, want %q", i, got[i], tt.events[i])
				}
			}
		})
	}
}

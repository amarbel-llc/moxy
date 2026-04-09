package native

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// resultURIPattern matches moxy.native://results/{session}/{id} substrings
// inside a command string. The session segment allows underscores, dots, and
// hyphens; the id is a UUIDv7-style string.
var resultURIPattern = regexp.MustCompile(`moxy\.native://results/[A-Za-z0-9._-]+/[A-Za-z0-9-]+`)

// resultSubstitution is the result of rewriting moxy.native://results/{session}/{id}
// references inside a command string. The caller must:
//
//  1. Set cmd.ExtraFiles = sub.ExtraFiles before cmd.Start().
//  2. Call sub.StartWriters() after cmd.Start() (so cached payloads stream
//     into the pipes once the child can read from them).
//  3. defer sub.Cleanup() to release pipe ends on every path.
//
// The first ExtraFiles entry becomes file descriptor 3 in the child, the
// second fd 4, and so on (the standard Go os/exec convention).
type resultSubstitution struct {
	Command    string
	ExtraFiles []*os.File

	// pipeWrites are the parent-side write ends paired with the cached
	// payload to stream into them. StartWriters consumes them.
	pipeWrites []pipeWrite
	// pipeReads are the parent's copies of the read ends. They are passed
	// to the child via ExtraFiles; the parent's copies must be closed
	// after Start (so the child sees EOF when the writer finishes).
	pipeReads []*os.File
	// started is set by StartWriters. Cleanup uses it to know whether the
	// goroutines own pipeWrites or it must close them itself.
	started bool
}

type pipeWrite struct {
	w       *os.File
	payload string
}

// StartWriters launches a goroutine for each cached payload that pushes the
// content into its pipe and closes the write end. Must be called after
// cmd.Start() so the child is already wired to the read end.
func (s *resultSubstitution) StartWriters() {
	s.started = true
	for _, pw := range s.pipeWrites {
		pw := pw
		go func() {
			defer pw.w.Close()
			_, _ = io.WriteString(pw.w, pw.payload)
		}()
	}
}

// Cleanup closes pipe handles still owned by the parent. Safe to call
// multiple times. After cmd.Start the parent must close its copy of the read
// ends; if StartWriters was never called (early error path) the write ends
// are also closed here.
func (s *resultSubstitution) Cleanup() {
	for _, r := range s.pipeReads {
		_ = r.Close()
	}
	s.pipeReads = nil
	if !s.started {
		for _, pw := range s.pipeWrites {
			_ = pw.w.Close()
		}
		s.pipeWrites = nil
	}
}

// substituteResultURIs scans command for moxy.native://results/{session}/{id}
// substrings, loads each unique URI from cache, and rewrites every reference
// to /dev/fd/N. Repeated references to the same URI share a single pipe and
// fd so commands like `diff X X` work without deadlocking or duplicating
// work. On error no pipe resources are leaked.
func substituteResultURIs(
	command string,
	cache *resultCache,
) (*resultSubstitution, error) {
	matches := resultURIPattern.FindAllStringIndex(command, -1)
	if len(matches) == 0 {
		return &resultSubstitution{Command: command}, nil
	}

	sub := &resultSubstitution{}
	// Dedupe key is "session/id" so two distinct sessions with the same
	// uuid stay separate.
	fdByKey := make(map[string]int)

	failf := func(format string, args ...any) (*resultSubstitution, error) {
		sub.Cleanup()
		return nil, fmt.Errorf(format, args...)
	}

	var b strings.Builder
	cursor := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		uri := command[start:end]
		session, id, ok := parseResultURI(uri)
		if !ok {
			return failf("invalid result URI: %s", uri)
		}

		key := session + "/" + id
		fd, seen := fdByKey[key]
		if !seen {
			cached, err := cache.load(session, id)
			if err != nil {
				return failf("loading %s: %w", uri, err)
			}

			pr, pw, err := os.Pipe()
			if err != nil {
				return failf("creating pipe for %s: %w", uri, err)
			}

			fd = 3 + len(sub.ExtraFiles)
			fdByKey[key] = fd
			sub.ExtraFiles = append(sub.ExtraFiles, pr)
			sub.pipeReads = append(sub.pipeReads, pr)
			sub.pipeWrites = append(sub.pipeWrites, pipeWrite{
				w:       pw,
				payload: cached.Output,
			})
		}

		b.WriteString(command[cursor:start])
		fmt.Fprintf(&b, "/dev/fd/%d", fd)
		cursor = end
	}
	b.WriteString(command[cursor:])

	sub.Command = b.String()
	return sub, nil
}

// parseResultURI extracts the session and id segments from a
// moxy.native://results/{session}/{id} URI. Returns ok=false for any URI
// that does not match the two-segment form.
func parseResultURI(uri string) (session, id string, ok bool) {
	const prefix = "moxy.native://results/"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", false
	}
	rest := uri[len(prefix):]
	if idx := strings.Index(rest, "?"); idx >= 0 {
		rest = rest[:idx]
	}
	slash := strings.Index(rest, "/")
	if slash <= 0 || slash == len(rest)-1 {
		return "", "", false
	}
	return rest[:slash], rest[slash+1:], true
}

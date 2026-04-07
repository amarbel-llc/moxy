package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// execURIPattern matches maneater.exec://results/{id} substrings inside an
// exec command. The id stops at any character outside [A-Za-z0-9-], which is
// sufficient for the UUIDv7 form newExecResultID() produces.
var execURIPattern = regexp.MustCompile(`maneater\.exec://results/[A-Za-z0-9-]+`)

// execSubstitution is the result of rewriting maneater.exec://results/{id}
// references inside an exec command. The caller must:
//
//  1. Set cmd.ExtraFiles = sub.ExtraFiles before cmd.Start().
//  2. Call sub.StartWriters() after cmd.Start() (so cached payloads stream
//     into the pipes once the child can read from them).
//  3. defer sub.Cleanup() to release pipe ends on every path.
//
// The first ExtraFiles entry becomes file descriptor 3 in the child, the
// second fd 4, and so on (the standard Go os/exec convention).
type execSubstitution struct {
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
func (s *execSubstitution) StartWriters() {
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
func (s *execSubstitution) Cleanup() {
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

// substituteExecURIs scans command for maneater.exec://results/{id}
// substrings, loads each unique id from cache, and rewrites every reference
// to /dev/fd/N. Repeated references to the same id share a single pipe and
// fd so commands like `diff X X` work without deadlocking or duplicating
// work. On error no pipe resources are leaked.
func substituteExecURIs(
	command string,
	cache *execResultCache,
) (*execSubstitution, error) {
	matches := execURIPattern.FindAllStringIndex(command, -1)
	if len(matches) == 0 {
		return &execSubstitution{Command: command}, nil
	}

	sub := &execSubstitution{}
	fdByID := make(map[string]int)

	failf := func(format string, args ...any) (*execSubstitution, error) {
		sub.Cleanup()
		return nil, fmt.Errorf(format, args...)
	}

	var b strings.Builder
	cursor := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		uri := command[start:end]
		id, ok := parseExecResultURI(uri)
		if !ok {
			return failf("invalid exec result URI: %s", uri)
		}

		fd, seen := fdByID[id]
		if !seen {
			cached, err := cache.load(id)
			if err != nil {
				return failf("loading %s: %w", uri, err)
			}

			pr, pw, err := os.Pipe()
			if err != nil {
				return failf("creating pipe for %s: %w", uri, err)
			}

			fd = 3 + len(sub.ExtraFiles)
			fdByID[id] = fd
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

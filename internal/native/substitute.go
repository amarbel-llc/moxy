package native

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// blobURIPattern matches madder://blobs/<digest> substrings inside a
// command string. Markl IDs are of the form `<algo>-<encoding>`
// (e.g. `blake2b256-aZ09…`); we accept any safe URI character set.
var blobURIPattern = regexp.MustCompile(`madder://blobs/[A-Za-z0-9._-]+`)

// BlobWriter pumps bytes for a single blob into its pipe write end.
// Real implementation runs `madder cat <digest>` as a subprocess;
// tests can swap in an in-memory implementation. Lifecycle:
// Start once, Wait once, Cleanup is idempotent and best-effort.
type BlobWriter interface {
	Start() error
	Wait() error
	Cleanup()
}

// blobSource opens a pipe and prepares a writer that fills it with the
// named blob's bytes. The pipe's read end is returned for the moxin
// child's ExtraFiles; the writer is returned for parent-side
// lifecycle (Start after the child is wired up, Wait after the moxin
// tool exits). MadderBackend is a superset; substitution takes the
// narrower interface so tests can stub more cheaply.
type blobSource interface {
	OpenBlob(ctx context.Context, digest string) (readEnd *os.File, writer BlobWriter, err error)
}

// resultSubstitution is the result of rewriting madder://blobs/<digest>
// references inside a command string. The caller must:
//
//  1. Set cmd.ExtraFiles = sub.ExtraFiles before cmd.Start().
//  2. Call sub.StartWriters() after cmd.Start() (so blob bytes start
//     streaming into the pipes once the child can read from them).
//  3. defer sub.Cleanup() to release pipe ends + reap subprocesses.
//
// The first ExtraFiles entry becomes file descriptor 3 in the child,
// the second fd 4, and so on (the standard Go os/exec convention).
type resultSubstitution struct {
	Command    string
	ExtraFiles []*os.File

	// writers fill each pipe with the corresponding blob's bytes.
	// StartWriters / Cleanup own them.
	writers []BlobWriter
	// pipeReads are the parent's copies of the read ends. They are
	// passed to the child via ExtraFiles; the parent's copies must
	// be closed after Start (so the child sees EOF when the writer
	// finishes).
	pipeReads []*os.File
	// started is set by StartWriters. Cleanup uses it to decide
	// whether to drain or close.
	started bool
}

// StartWriters launches each writer (e.g. spawns a `madder cat`
// subprocess). Must be called after cmd.Start() so the child is
// already wired to the read end.
func (s *resultSubstitution) StartWriters() error {
	s.started = true
	for _, w := range s.writers {
		if err := w.Start(); err != nil {
			return err
		}
	}
	return nil
}

// WaitWriters blocks until every writer has finished pushing bytes.
// Call after the moxin tool's cmd.Wait so we surface any madder cat
// errors instead of dropping them.
func (s *resultSubstitution) WaitWriters() error {
	if !s.started {
		return nil
	}
	var firstErr error
	for _, w := range s.writers {
		if err := w.Wait(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Cleanup closes pipe handles still owned by the parent and best-effort
// reaps any background writers. Safe to call multiple times.
func (s *resultSubstitution) Cleanup() {
	for _, r := range s.pipeReads {
		_ = r.Close()
	}
	s.pipeReads = nil
	for _, w := range s.writers {
		w.Cleanup()
	}
	s.writers = nil
}

// substituteMadderURIs scans command for madder://blobs/<digest>
// substrings, opens a pipe per unique digest, and rewrites every
// reference to /dev/fd/N. Repeated references to the same digest
// share a single pipe and fd so commands like `diff X X` work
// without deadlocking or duplicating work. On error no pipe
// resources are leaked.
func substituteMadderURIs(
	ctx context.Context,
	command string,
	src blobSource,
) (*resultSubstitution, error) {
	matches := blobURIPattern.FindAllStringIndex(command, -1)
	if len(matches) == 0 {
		return &resultSubstitution{Command: command}, nil
	}

	sub := &resultSubstitution{}
	fdByDigest := make(map[string]int)

	failf := func(format string, args ...any) (*resultSubstitution, error) {
		sub.Cleanup()
		return nil, fmt.Errorf(format, args...)
	}

	var b strings.Builder
	cursor := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		uri := command[start:end]
		digest, ok := parseBlobURI(uri)
		if !ok {
			return failf("invalid blob URI: %s", uri)
		}

		fd, seen := fdByDigest[digest]
		if !seen {
			pr, writer, err := src.OpenBlob(ctx, digest)
			if err != nil {
				return failf("opening %s: %w", uri, err)
			}

			fd = 3 + len(sub.ExtraFiles)
			fdByDigest[digest] = fd
			sub.ExtraFiles = append(sub.ExtraFiles, pr)
			sub.pipeReads = append(sub.pipeReads, pr)
			sub.writers = append(sub.writers, writer)
		}

		b.WriteString(command[cursor:start])
		fmt.Fprintf(&b, "/dev/fd/%d", fd)
		cursor = end
	}
	b.WriteString(command[cursor:])

	sub.Command = b.String()
	return sub, nil
}

// openBlobBuffered reads the entire blob into memory via the source's
// OpenBlob/Start/Wait dance. Suitable for short blobs (e.g. stdin
// payloads); large outputs should be streamed via substituteMadderURIs
// instead so they never fully materialize in moxy's memory.
func openBlobBuffered(ctx context.Context, src blobSource, digest string) ([]byte, error) {
	pr, writer, err := src.OpenBlob(ctx, digest)
	if err != nil {
		return nil, err
	}
	defer pr.Close()
	if err := writer.Start(); err != nil {
		writer.Cleanup()
		return nil, err
	}
	defer writer.Cleanup()
	body, readErr := io.ReadAll(pr)
	waitErr := writer.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return body, nil
}

// parseBlobURI extracts the digest segment from a
// madder://blobs/<digest> URI. Returns ok=false for any URI that does
// not match.
func parseBlobURI(uri string) (digest string, ok bool) {
	const prefix = "madder://blobs/"
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	rest := uri[len(prefix):]
	if idx := strings.Index(rest, "?"); idx >= 0 {
		rest = rest[:idx]
	}
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return rest, true
}

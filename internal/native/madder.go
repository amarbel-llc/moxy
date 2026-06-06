package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// defaultMadderBin is set at build time via -ldflags -X. Empty in
// dev/static builds; we fall back to PATH lookup so `go run` and
// tests still work.
var defaultMadderBin = ""

// defaultStoreID is the blob-store-id passed to `madder write` /
// `madder info-repo`. The leading "." selects the CWD-relative store
// (under <repo>/.madder/local/share/blob_stores/default/), matching
// the store spinclass auto-initializes per worktree.
const defaultStoreID = ".default"

// MadderClient is a process-level handle to a madder binary. It is
// stateless and safe to share across goroutines and Server instances.
type MadderClient struct {
	bin string
}

// NewMadderClient resolves the madder binary path (build-time pin
// first, else PATH) and returns a client.
func NewMadderClient() (*MadderClient, error) {
	bin := defaultMadderBin
	if bin == "" {
		resolved, err := exec.LookPath("madder")
		if err != nil {
			return nil, fmt.Errorf(
				"madder binary not found (checked build-time pin and $PATH): %w",
				err,
			)
		}
		bin = resolved
	}
	return &MadderClient{bin: bin}, nil
}

// Bin returns the resolved madder binary path. Useful for diagnostics
// and version reporting.
func (c *MadderClient) Bin() string { return c.bin }

// VerifyDefaultStore checks that the .default store exists at the
// current working directory's `.madder/` (or an ancestor). Returns
// an actionable error naming the fix on failure.
func (c *MadderClient) VerifyDefaultStore(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, c.bin, "info-repo", defaultStoreID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"madder default store (%s) not found: %w; run `madder init %s` from the repo root",
			defaultStoreID, err, defaultStoreID,
		)
	}
	return nil
}

// writeRecord is one NDJSON record from `madder write -format json`.
type writeRecord struct {
	ID    string `json:"id"`
	Size  int64  `json:"size"`
	Error string `json:"error,omitempty"`
}

// Write streams content into the .default store and returns the
// resulting markl-id (blob digest). Equivalent to:
//
//	madder write -format json .default -
func (c *MadderClient) Write(ctx context.Context, content io.Reader) (string, error) {
	return c.WriteToStore(ctx, defaultStoreID, content)
}

// WriteToStore streams content into the named store and returns the
// resulting markl-id (blob digest). Deliberately no companion
// "EnsureStore": user-level stores are provisioned out-of-band
// (home-manager) because `madder init` with an unprefixed id from inside a
// worktree lands in the ancestor .madder, shadowing XDG scope (madder#227).
func (c *MadderClient) WriteToStore(ctx context.Context, storeID string, content io.Reader) (string, error) {
	cmd := exec.CommandContext(ctx, c.bin,
		"write", "-format", "json", storeID, "-")
	cmd.Stdin = content
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("madder write: %w (stderr: %s)", err, stderr.String())
	}

	// One blob, one NDJSON record. Decode the first record; ignore
	// trailing whitespace.
	dec := json.NewDecoder(&stdout)
	var rec writeRecord
	if err := dec.Decode(&rec); err != nil {
		return "", fmt.Errorf("parsing madder write output: %w (stdout: %q)", err, stdout.String())
	}
	if rec.Error != "" {
		return "", fmt.Errorf("madder write reported error: %s", rec.Error)
	}
	if rec.ID == "" {
		return "", errors.New("madder write returned empty id")
	}
	return rec.ID, nil
}

// CatCommand returns an *exec.Cmd that, when started, will stream the
// raw bytes of `digest` to its stdout. Caller wires Stdout to a pipe
// or buffer as needed and is responsible for Start/Wait.
func (c *MadderClient) CatCommand(ctx context.Context, digest string) *exec.Cmd {
	return exec.CommandContext(ctx, c.bin, "cat", digest)
}

// CatBytes runs `madder cat <digest>` synchronously and returns its
// stdout. Buffered — not suitable for very large blobs; callers that
// stream into a child process should use OpenBlob instead.
func (c *MadderClient) CatBytes(ctx context.Context, digest string) ([]byte, error) {
	cmd := c.CatCommand(ctx, digest)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("madder cat %s: %w (stderr: %s)", digest, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// OpenBlob implements blobSource. It opens an OS pipe and prepares a
// `madder cat <digest>` subprocess that writes to the pipe's write
// end. The returned read end is intended for the moxin child's
// ExtraFiles; the writer's Start spawns the subprocess and Wait/
// Cleanup reap it.
func (c *MadderClient) OpenBlob(ctx context.Context, digest string) (*os.File, BlobWriter, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating pipe for %s: %w", digest, err)
	}
	cmd := c.CatCommand(ctx, digest)
	cmd.Stdout = pw
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return pr, &madderCatWriter{cmd: cmd, pw: pw, stderr: &stderr, digest: digest}, nil
}

// madderCatWriter manages the lifecycle of a `madder cat <digest>`
// subprocess that writes a blob to a pipe.
type madderCatWriter struct {
	cmd     *exec.Cmd
	pw      *os.File
	stderr  *bytes.Buffer
	digest  string
	started bool
	waited  bool
}

func (w *madderCatWriter) Start() error {
	if w.started {
		return errors.New("madderCatWriter: Start called twice")
	}
	w.started = true
	if err := w.cmd.Start(); err != nil {
		_ = w.pw.Close()
		return fmt.Errorf("starting madder cat %s: %w", w.digest, err)
	}
	// Parent doesn't need its copy of the write end — the subprocess
	// has its own dup. Closing now ensures the moxin child sees EOF
	// after `madder cat` exits.
	if err := w.pw.Close(); err != nil {
		return fmt.Errorf("closing parent write end for %s: %w", w.digest, err)
	}
	w.pw = nil
	return nil
}

func (w *madderCatWriter) Wait() error {
	if !w.started || w.waited {
		return nil
	}
	w.waited = true
	if err := w.cmd.Wait(); err != nil {
		// Consumer closing the pipe early (e.g. `diff X X` short-circuits
		// on same-inode without reading) is a legitimate use case for our
		// fd-substitution flow, not a tool failure. SIGPIPE here just
		// means the moxin child didn't need all the bytes.
		if isBrokenPipe(err) {
			return nil
		}
		return fmt.Errorf("madder cat %s: %w (stderr: %s)", w.digest, err, w.stderr.String())
	}
	return nil
}

// isBrokenPipe reports whether err is the result of `madder cat`
// exiting because its stdout was closed by the reader.
func isBrokenPipe(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		return status.Signaled() && status.Signal() == syscall.SIGPIPE
	}
	return false
}

func (w *madderCatWriter) Cleanup() {
	if !w.started {
		if w.pw != nil {
			_ = w.pw.Close()
			w.pw = nil
		}
		return
	}
	if !w.waited {
		if w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
		}
		_ = w.cmd.Wait()
		w.waited = true
	}
}

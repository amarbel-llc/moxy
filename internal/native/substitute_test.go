package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// fakeMadder is a stub MadderBackend backed by an in-memory map. It is
// shared across server_test.go and substitute_test.go.
type fakeMadder struct {
	blobs map[string]string
}

func newFakeMadder() *fakeMadder {
	return &fakeMadder{blobs: make(map[string]string)}
}

// put injects content under a caller-chosen digest. Useful for tests
// that pre-populate the store and then reference the digest in tool
// arguments.
func (f *fakeMadder) put(digest, content string) {
	f.blobs[digest] = content
}

// Write hashes content with sha256 and returns a stable
// blake2b256-style digest (the prefix is decorative — these blobs
// never round-trip through real madder). Subsequent reads via
// OpenBlob/CatBytes find them by the same digest.
func (f *fakeMadder) Write(_ context.Context, content io.Reader) (string, error) {
	body, err := io.ReadAll(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	digest := "blake2b256-" + hex.EncodeToString(sum[:])
	f.blobs[digest] = string(body)
	return digest, nil
}

func (f *fakeMadder) OpenBlob(_ context.Context, digest string) (*os.File, BlobWriter, error) {
	payload, ok := f.blobs[digest]
	if !ok {
		return nil, nil, fmt.Errorf("blob %s not found", digest)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	return pr, &fakeBlobWriter{pw: pw, payload: payload}, nil
}

func (f *fakeMadder) CatBytes(_ context.Context, digest string) ([]byte, error) {
	payload, ok := f.blobs[digest]
	if !ok {
		return nil, fmt.Errorf("blob %s not found", digest)
	}
	return []byte(payload), nil
}

type fakeBlobWriter struct {
	pw      *os.File
	payload string
	started bool
	done    chan struct{}
}

func (w *fakeBlobWriter) Start() error {
	if w.started {
		return errors.New("Start called twice")
	}
	w.started = true
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		defer w.pw.Close()
		_, _ = io.WriteString(w.pw, w.payload)
	}()
	return nil
}

func (w *fakeBlobWriter) Wait() error {
	if !w.started {
		return nil
	}
	<-w.done
	return nil
}

func (w *fakeBlobWriter) Cleanup() {
	if !w.started {
		_ = w.pw.Close()
		return
	}
	<-w.done
}

func TestSubstituteNoURIs(t *testing.T) {
	src := &fakeMadder{blobs: map[string]string{}}
	sub, err := substituteMadderURIs(context.Background(), "echo hello world", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sub.Cleanup()

	if sub.Command != "echo hello world" {
		t.Errorf("command = %q, want %q", sub.Command, "echo hello world")
	}
	if len(sub.ExtraFiles) != 0 {
		t.Errorf("len(ExtraFiles) = %d, want 0", len(sub.ExtraFiles))
	}
}

func TestSubstituteSingleURI(t *testing.T) {
	src := &fakeMadder{blobs: map[string]string{
		"blake2b256-abc": "cached content",
	}}

	sub, err := substituteMadderURIs(
		context.Background(),
		"grep pattern madder://blobs/blake2b256-abc",
		src,
	)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	defer sub.Cleanup()

	if strings.Contains(sub.Command, "madder://") {
		t.Error("URI was not rewritten")
	}
	if !strings.Contains(sub.Command, "/dev/fd/3") {
		t.Errorf("expected /dev/fd/3 in command, got %q", sub.Command)
	}
	if sub.Command != "grep pattern /dev/fd/3" {
		t.Errorf("command = %q, want %q", sub.Command, "grep pattern /dev/fd/3")
	}
	if len(sub.ExtraFiles) != 1 {
		t.Errorf("len(ExtraFiles) = %d, want 1", len(sub.ExtraFiles))
	}
}

func TestSubstituteMultipleURIs(t *testing.T) {
	src := &fakeMadder{blobs: map[string]string{
		"blake2b256-aaa": "first content",
		"blake2b256-bbb": "second content",
	}}

	sub, err := substituteMadderURIs(
		context.Background(),
		"diff madder://blobs/blake2b256-aaa madder://blobs/blake2b256-bbb",
		src,
	)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	defer sub.Cleanup()

	if sub.Command != "diff /dev/fd/3 /dev/fd/4" {
		t.Errorf("command = %q, want %q", sub.Command, "diff /dev/fd/3 /dev/fd/4")
	}
	if len(sub.ExtraFiles) != 2 {
		t.Errorf("len(ExtraFiles) = %d, want 2", len(sub.ExtraFiles))
	}
}

func TestSubstituteDuplicateURI(t *testing.T) {
	src := &fakeMadder{blobs: map[string]string{
		"blake2b256-dup": "shared content",
	}}

	sub, err := substituteMadderURIs(
		context.Background(),
		"diff madder://blobs/blake2b256-dup madder://blobs/blake2b256-dup",
		src,
	)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	defer sub.Cleanup()

	if sub.Command != "diff /dev/fd/3 /dev/fd/3" {
		t.Errorf("command = %q, want %q", sub.Command, "diff /dev/fd/3 /dev/fd/3")
	}
	if len(sub.ExtraFiles) != 1 {
		t.Errorf("len(ExtraFiles) = %d, want 1 (should deduplicate)", len(sub.ExtraFiles))
	}
}

func TestSubstituteInvalidURI(t *testing.T) {
	src := &fakeMadder{blobs: map[string]string{}}
	_, err := substituteMadderURIs(
		context.Background(),
		"cat madder://blobs/blake2b256-nope",
		src,
	)
	if err == nil {
		t.Fatal("expected error for missing blob, got nil")
	}
	if !strings.Contains(err.Error(), "opening") {
		t.Errorf("error = %q, expected it to mention 'opening'", err.Error())
	}
}

package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"syscall"

	"code.linenisgreat.com/madder/go/pkgs/blob_store_env"
	"code.linenisgreat.com/madder/go/pkgs/env_dir"
	"code.linenisgreat.com/madder/go/pkgs/env_local"
	"code.linenisgreat.com/madder/go/pkgs/env_ui"
	"code.linenisgreat.com/madder/go/pkgs/madder_env"
	"code.linenisgreat.com/madder/go/pkgs/scoped_id"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	deweyerrors "code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// madderModulePath is the Go module whose build-info version `moxy version`
// reports for madder now that moxy calls the library in-process (there is no
// `madder` binary to point at anymore).
const madderModulePath = "code.linenisgreat.com/madder/go"

// defaultStoreID is the blob-store-id used for the primary cache. The leading
// "." selects the CWD-relative store (under <repo>/.madder/…), matching the
// store spinclass auto-initializes per worktree.
const defaultStoreID = ".default"

// MadderClient is a process-level handle to the madder blob-store library.
// It holds a long-lived parent context; each operation builds a fresh
// per-call errors.Context + BlobStoreEnv and runs the work inside
// errors.Context.Run, which recovers madder's panic/cancel error model into a
// returned Go error. The env is NOT shared across calls: BlobStoreEnv is
// itself an errors.Context and its stores capture that context at discovery,
// so a cancel on one call's error path would poison a shared env (madder#277
// tracks a composable-context API that would let us cache discovery safely).
type MadderClient struct {
	// parent is the base context every per-call errors.Context derives from.
	parent context.Context
}

// NewMadderClient returns a library-backed client. No binary resolution — the
// madder Go library is linked in-process.
func NewMadderClient() (*MadderClient, error) {
	return &MadderClient{parent: context.Background()}, nil
}

// Version reports the madder Go module's version from the binary's build info
// (replaces the old binary-path reporting). Returns "" if unavailable (e.g. a
// `go run` build with no module info).
func (c *MadderClient) Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range info.Deps {
		if dep.Path == madderModulePath {
			return dep.Version
		}
	}
	return ""
}

// withStoreEnv builds a fresh per-call errors.Context + BlobStoreEnv (running
// store discovery) and invokes fn inside errors.Context.Run, so any
// Cancel/panic from env construction or a store operation is recovered into
// the returned error. The env and its embedded context are discarded when
// this returns.
func (c *MadderClient) withStoreEnv(
	ctx context.Context,
	fn func(env blob_store_env.BlobStoreEnv) error,
) error {
	if ctx == nil {
		ctx = c.parent
	}
	ec := deweyerrors.MakeContext(ctx)
	return ec.Run(func(runCtx deweyerrors.Context) {
		cfg := env_dir.Config{EnvVarNames: madder_env.DefaultEnvVarNames}
		dirEnv := env_dir.MakeDefault(runCtx, cfg, "madder")
		uiEnv := env_ui.MakeDefault(runCtx)
		localEnv := env_local.Make(uiEnv, dirEnv)
		env := blob_store_env.MakeBlobStoreEnv(localEnv)
		if env.GetDefaultBlobStoreId() == "" {
			deweyerrors.ContextCancelWithErrorf(
				runCtx,
				"madder: no blob stores discovered (run `madder init %s`)",
				defaultStoreID,
			)
			return
		}
		if err := fn(env); err != nil {
			deweyerrors.ContextCancelWithError(runCtx, err)
		}
	})
}

// parseDigest converts a moxy-held digest string (e.g. "blake2b256-…") into a
// markl.Id for the library calls.
func parseDigest(digest string) (markl.Id, error) {
	var id markl.Id
	if err := id.Set(digest); err != nil {
		return id, fmt.Errorf("parsing blob digest %q: %w", digest, err)
	}
	return id, nil
}

// VerifyDefaultStore confirms the .default store is discoverable. A successful
// env build with a non-empty default id is the verification.
func (c *MadderClient) VerifyDefaultStore(ctx context.Context) error {
	return c.withStoreEnv(ctx, func(env blob_store_env.BlobStoreEnv) error {
		return nil
	})
}

// Write streams content into the .default store and returns the resulting
// markl-id (blob digest).
func (c *MadderClient) Write(ctx context.Context, content io.Reader) (string, error) {
	return c.WriteToStore(ctx, defaultStoreID, content)
}

// WriteToStore streams content into the named store and returns the resulting
// markl-id. storeID is a blob-store-id (e.g. ".default" or a user-scoped id).
func (c *MadderClient) WriteToStore(ctx context.Context, storeID string, content io.Reader) (string, error) {
	var digest string
	err := c.withStoreEnv(ctx, func(env blob_store_env.BlobStoreEnv) error {
		store := env.GetBlobStore(scoped_id.Make(storeID))
		w, err := store.MakeBlobWriter(store.GetDefaultHashType())
		if err != nil {
			return fmt.Errorf("madder write: opening writer for %q: %w", storeID, err)
		}
		if _, err := io.Copy(w, content); err != nil {
			_ = w.Close()
			return fmt.Errorf("madder write: copying content: %w", err)
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("madder write: finalizing blob: %w", err)
		}
		digest = w.GetMarklId().String()
		return nil
	})
	if err != nil {
		return "", err
	}
	return digest, nil
}

// CatBytes reads a blob by digest into memory. Buffered — not for very large
// blobs; streaming consumers use OpenBlob.
func (c *MadderClient) CatBytes(ctx context.Context, digest string) ([]byte, error) {
	id, err := parseDigest(digest)
	if err != nil {
		return nil, err
	}
	var body []byte
	err = c.withStoreEnv(ctx, func(env blob_store_env.BlobStoreEnv) error {
		reader, err := env.OpenBlob(id)
		if err != nil {
			return fmt.Errorf("madder cat %s: %w", digest, err)
		}
		defer reader.Close()
		b, err := io.ReadAll(reader)
		if err != nil {
			return fmt.Errorf("madder cat %s: reading blob: %w", digest, err)
		}
		body = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

// OpenBlob implements blobSource. It fails fast if the blob is missing in every
// store (moxy#11 — the old CLI path deferred this to a `madder cat` that could
// hang), then opens a pipe and returns its read end for the moxin child's
// ExtraFiles plus a BlobWriter that streams the blob's bytes into the pipe
// in-process (an io.Copy goroutine, replacing the old `madder cat` subprocess).
func (c *MadderClient) OpenBlob(ctx context.Context, digest string) (*os.File, BlobWriter, error) {
	id, err := parseDigest(digest)
	if err != nil {
		return nil, nil, err
	}

	// Fail fast on a missing blob: check existence (recovered via withStoreEnv)
	// before creating a pipe/child, so a missing digest errors immediately
	// instead of leaving a reader blocked on a pipe that never fills (moxy#11).
	if err := c.withStoreEnv(ctx, func(env blob_store_env.BlobStoreEnv) error {
		if !env.HasBlobInAnyStore(id) {
			return fmt.Errorf("blob not found in any store: %s", digest)
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating pipe for %s: %w", digest, err)
	}
	return pr, &libBlobWriter{client: c, ctx: ctx, id: id, digest: digest, pw: pw}, nil
}

// libBlobWriter fills a pipe with a blob's bytes via the madder library. It
// mirrors the BlobWriter lifecycle (Start once, Wait once, idempotent Cleanup)
// the fd-substitution flow in substitute.go expects.
type libBlobWriter struct {
	client *MadderClient
	ctx    context.Context
	id     markl.Id
	digest string
	pw     *os.File

	started bool
	waited  bool
	done    chan struct{}
	err     error
}

// Start launches a goroutine that opens the blob and copies its bytes into the
// pipe write end, closing pw when done so the moxin child sees EOF. Any error
// (or a copy short-circuited by the reader closing early) is captured for Wait.
func (w *libBlobWriter) Start() error {
	if w.started {
		return fmt.Errorf("libBlobWriter: Start called twice")
	}
	w.started = true
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		defer w.pw.Close()
		copyErr := w.client.withStoreEnv(w.ctx, func(env blob_store_env.BlobStoreEnv) error {
			reader, err := env.OpenBlob(w.id)
			if err != nil {
				return fmt.Errorf("madder cat %s: %w", w.digest, err)
			}
			defer reader.Close()
			if _, err := io.Copy(w.pw, reader); err != nil {
				// A consumer closing the pipe early (e.g. `diff X X`
				// short-circuits without reading) is legitimate, not a
				// tool failure — surface it as broken-pipe-tolerant.
				if isBrokenPipe(err) {
					return nil
				}
				return fmt.Errorf("madder cat %s: streaming blob: %w", w.digest, err)
			}
			return nil
		})
		w.err = copyErr
	}()
	return nil
}

// Wait blocks until the writer goroutine finishes and returns its error.
func (w *libBlobWriter) Wait() error {
	if !w.started || w.waited {
		return nil
	}
	w.waited = true
	<-w.done
	return w.err
}

// Cleanup releases the pipe write end (if Start never ran) or joins the
// goroutine (if it did). Idempotent, best-effort.
func (w *libBlobWriter) Cleanup() {
	if !w.started {
		if w.pw != nil {
			_ = w.pw.Close()
			w.pw = nil
		}
		return
	}
	if !w.waited {
		<-w.done
		w.waited = true
	}
}

// isBrokenPipe reports whether err is a write to a closed pipe — the moxin
// child (reader) closed its end before consuming all the blob's bytes (e.g.
// `diff X X` short-circuits without reading). io.Copy into the pipe surfaces
// syscall.EPIPE (often wrapped in *os.PathError), or os.ErrClosed if the fd is
// already closed.
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed)
}

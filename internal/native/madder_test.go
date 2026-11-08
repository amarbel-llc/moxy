package native

import (
	"context"
	"errors"
	"testing"
	"time"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// A digest string markl accepts; no blob with it exists anywhere.
const wellFormedDigest = "blake2b256-hachgsma4xthlhrhj9k4tnvx78ygyt0tjsc0cm2gdpwx7gnn3jwscxvauv"

// moxy#11/#431: a missing blob must make OpenBlob fail at once, before any
// pipe or child exists, instead of leaving a reader blocked on a pipe that
// never fills.
func TestOpenBlobMissingBlobFailsFast(t *testing.T) {
	errMissing := errors.New("blob not found in any store")
	client := &MadderClient{
		parent: context.Background(),
		requireBlob: func(context.Context, string, markl.Id) error {
			return errMissing
		},
	}

	type result struct {
		err    error
		reader any
		writer any
	}
	done := make(chan result, 1)
	go func() {
		r, w, err := client.OpenBlob(context.Background(), wellFormedDigest)
		var res result
		res.err = err
		if r != nil {
			res.reader = r
			_ = r.Close()
		}
		if w != nil {
			res.writer = w
		}
		done <- res
	}()

	select {
	case res := <-done:
		if !errors.Is(res.err, errMissing) {
			t.Fatalf("OpenBlob error = %v, want the missing-blob error", res.err)
		}
		if res.reader != nil || res.writer != nil {
			t.Fatalf("OpenBlob returned a pipe or writer for a missing blob")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OpenBlob did not return within 2s for a missing blob")
	}
}

func TestOpenBlobRejectsMalformedDigest(t *testing.T) {
	client := &MadderClient{parent: context.Background()}
	if _, _, err := client.OpenBlob(context.Background(), "not-a-digest"); err == nil {
		t.Fatal("OpenBlob accepted a malformed digest")
	}
}

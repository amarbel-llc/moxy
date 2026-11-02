package toolbox

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"golang.org/x/crypto/blake2b"
)

// digest writes one "<markl id>  <name>" line per input, like sha256sum but
// in markl's encoding, so a blob's blake2b256 id equals the digest in its
// madder://blobs/ URI.
func digest(algorithm string, inputs []namedReader, out io.Writer) error {
	if algorithm == "" {
		algorithm = "sha256"
	}
	for _, in := range inputs {
		var h hash.Hash
		switch algorithm {
		case "sha256":
			h = sha256.New()
		case "blake2b256":
			h, _ = blake2b.New256(nil)
		default:
			return fmt.Errorf("digest: unknown algorithm %q", algorithm)
		}
		if _, err := io.Copy(h, in.r); err != nil {
			return fmt.Errorf("digest: %s: %w", in.name, err)
		}
		var id markl.Id
		if err := id.SetMarklId(algorithm, h.Sum(nil)); err != nil {
			return fmt.Errorf("digest: encoding %s: %w", in.name, err)
		}
		if _, err := fmt.Fprintf(out, "%s  %s\n", id.String(), in.name); err != nil {
			return err
		}
	}
	return nil
}

type namedReader struct {
	name string
	r    io.Reader
}

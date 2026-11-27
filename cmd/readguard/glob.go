package main

import (
	"fmt"
	"io"
	"os"

	"code.linenisgreat.com/moxy/internal/folioglob"
)

// linkNotice is folio.glob's answer to a link it did not search. It names
// nothing: not the link's target, and not how much lies behind it.
const linkNotice = "(Not searched: at least one link met while matching leads outside path, or is absolute. To search a linked directory, pass it as path.)"

// runGlob prints the files pattern matches under dir, "" meaning the current
// directory.
func runGlob(w io.Writer, pattern, dir string) error {
	if dir == "" {
		dir = "."
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		if orig := os.Getenv("READGUARD_ORIG"); orig != "" && dir == os.Getenv("READGUARD_REPL") {
			return fmt.Errorf("not a directory: %s", orig)
		}
		return fmt.Errorf("opening %s: %w", dir, err)
	}
	defer root.Close()
	res, err := folioglob.Match(root, pattern)
	if err != nil {
		return err
	}
	for _, name := range res.Files {
		fmt.Fprintln(w, name)
	}
	switch {
	case len(res.Files) > 0:
	case res.Dirs > 0:
		fmt.Fprintf(w, "No files found (%d directories matched; glob returns files only)\n", res.Dirs)
	default:
		fmt.Fprintln(w, "No files found")
	}
	if res.LinkNotSearched {
		fmt.Fprintln(w, linkNotice)
	}
	return nil
}

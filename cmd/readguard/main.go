// Command readguard runs a read tool's script on files it opened itself
// (moxy#490; docs/plans/2026-11-11-490-read-symlink-leaves.md).
//
//	readguard exec --floor FOLIO_PERMS [--file-args N,..] [--json-files-args N,..]
//	               [--verify-args N,..] [--cwd-arg N] -- PROGRAM ARG...
//
// Each N is the 1-based position among ARG... of an argument that is a path
// (file-args), a JSON array of paths (json-files-args) or a path the program
// must open itself (verify-args). A file argument is replaced by /dev/fd/D of
// a descriptor opened without following a symlink the read policy did not
// allow; PROGRAM is then exec'd. A refusal exits 2 with the reason on stderr.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"code.linenisgreat.com/moxy/internal/readguard"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "exec" {
		fmt.Fprintln(os.Stderr, "usage: readguard exec --floor FOLIO_PERMS [flags] -- PROGRAM ARG...")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("readguard", flag.ExitOnError)
	floor := fs.String("floor", "", "folio-perms, the static read floor")
	fileArgs := fs.String("file-args", "", "1-based positions of path arguments to open")
	jsonArgs := fs.String("json-files-args", "", "1-based positions of JSON-array-of-paths arguments to open")
	verifyArgs := fs.String("verify-args", "", "1-based positions of paths to check but leave to the program")
	anyArgs := fs.String("any-args", "", "1-based positions of paths that may be directories: a file becomes /dev/fd/D, a directory is entered (fchdir) and the argument becomes \".\"")
	cwdArg := fs.Int("cwd-arg", 0, "1-based position of the working directory argument")
	_ = fs.Parse(os.Args[2:])
	if *floor == "" || fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "readguard: --floor and a program are required")
		os.Exit(2)
	}
	prog, args := fs.Arg(0), append([]string(nil), fs.Args()[1:]...)

	if err := guard(*floor, positions(*fileArgs), positions(*jsonArgs), positions(*verifyArgs), positions(*anyArgs), *cwdArg, args); err != nil {
		fmt.Fprintf(os.Stderr, "readguard: %v\n", err)
		os.Exit(2)
	}
	path, err := exec.LookPath(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readguard: %v\n", err)
		os.Exit(2)
	}
	err = syscall.Exec(path, append([]string{prog}, args...), os.Environ())
	fmt.Fprintf(os.Stderr, "readguard: exec %s: %v\n", path, err)
	os.Exit(2)
}

func positions(s string) []int {
	var out []int
	for _, f := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// guard rewrites args in place. Descriptors it opens stay open, with
// close-on-exec cleared, for the exec that follows.
//
// For an any-arg the program is told what happened through the environment:
// READGUARD_ORIG is the operand as the caller named it and READGUARD_REPL the
// text the program will see in its place, so it can map its output back
// (READGUARD_REPL is "./" for an entered directory, "/dev/fd/D" for a file).
func guard(floor string, files, jsonFiles, verify, anyPaths []int, cwdArg int, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	g := &readguard.Gate{Floor: floor}
	defer g.Close()
	o := readguard.Opener{Gate: g, Cwd: cwd}
	// the program will run in --cwd-arg's directory, so that is what its
	// relative file operands mean; the directory itself is named from here
	fileOpener := o
	if cwdArg > 0 && cwdArg <= len(args) && args[cwdArg-1] != "" {
		d := args[cwdArg-1]
		if !filepath.IsAbs(d) {
			d = filepath.Join(cwd, d)
		}
		fileOpener.Cwd = d
	}
	ctx := context.Background()

	at := func(n int) (string, bool) {
		if n > len(args) {
			return "", false
		}
		v := args[n-1]
		return v, v != "" && v != "null" && !readguard.Passthrough(v)
	}
	openFd := func(o readguard.Opener, operand string) (string, error) {
		f, err := o.Open(ctx, operand)
		if err != nil {
			return "", err
		}
		if err := keepAcrossExec(f); err != nil {
			return "", err
		}
		return fmt.Sprintf("/dev/fd/%d", f.Fd()), nil
	}

	for _, n := range verify {
		if v, ok := at(n); ok {
			if err := o.Verify(ctx, v); err != nil {
				return err
			}
		}
	}
	var enter *os.File // the directory to fchdir into once everything is opened
	for _, n := range anyPaths {
		v, ok := at(n)
		if !ok {
			continue
		}
		f, err := o.OpenAny(ctx, v)
		if err != nil {
			return err
		}
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		os.Setenv("READGUARD_ORIG", v)
		if fi.IsDir() {
			enter = f
			args[n-1] = "."
			os.Setenv("READGUARD_REPL", "./")
			continue
		}
		if err := keepAcrossExec(f); err != nil {
			return err
		}
		args[n-1] = fmt.Sprintf("/dev/fd/%d", f.Fd())
		os.Setenv("READGUARD_REPL", args[n-1])
	}
	for _, n := range files {
		if v, ok := at(n); ok {
			fd, err := openFd(o, v)
			if err != nil {
				return err
			}
			args[n-1] = fd
		}
	}
	for _, n := range jsonFiles {
		v, ok := at(n)
		if !ok {
			continue
		}
		var list []string
		if json.Unmarshal([]byte(v), &list) != nil {
			continue // not an array of strings: the program rejects it
		}
		for i, operand := range list {
			if readguard.Passthrough(operand) || strings.HasPrefix(operand, "-") {
				continue
			}
			if list[i], err = openFd(fileOpener, operand); err != nil {
				return err
			}
		}
		rewritten, _ := json.Marshal(list)
		args[n-1] = string(rewritten)
	}
	if enter != nil {
		return syscall.Fchdir(int(enter.Fd()))
	}
	return nil
}

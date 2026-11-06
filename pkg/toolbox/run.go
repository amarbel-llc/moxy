package toolbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// RunOptions wires a Run to its caller's world.
type RunOptions struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Cwd resolves relative file operands.
	Cwd string
	// Canonical maps an absolute path to the form the permission gate judges
	// (pkg/perms.CanonicalPath): symlinks above the leaf resolved, the leaf
	// kept as named. Identity when nil.
	Canonical func(string) string
	// Check is asked about each file operand's canonical path before it is
	// opened, so a refused path is never opened, not even to learn whether
	// it exists. A non-nil error refuses the whole command. Required.
	Check func(ctx context.Context, path string) error
	// LookPath resolves an applet's program; exec.LookPath when nil.
	LookPath func(string) (string, error)
}

// Run executes a parsed pipeline. File operands are opened by Run itself,
// after Check allowed them, without following any symlink, and reach the
// applet's program only as inherited descriptors (/dev/fd/N); programs run
// with an environment of LC_ALL=C.UTF-8 alone.
func Run(ctx context.Context, p *Pipeline, o RunOptions) error {
	if o.Check == nil {
		return errors.New("toolbox: no permission check configured")
	}
	var opened []*os.File
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	stageFiles := make([][]*os.File, len(p.Stages))
	for i, st := range p.Stages {
		if !st.Applet.FileOperands {
			continue
		}
		for _, operand := range st.Operands {
			if operand == "-" {
				continue
			}
			f, err := o.openOperand(ctx, operand)
			if err != nil {
				return err
			}
			opened = append(opened, f)
			stageFiles[i] = append(stageFiles[i], f)
		}
	}
	return o.start(ctx, p, stageFiles)
}

func (o RunOptions) openOperand(ctx context.Context, operand string) (*os.File, error) {
	if rest, ok := strings.CutPrefix(operand, "/dev/fd/"); ok {
		return inheritedFd(operand, rest)
	}
	if strings.HasPrefix(operand, "madder://") {
		return nil, fmt.Errorf("%s: blob URI was not substituted; call toolbox through moxy", operand)
	}
	path := operand
	if !filepath.IsAbs(path) {
		path = filepath.Join(o.Cwd, path)
	}
	if o.Canonical != nil {
		path = o.Canonical(path)
	}
	if err := o.Check(ctx, path); err != nil {
		return nil, fmt.Errorf("%s: %w", operand, err)
	}
	f, err := openNoSymlinks(path)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s: refusing to follow a symlink", operand)
		}
		return nil, fmt.Errorf("%s: %w", operand, err)
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("not a regular file")
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", operand, err)
	}
	return f, nil
}

// inheritedFd takes a descriptor moxy passed in for a substituted blob URI.
// It is duplicated so that two operands naming the same blob close
// independently.
func inheritedFd(operand, n string) (*os.File, error) {
	fd, err := strconv.Atoi(n)
	if err != nil || fd < 3 {
		return nil, fmt.Errorf("%s: only descriptors moxy passed in (3 and up) may be named", operand)
	}
	dup, err := syscall.Dup(fd)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operand, err)
	}
	syscall.CloseOnExec(dup)
	return os.NewFile(uintptr(dup), operand), nil
}

func (o RunOptions) start(ctx context.Context, p *Pipeline, stageFiles [][]*os.File) error {
	lookPath := o.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	programs := make([]string, len(p.Stages))
	for i, st := range p.Stages {
		if st.Applet.Program == "" {
			continue
		}
		path, err := lookPath(st.Applet.Program)
		if err != nil {
			return fmt.Errorf("%s: %w", st.Applet.Name, err)
		}
		programs[i] = path
	}
	stderr := &lockedWriter{w: o.Stderr}
	var (
		waits []func() error
		in    = o.Stdin
		inR   *os.File // the previous stage's pipe, which this process must close
	)
	if in == nil {
		in = strings.NewReader("")
	}
	// fail closes this process's pipe ends, so stages already running see
	// EOF or EPIPE and finish, then waits for them.
	fail := func(err error, open ...*os.File) error {
		for _, f := range open {
			if f != nil {
				f.Close()
			}
		}
		for _, w := range waits {
			_ = w()
		}
		return err
	}
	for i, st := range p.Stages {
		var (
			out    = o.Stdout
			pr, pw *os.File
			err    error
		)
		if i < len(p.Stages)-1 {
			if pr, pw, err = os.Pipe(); err != nil {
				return fail(err, inR)
			}
			out = pw
		}
		name := st.Applet.Name
		if st.Applet.Program == "" {
			done := make(chan error, 1)
			go func(st Stage, in io.Reader, inR, pw *os.File, files []*os.File) {
				err := runInProcess(st, in, out, files)
				if pw != nil {
					pw.Close()
				}
				if inR != nil {
					inR.Close()
				}
				done <- err
			}(st, in, inR, pw, stageFiles[i])
			waits = append(waits, func() error { return <-done })
		} else {
			argv := append(append([]string{}, st.Args...), "--")
			if st.Applet.FileOperands {
				j := 0
				for _, operand := range st.Operands {
					if operand == "-" {
						argv = append(argv, "-")
						continue
					}
					argv = append(argv, fmt.Sprintf("/dev/fd/%d", 3+j))
					j++
				}
			} else {
				argv = append(argv, st.Operands...)
			}
			cmd := exec.CommandContext(ctx, programs[i], argv...)
			cmd.Env = []string{"LC_ALL=C.UTF-8"}
			cmd.Dir = o.Cwd
			cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, stderr
			cmd.ExtraFiles = stageFiles[i]
			if err := cmd.Start(); err != nil {
				return fail(fmt.Errorf("%s: %w", name, err), inR, pr, pw)
			}
			if pw != nil {
				pw.Close()
			}
			if inR != nil {
				inR.Close()
			}
			waits = append(waits, func() error {
				if err := cmd.Wait(); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				return nil
			})
		}
		in, inR = pr, pr
	}
	var first error
	for _, w := range waits {
		if err := w(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func runInProcess(st Stage, in io.Reader, out io.Writer, files []*os.File) error {
	if strings.HasPrefix(st.Applet.Name, stringApplet.Name+" ") {
		return runString(st, in, out)
	}
	var inputs []namedReader
	j := 0
	for _, operand := range st.Operands {
		if operand == "-" {
			inputs = append(inputs, namedReader{"-", in})
			continue
		}
		inputs = append(inputs, namedReader{operand, files[j]})
		j++
	}
	if len(inputs) == 0 {
		inputs = []namedReader{{"-", in}}
	}
	switch st.Applet.Name {
	case "digest":
		return digest(st.Flags["algorithm"], inputs, out)
	case stringsApplet.Name:
		return runStrings(st, inputs, out)
	}
	return fmt.Errorf("%s: no in-process implementation", st.Applet.Name)
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return len(p), nil
	}
	return l.w.Write(p)
}

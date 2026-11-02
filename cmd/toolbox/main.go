// Command toolbox runs toolbox commands for the toolbox moxin (FDR 0016,
// moxy#500). See toolbox(1).
//
//	toolbox run --floor FOLIO_PERMS -- COMMAND    run COMMAND, stdin to stdout
//	toolbox perms --floor FOLIO_PERMS -- COMMAND  the moxin's static floor
//	toolbox applets                               list the applets
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"code.linenisgreat.com/moxy/pkg/perms"
	"code.linenisgreat.com/moxy/pkg/policybroker"
	"code.linenisgreat.com/moxy/pkg/toolbox"
)

// brokerDialWait bounds the wait for the broker's hello, as the hook's.
const brokerDialWait = 250 * time.Millisecond

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		floor, command := commandArgs(os.Args[2:])
		os.Exit(run(floor, command))
	case "perms":
		floor, command := commandArgs(os.Args[2:])
		os.Exit(floorVerdict(floor, command, os.Stdout))
	case "applets":
		for _, a := range toolbox.Applets() {
			fmt.Println(a.Name)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: toolbox run|perms --floor FOLIO_PERMS -- COMMAND | toolbox applets")
	os.Exit(2)
}

func commandArgs(args []string) (floor, command string) {
	fs := flag.NewFlagSet("toolbox", flag.ExitOnError)
	fs.StringVar(&floor, "floor", "", "folio-perms, the static read floor")
	_ = fs.Parse(args)
	if floor == "" || fs.NArg() != 1 {
		usage()
	}
	return floor, fs.Arg(0)
}

// floorVerdict is the moxin's [dynamic-perms] command: a command that does
// not parse is denied (exit 2), so an unparseable command can never come out
// allowed; otherwise folio-perms read judges exactly the parsed file
// operands, and its exit code (0 allow, 1 ask, 2 deny) is the verdict.
func floorVerdict(floor, command string, out io.Writer) int {
	p, err := toolbox.Parse(command)
	if err != nil {
		fmt.Fprintf(out, "toolbox: %v\n", err)
		return 2
	}
	files := p.Files()
	if len(files) == 0 {
		fmt.Fprintln(out, "no file operands")
		return 0
	}
	cmd := exec.Command(floor, append([]string{"read"}, files...)...)
	cmd.Stdout, cmd.Stderr = out, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintf(out, "toolbox: floor: %v\n", err)
		return 2
	}
	return 0
}

func run(floor, command string) int {
	p, err := toolbox.Parse(command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "toolbox: %v\n", err)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "toolbox: %v\n", err)
		return 2
	}
	g := &gate{floor: floor}
	defer g.close()
	err = toolbox.Run(context.Background(), p, toolbox.RunOptions{
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Canonical: perms.CanonicalPath,
		Check:     g.check,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "toolbox: %v\n", err)
		return 1
	}
	return 0
}

// gate re-asks, at open time, the question the PreToolUse hook asked: the
// policy broker when one answers, else the static floor. A broker that is
// down or hangs is no answer and falls to the floor, never to allow. Only a
// deny refuses here: an ask was the hook's to put (to the user, or refused
// under dontAsk) before the call reached toolbox, and nothing here can ask.
type gate struct {
	floor  string
	once   sync.Once
	client *policybroker.Client
}

func (g *gate) check(ctx context.Context, path string) error {
	g.once.Do(func() {
		c := policybroker.FromEnv(os.Getenv, nil, "moxy-toolbox")
		if c == nil {
			return
		}
		wait, cancel := context.WithTimeout(ctx, brokerDialWait)
		defer cancel()
		c.WaitConnected(wait)
		g.client = c
	})
	if g.client != nil {
		dec, reason, answered := g.client.Check(ctx, perms.Read, perms.Resource{Kind: perms.KindFile, URI: "file://" + path})
		if answered {
			if dec == perms.Deny {
				return fmt.Errorf("policy broker denies: %s", reason)
			}
			return nil
		}
	}
	out, err := exec.Command(g.floor, "read", path).CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return nil // ask: the hook's to put, already put
	}
	return fmt.Errorf("denied: %s", strings.TrimSpace(string(out)))
}

func (g *gate) close() {
	if g.client != nil {
		g.client.Close()
	}
}

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

	"code.linenisgreat.com/moxy/internal/readguard"
	"code.linenisgreat.com/moxy/pkg/toolbox"
)

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
	// readguard re-asks the gate at open time (the broker when one answers,
	// else the floor), opens without following a symlink the policy did not
	// allow, and follows one whose resolved target it does (moxy#490).
	g := &readguard.Gate{Floor: floor}
	defer g.Close()
	err = toolbox.Run(context.Background(), p, toolbox.RunOptions{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Cwd:    cwd,
		Open:   readguard.Opener{Gate: g, Cwd: cwd}.Open,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "toolbox: %v\n", err)
		return 1
	}
	return 0
}

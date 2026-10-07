package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"code.linenisgreat.com/purse-first/libs/go-mcp/command"

	"code.linenisgreat.com/moxy/internal/config"
	"code.linenisgreat.com/moxy/internal/narrowing"
)

// parentMoxyfileEnv names the rendered effective moxyfile of whoever spawned
// this moxy. When set, moxy serves only a config that narrows it — see
// moxyfile(5) NARROWING and docs/features/0013-moxyfile-narrowing.md.
const parentMoxyfileEnv = "MOXY_PARENT_MOXYFILE"

// Exit codes of `moxy narrows`. A caller must be able to tell "this child
// widens" from "I could not read what you gave me".
const (
	exitNarrows    = 0
	exitWidens     = 1
	exitUnreadable = 2
)

// wideningError is a refusal to serve: the effective moxyfile grants more than
// the parent reference allows. Its message carries one line per violation so
// the refusal is legible wherever it surfaces — moxy's stderr at startup, the
// restart tool's result on reload.
type wideningError struct {
	parentPath string
	violations []narrowing.Violation
}

func (e *wideningError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "effective moxyfile does not narrow %s (%s):", parentMoxyfileEnv, e.parentPath)
	for _, v := range e.violations {
		b.WriteString("\n")
		b.WriteString(v.String())
	}
	return b.String()
}

// loadServingConfig reads the effective moxyfile for the current directory
// and, when MOXY_PARENT_MOXYFILE is set, refuses it unless it narrows that
// parent. Every path that turns a moxyfile into running tools goes through
// here — startup, a full reload, a single-moxin reload — so editing the
// moxyfile and calling `restart` cannot widen a running moxy.
//
// The config is read once, judged in its literal form, and only then has its
// header $VARs expanded: what is served is exactly what was judged.
func loadServingConfig() (config.Config, error) {
	hierarchy, err := config.LoadDefaultHierarchyLiteral()
	if err != nil {
		return config.Config{}, err
	}

	if parentPath := os.Getenv(parentMoxyfileEnv); parentPath != "" {
		parent, err := config.LoadFileLiteral(parentPath)
		if err != nil {
			return config.Config{}, fmt.Errorf("%s: %w", parentMoxyfileEnv, err)
		}
		if violations := narrowing.Check(parent.Merged, hierarchy.Merged); len(violations) > 0 {
			return config.Config{}, &wideningError{parentPath: parentPath, violations: violations}
		}
	}

	return config.ExpandHeaders(hierarchy.Merged), nil
}

// loadServingConfigForReload is loadServingConfig for the paths that run
// inside an already-serving moxy. Their error goes back to the MCP client as a
// tool result; the extra stderr line puts the refusal in moxy's own log too.
func loadServingConfigForReload() (config.Config, error) {
	cfg, err := loadServingConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: reload refused, keeping the running configuration: %v\n", err)
	}
	return cfg, err
}

func addNarrowingCommands(app *command.App) {
	app.AddCommand(&command.Command{
		Name: "narrows",
		Description: command.Description{
			Short: "Check that a child moxyfile grants no more than a parent",
			Long: "Resolves --parent and --child to effective moxyfiles and reports " +
				"every way the child grants more than the parent. A directory is " +
				"walked as if it were the working directory (global moxyfile, each " +
				"directory from $HOME down to it, and includes); a file is that file " +
				"plus its includes and nothing else, which is how a file written by " +
				"show-effective-moxyfile is read. Prints one '<key>: <reason>' line " +
				"per violation, sorted by key then subject, to stdout. Exits 0 when " +
				"the child narrows the parent, 1 when it does not, 2 when either " +
				"side cannot be read. See moxyfile(5) NARROWING.",
		},
		Params: []command.Param{
			{
				Name:        "parent",
				Type:        command.String,
				Description: "Parent reference: a moxyfile, or a directory to walk.",
				Required:    true,
			},
			{
				Name:        "child",
				Type:        command.String,
				Description: "Child reference: a moxyfile, or a directory to walk.",
				Required:    true,
			},
			{
				Name:        "json",
				Type:        command.Bool,
				Description: "Print {\"narrows\":bool,\"violations\":[{\"key\",\"subject\",\"message\"}]} instead of lines.",
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				Parent string `json:"parent"`
				Child  string `json:"child"`
				JSON   bool   `json:"json"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			os.Exit(runNarrows(args.Parent, args.Child, args.JSON))
			return nil
		},
	})

	app.AddCommand(&command.Command{
		Name: "show-effective-moxyfile",
		Description: command.Description{
			Short: "Print the effective merged moxyfile as canonical TOML",
			Long: "Merges the moxyfile hierarchy for the working directory (or --dir) " +
				"and prints the result as one include-free moxyfile: fixed key order, " +
				"sorted lists and servers, commands in array form. The same " +
				"configuration always prints the same bytes. Header values keep " +
				"their $VAR references — nothing is expanded — so the output is safe " +
				"to write to disk. A spawner captures this at spawn time and hands " +
				"it to a child moxy as MOXY_PARENT_MOXYFILE. See moxyfile(5) NARROWING.",
		},
		Params: []command.Param{
			{
				Name:        "dir",
				Type:        command.String,
				Description: "Directory to walk as if it were the working directory. Default: the working directory.",
				Default:     "",
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				Dir string `json:"dir"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			return showEffectiveMoxyfile(args.Dir)
		},
	})
}

func runNarrows(parentRef, childRef string, asJSON bool) int {
	if parentRef == "" || childRef == "" {
		fmt.Fprintln(os.Stderr, "moxy: narrows requires --parent and --child")
		return exitUnreadable
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: getting home dir: %v\n", err)
		return exitUnreadable
	}
	parent, err := config.LoadReferenceLiteral(home, parentRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: --parent: %v\n", err)
		return exitUnreadable
	}
	child, err := config.LoadReferenceLiteral(home, childRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: --child: %v\n", err)
		return exitUnreadable
	}

	violations := narrowing.Check(parent.Merged, child.Merged)

	if asJSON {
		report := struct {
			Narrows    bool                  `json:"narrows"`
			Violations []narrowing.Violation `json:"violations"`
		}{
			Narrows:    len(violations) == 0,
			Violations: append([]narrowing.Violation{}, violations...),
		}
		out, err := json.Marshal(report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "moxy: encoding report: %v\n", err)
			return exitUnreadable
		}
		fmt.Println(string(out))
	} else {
		for _, v := range violations {
			fmt.Println(v.String())
		}
	}

	if len(violations) > 0 {
		return exitWidens
	}
	return exitNarrows
}

func showEffectiveMoxyfile(dir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return fmt.Errorf("getting cwd: %w", err)
		}
	}
	hierarchy, err := config.LoadReferenceLiteral(home, dir)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(config.RenderEffective(hierarchy.Merged))
	return err
}

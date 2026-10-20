package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"code.linenisgreat.com/purse-first/libs/go-mcp/command"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
	"code.linenisgreat.com/moxy/pkg/narrowing"
)

// parentMoxyfileEnv names the rendered effective moxyfile of whoever spawned
// this moxy. When set, moxy serves only a config that narrows it — see
// moxyfile(5) NARROWING and docs/features/0013-moxyfile-narrowing.md. The
// serve entry points load it with moxy.LoadParentReference and hand it to
// the library as Options.Parent (FDR 0014); the judging lives in pkg/moxy.
const parentMoxyfileEnv = "MOXY_PARENT_MOXYFILE"

// Exit codes of `moxy narrows`. A caller must be able to tell "this child
// widens" from "I could not read what you gave me".
const (
	exitNarrows    = 0
	exitWidens     = 1
	exitUnreadable = 2
)

// resolvedMoxinsFor reports the moxins a moxy serving cfg from dir would
// discover, by name and source directory: MOXIN_PATH (or the default
// hierarchy under dir), then the system moxins unless cfg turns them off.
func resolvedMoxinsFor(cfg moxyfile.Config, dir string) (map[string]string, error) {
	return native.ResolvedMoxinSources(
		os.Getenv("MOXIN_PATH"),
		dir,
		native.SystemMoxinDirFor(cfg.BuiltinNative),
	)
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
				"show-effective-moxyfile is read. A directory is also given the " +
				"resolved-moxins record a moxy served from it would discover in this " +
				"environment; a file keeps the record it carries. Tools are judged " +
				"at three levels, none < pass < use: every tool the child holds in " +
				"allow-tools (use) or pass-tools (pass) must be one the parent holds " +
				"in either, so a child may use a tool its parent only passes. A " +
				"refused tool is reported as '<key>: child holds <tool> at <level>, " +
				"parent at none'. Prints one " +
				"'<key>: <reason>' line per violation, sorted by key then subject, " +
				"to stdout. Exits 0 when the child narrows the parent, 1 when it " +
				"does not, 2 when either side cannot be read or the arguments are " +
				"malformed. See moxyfile(5) NARROWING.",
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
				"to write to disk. allow-tools and pass-tools are written as the " +
				"hierarchy merged them, per tool at the lowest level any layer " +
				"holds it. Rendering a directory also records, under " +
				"resolved-moxins, the directory each moxin name resolves to in the " +
				"current environment; rendering a file reproduces the record it " +
				"carries. A spawner captures this at spawn time and hands " +
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
	parent, err := loadEffectiveReference(home, parentRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: --parent: %v\n", err)
		return exitUnreadable
	}
	child, err := loadEffectiveReference(home, childRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: --child: %v\n", err)
		return exitUnreadable
	}

	violations := narrowing.Check(parent, child)

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
	cfg, err := loadEffectiveReference(home, dir)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(moxyfile.RenderEffective(cfg))
	return err
}

// loadEffectiveReference resolves a reference the way both verbs read one. A
// directory is a live configuration: its hierarchy is walked and the moxins it
// resolves to right now, in this environment, are recorded — the same thing a
// moxy served from it would discover. A file is already a document: it is
// taken as written, record included, never recomputed, so rendering a rendered
// file changes nothing.
func loadEffectiveReference(home, ref string) (moxyfile.Config, error) {
	hierarchy, err := moxyfile.LoadReferenceLiteral(home, ref)
	if err != nil {
		return moxyfile.Config{}, err
	}
	cfg := hierarchy.Merged
	if info, err := os.Stat(ref); err == nil && info.IsDir() {
		abs, err := filepath.Abs(ref)
		if err != nil {
			return moxyfile.Config{}, err
		}
		if cfg.ResolvedMoxins, err = resolvedMoxinsFor(cfg, abs); err != nil {
			return moxyfile.Config{}, fmt.Errorf("discovering moxins: %w", err)
		}
	}
	return cfg, nil
}

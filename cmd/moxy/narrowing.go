package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"code.linenisgreat.com/purse-first/libs/go-mcp/command"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
	"code.linenisgreat.com/moxy/pkg/narrowing"
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

// servingConfig is an effective moxyfile on its way to being served: read
// once, in its literal form, together with the parent reference it must
// narrow. Every path that turns a moxyfile into running tools — startup, a
// full reload, a single-moxin reload — loads one, calls refuseUnlessNarrows
// with the moxins it is about to serve, and only then takes expanded(). So
// editing the moxyfile and calling `restart` cannot widen a running moxy, and
// what is served is exactly what was judged.
type servingConfig struct {
	literal    moxyfile.Config
	parentPath string          // "" when MOXY_PARENT_MOXYFILE is unset
	parent     moxyfile.Config // meaningful only when parentPath != ""
}

func loadServingConfig() (servingConfig, error) {
	hierarchy, err := moxyfile.LoadDefaultHierarchyLiteral()
	if err != nil {
		return servingConfig{}, err
	}
	ref := pinnedParentReference()
	if ref.err != nil {
		return servingConfig{}, ref.err
	}
	return servingConfig{literal: hierarchy.Merged, parentPath: ref.path, parent: ref.cfg}, nil
}

type parentReference struct {
	path string // "" when MOXY_PARENT_MOXYFILE is unset
	cfg  moxyfile.Config
	err  error
}

// pinnedParentReference reads MOXY_PARENT_MOXYFILE once per process and
// returns that same reading ever after. The reference is what every reload is
// judged against; were it re-read, whoever can write the file — possibly the
// very agent being confined — could widen it and then call `restart`.
var pinnedParentReference = sync.OnceValue(readParentReference)

// startupAllowTools pins, once per process, the allow-tools set this process
// was actually serving right after the first successful judging that decides
// what is served — bootstrap()'s own first call, genuine startup, before any
// `restart` has run (moxy#461); never loadJudgedConfigAtStartup's separate,
// earlier preflight read, which exists only to unlock credential setup and
// is not what ends up served (see refuseUnlessNarrows's pin parameter). A
// later reload's refuseUnlessNarrows compares against the pinned set via
// narrowing.CheckReload: a tool the parent reference holds at pass may reach
// use only if it was already there at startup, so the operator's own choice
// is what a reload can never exceed for such a tool. A plain sync.OnceValue
// cannot express this: its zero-ish "not yet pinned" state would be
// indistinguishable from the perfectly meaningful nil AllowToolSet
// (unrestricted at startup), so pinned is tracked explicitly instead.
var startupAllowTools struct {
	mu     sync.Mutex
	pinned bool
	set    moxyfile.AllowToolSet
}

func pinStartupAllowTools(set moxyfile.AllowToolSet) {
	startupAllowTools.mu.Lock()
	defer startupAllowTools.mu.Unlock()
	if startupAllowTools.pinned {
		return
	}
	startupAllowTools.pinned, startupAllowTools.set = true, set
}

func readStartupAllowTools() (pinned bool, set moxyfile.AllowToolSet) {
	startupAllowTools.mu.Lock()
	defer startupAllowTools.mu.Unlock()
	return startupAllowTools.pinned, startupAllowTools.set
}

func readParentReference() parentReference {
	path := os.Getenv(parentMoxyfileEnv)
	if path == "" {
		return parentReference{}
	}
	fail := func(err error) parentReference {
		return parentReference{path: path, err: fmt.Errorf("%s: %w", parentMoxyfileEnv, err)}
	}
	// An empty file parses as an empty config, which as a parent would
	// constrain almost nothing. It is far more likely a render that failed
	// than a reference anyone meant, so it is refused rather than obeyed.
	// A relative path would name a different file from every working
	// directory, the child's own included.
	if !filepath.IsAbs(path) {
		return fail(fmt.Errorf("%s is not an absolute path", path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return fail(fmt.Errorf("%s is empty", path))
	}
	parent, err := moxyfile.LoadFileLiteral(path)
	if err != nil {
		return fail(err)
	}
	cfg := parent.Merged
	// A reference with no moxin record says nothing about which moxins the
	// parent had. For a process about to serve, "nothing" must not mean "any":
	// the default MOXIN_PATH includes the working directory's own
	// .moxy/moxins. So an absent record is read as an empty one, and a child
	// that reaches any moxin under such a reference is refused.
	if cfg.ResolvedMoxins == nil {
		cfg.ResolvedMoxins = map[string]string{}
	}
	return parentReference{path: path, cfg: cfg}
}

// judgesMoxins reports whether the parent reference bounds which moxins may
// be served, i.e. whether working out the moxin set is worth it before
// refuseUnlessNarrows.
func (sc servingConfig) judgesMoxins() bool {
	return sc.parentPath != "" && sc.parent.ResolvedMoxins != nil
}

// refuseUnlessNarrows judges the config against the parent reference. moxins
// is the name-to-directory set the caller is about to serve; it stands in for
// whatever resolved-moxins record the moxyfile itself carries, which is never
// trusted. A nil moxins skips the moxin rule and is only right when
// judgesMoxins is false.
//
// This is the one choke point every path that turns a moxyfile into running
// tools shares (see servingConfig's own doc comment), so it is also the one
// place that checks the startup allow-tools snapshot (moxy#461). pin says
// whether THIS call, if it succeeds, is the moment to record that snapshot
// (the first time it hasn't been yet) — true from bootstrap() and
// moxinReloaderImpl.ReloadMoxin, the calls that decide what is actually
// served; false from loadJudgedConfigAtStartup, a preflight read for
// credential setup alone, so its own earlier, separate read of the same
// moxyfile never becomes the permanent baseline in bootstrap's place.
func (sc servingConfig) refuseUnlessNarrows(moxins map[string]string, pin bool) error {
	if sc.parentPath == "" {
		return nil
	}
	judged := sc.literal
	judged.ResolvedMoxins = moxins
	pinned, startup := readStartupAllowTools()
	if violations := narrowing.CheckReload(sc.parent, judged, pinned, startup); len(violations) > 0 {
		return &wideningError{parentPath: sc.parentPath, violations: violations}
	}
	if pin && !pinned {
		pinStartupAllowTools(judged.BuildAllowToolSet())
	}
	return nil
}

// expanded is the config to serve: the judged text with header $VARs expanded.
func (sc servingConfig) expanded() moxyfile.Config {
	return moxyfile.ExpandHeaders(sc.literal)
}

// loadJudgedConfigAtStartup loads and judges the moxyfile before anything is
// started, against the moxins discovery finds in this environment. bootstrap
// judges again against the moxins it actually loads; this earlier pass exists
// so a refusal is the first thing a launcher sees.
func loadJudgedConfigAtStartup() (moxyfile.Config, error) {
	sc, err := loadServingConfig()
	if err != nil {
		return moxyfile.Config{}, err
	}
	var moxins map[string]string
	if sc.judgesMoxins() {
		cwd, err := os.Getwd()
		if err != nil {
			return moxyfile.Config{}, err
		}
		if moxins, err = resolvedMoxinsFor(sc.literal, cwd); err != nil {
			return moxyfile.Config{}, fmt.Errorf("discovering moxins: %w", err)
		}
	}
	if err := sc.refuseUnlessNarrows(moxins, false); err != nil {
		return moxyfile.Config{}, err
	}
	return sc.expanded(), nil
}

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

// logReloadRefused records a failed reload in moxy's own log. The error also
// goes back to the MCP client as the restart tool's result; either way the
// running configuration is left as it was.
func logReloadRefused(err error) {
	fmt.Fprintf(os.Stderr, "moxy: reload refused, keeping the running configuration: %v\n", err)
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"code.linenisgreat.com/purse-first/libs/go-mcp/command"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
	"github.com/google/uuid"

	"code.linenisgreat.com/moxy/internal/add"
	"code.linenisgreat.com/moxy/internal/credentials"
	"code.linenisgreat.com/moxy/internal/diaglog"
	"code.linenisgreat.com/moxy/internal/hook"
	"code.linenisgreat.com/moxy/internal/naming"
	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/status"
)

// version and commit are set at build time via -ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

// printVersionTable renders the hybrid version output specified by
// eng-versioning(7): a self-identification line, a blank line, then a
// COMPONENT/VERSION/REV table listing every build-time-pinned external
// dep (currently: madder). Pin rows probe the pinned binary at runtime,
// so a missing or broken pin produces an "(error: …)" cell rather than
// aborting the whole command. moxy always pins madder, so it is a
// with-components binary per eng-versioning(7) and always emits the
// table; the spec's self-line-only shape is for binaries that pin
// nothing downstream.
func printVersionTable(ctx context.Context, moxyVersion string) error {
	fmt.Fprintf(os.Stdout, "moxy %s\n\n", moxyVersion)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tVERSION\tREV")

	madderClient, err := native.NewMadderClient()
	if err != nil {
		fmt.Fprintf(w, "madder\t(error: %v)\t-\n", err)
	} else if v := madderClient.Version(); v != "" {
		// The madder Go library is linked in-process; its version comes from
		// build info (there is no binary path to show anymore).
		fmt.Fprintf(w, "madder\t%s\tlibrary\n", v)
	} else {
		fmt.Fprintf(w, "madder\t(unknown)\tlibrary\n")
	}

	return w.Flush()
}

func newApp() *command.App {
	app := command.NewApp("moxy", "MCP proxy that aggregates child MCP servers")
	app.Version = version + "+" + commit
	app.MCPArgs = []string{"serve", "mcp"}
	if b := os.Getenv("MOXY_MCP_BINARY"); b != "" {
		app.MCPBinary = b
	}
	app.Description.Long = "Moxy spawns child MCP servers as subprocesses, communicates with them " +
		"via JSON-RPC over stdio, and presents their tools, resources, and prompts " +
		"through a single unified MCP server. Child server capabilities are namespaced " +
		"with a dot separator (e.g. grit.status, folio.read). Configuration is loaded " +
		"from a hierarchy of TOML moxyfiles: global (~/.config/moxy/moxyfile), " +
		"per-directory, and project-local."

	app.Examples = []command.Example{
		{
			Description: "Start the MCP proxy server",
			Command:     "moxy serve mcp",
		},
		{
			Description: "Show config hierarchy, moxins, and validation",
			Command:     "moxy status",
		},
		{
			Description: "Interactively add a server to the local moxyfile",
			Command:     "moxy add",
		},
	}

	app.AddCommand(&command.Command{
		Name: "serve-mcp",
		Description: command.Description{
			Short: "Run as MCP proxy server over stdio",
			Long: "Loads the moxyfile hierarchy, spawns child MCP servers, performs " +
				"initialize handshakes, probes ephemeral servers, and serves as a " +
				"unified MCP server on stdin/stdout. Shuts down gracefully on SIGINT/SIGTERM.",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return runServer(app, transportStdio, "", "", "", "")
		},
	})

	app.AddCommand(&command.Command{
		Name: "serve-http",
		Description: command.Description{
			Short: "Run as MCP proxy server over streamable HTTP",
			Long: "Loads the moxyfile hierarchy, spawns child MCP servers, serves " +
				"/healthz and /mcp over streamable HTTP, and shuts down on " +
				"SIGINT/SIGTERM. With no --listen flag, binds an ephemeral port on " +
				"127.0.0.1 and prints a clown-plugin-protocol handshake line to " +
				"stdout (for clown-plugin-host). With --listen ADDR, binds that " +
				"fixed address instead and logs the bound address to stderr only — " +
				"use this to put moxy behind a reverse proxy or tunnel that needs a " +
				"stable upstream. STARTUP: a fixed --listen address is bound before " +
				"child servers are spawned, so a restart never refuses connections " +
				"(which a tunnel surfaces as a 502). Until every child has " +
				"finished starting, /healthz answers 503 {\"status\":\"starting\"} and " +
				"every other request (initialize, tools/list, /clown/*) is held on " +
				"its accepted connection, then served once startup completes, so " +
				"the initialize instructions are complete; a request held longer " +
				"than 90s gets a 503 with Retry-After. The ephemeral-port " +
				"(clown-plugin) mode binds after startup, since its handshake line " +
				"announces a ready server.",
		},
		Params: []command.Param{
			{
				Name:        "listen",
				Type:        command.String,
				Description: "Fixed TCP address to bind, e.g. 127.0.0.1:8731. When omitted, binds an ephemeral 127.0.0.1 port and prints the clown-plugin handshake.",
				Default:     "",
			},
			{
				Name:        "expose",
				Type:        command.String,
				Description: "Tool-exposure filter for strict/public frontends, e.g. 'resources-only' or '-meta'. Comma-separated profiles (full, no-meta, resources-only) and +/-{child,resource-bridge,meta} toggles. Default (empty) exposes everything. See docs/features/0006-tool-exposure-filter.md.",
				Default:     "",
			},
			{
				Name:        "name-template",
				Type:        command.String,
				Description: "Template for advertised tool/prompt names using {server} and {tool}, e.g. '{server}_{tool}' for claude.ai-safe dot-free names. Default '{server}.{tool}'. {tool} is required; omitting {server} risks cross-server collisions (rejected at startup). See docs/features/0007-name-template.md.",
				Default:     "",
			},
			{
				Name:        "permissions",
				Type:        command.String,
				Description: "Serve-time permission mode: 'delegate' (default: a Claude Code hook in front of moxy decides) or 'enforce' (moxy resolves every call's perms-request itself and refuses anything but allow, for clients with no hook). Tightens the moxyfile's permission-mode; it cannot loosen it. See moxyfile(5) and docs/features/0015-serve-time-permission-enforcement.md.",
				Default:     "",
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				Listen       string `json:"listen"`
				Expose       string `json:"expose"`
				NameTemplate string `json:"name-template"`
				Permissions  string `json:"permissions"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			return runServer(app, transportHTTP, args.Listen, args.Expose, args.NameTemplate, args.Permissions)
		},
	})

	app.AddCommand(&command.Command{
		Name: "add",
		Description: command.Description{
			Short: "Interactively add a server to a moxyfile",
			Long: "Opens a terminal form to configure a new MCP server entry " +
				"(name, command, annotations) and appends it to the specified moxyfile.",
		},
		Params: []command.Param{
			{
				Name:        "path",
				Type:        command.String,
				Description: "Path to the moxyfile to modify",
				Default:     "moxyfile",
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			path := args.Path
			if path == "" {
				path = "moxyfile"
			}
			credStore := credentials.NewStore(nil) // add command uses default keychain
			return add.Run(path, credStore)
		},
	})

	app.AddCommand(&command.Command{
		Name: "moxin-path",
		Description: command.Description{
			Short: "Print default MOXIN_PATH from legacy hierarchy",
			Long: "Computes a colon-separated MOXIN_PATH by probing the legacy directory " +
				"hierarchy (.moxy/moxins/ in cwd, intermediate parents, global config, " +
				"and system moxins). Only existing directories are included. " +
				"Usage: export MOXIN_PATH=$(moxy moxin-path)",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("getting home dir: %w", err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting cwd: %w", err)
			}
			systemDir := native.SystemMoxinDir()
			fmt.Println(native.DefaultMoxinPath(home, cwd, systemDir))
			return nil
		},
	})

	app.AddCommand(&command.Command{
		Name: "version",
		Description: command.Description{
			Short: "Print moxy build version and any build-time-pinned tools",
			Long: "Prints the moxy version+commit burnt in via -ldflags at " +
				"build time, plus a row for each external tool that was " +
				"pinned alongside (currently: madder). Mirrors `spinclass " +
				"version` so the audit trail is consistent.",
		},
		Annotations: &protocol.ToolAnnotations{
			ReadOnlyHint: boolPtr(true),
		},
		RunCLI: func(ctx context.Context, _ json.RawMessage) error {
			return printVersionTable(ctx, app.Version)
		},
	})

	app.AddCommand(&command.Command{
		Name: "status",
		Description: command.Description{
			Short: "Show all configured servers and moxins with their sources",
			Long: "Loads the moxyfile hierarchy and discovers moxins from MOXIN_PATH, " +
				"shows each config level with its servers and moxins, validates TOML " +
				"syntax, server commands, and moxin configs. Exit code 1 on validation failure.",
		},
		Annotations: &protocol.ToolAnnotations{
			ReadOnlyHint: boolPtr(true),
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("getting home dir: %w", err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting cwd: %w", err)
			}
			os.Exit(status.Run(os.Stdout, home, cwd))
			return nil
		},
		Run: func(_ context.Context, _ json.RawMessage, _ command.Prompter) (*command.Result, error) {
			home, err := os.UserHomeDir()
			if err != nil {
				return command.TextErrorResult(fmt.Sprintf("getting home dir: %v", err)), nil
			}
			cwd, err := os.Getwd()
			if err != nil {
				return command.TextErrorResult(fmt.Sprintf("getting cwd: %v", err)), nil
			}
			text, err := status.Format(home, cwd)
			if err != nil {
				return command.TextErrorResult(err.Error()), nil
			}
			return command.TextResult(text), nil
		},
	})

	app.AddCommand(&command.Command{
		Name: "list-moxins",
		Description: command.Description{
			Short: "List available builtin moxins",
			Long: "Enumerates moxins from the system moxin directory and MOXIN_PATH, " +
				"printing each name and description. Useful for discovering which " +
				"moxins can be served standalone via 'moxy serve-moxin'.",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return listMoxins()
		},
	})

	app.AddCommand(&command.Command{
		Name: "serve-moxin",
		Description: command.Description{
			Short: "Serve a single builtin moxin as a standalone MCP server",
			Long: "Looks up a moxin by name from the builtin system moxins, " +
				"then serves it as a standalone MCP server over stdio. " +
				"No proxy, no moxyfile — just the named moxin's tools. " +
				"Use this to run individual moxins without the full moxy proxy.",
		},
		Params: []command.Param{
			{
				Name:        "name",
				Type:        command.String,
				Description: "Name of the builtin moxin to serve",
				Required:    true,
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			if args.Name == "" {
				return fmt.Errorf("moxin name is required")
			}
			return runMoxinServer(args.Name)
		},
	})

	app.AddCommand(&command.Command{
		Name: "render",
		Description: command.Description{
			Short: "Render or reverse-resolve a namespaced tool/prompt name under a template",
			Long: "Forward (default): prints the advertised name for --server/--tool " +
				"under --name-template. Reverse (--resolve <rendered>): prints the " +
				"canonical \"server<TAB>tool\" via a structural inverse, which works " +
				"only for invertible templates (exactly one {server} and one {tool} " +
				"separated by a literal). render does NOT introspect a running " +
				"server — pass the same --name-template you gave serve-http. " +
				"See docs/features/0007-name-template.md.",
		},
		Params: []command.Param{
			{
				Name:        "name-template",
				Type:        command.String,
				Description: "Template using {server} and {tool}. Default '{server}.{tool}'.",
				Default:     "",
			},
			{
				Name:        "server",
				Type:        command.String,
				Description: "Server name (forward direction).",
				Default:     "",
			},
			{
				Name:        "tool",
				Type:        command.String,
				Description: "Tool or prompt name (forward direction).",
				Default:     "",
			},
			{
				Name:        "resolve",
				Type:        command.String,
				Description: "Reverse-resolve this rendered name to 'server<TAB>tool' instead of rendering forward.",
				Default:     "",
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				NameTemplate string `json:"name-template"`
				Server       string `json:"server"`
				Tool         string `json:"tool"`
				Resolve      string `json:"resolve"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			tmpl, err := naming.Parse(args.NameTemplate)
			if err != nil {
				return fmt.Errorf("--name-template: %w", err)
			}
			if args.Resolve != "" {
				if !tmpl.Invertible() {
					return fmt.Errorf("cannot resolve %q: template %q is not invertible without the live server set", args.Resolve, tmpl)
				}
				server, tool, ok := tmpl.Resolve(args.Resolve)
				if !ok {
					return fmt.Errorf("cannot resolve %q under template %q", args.Resolve, tmpl)
				}
				fmt.Printf("%s\t%s\n", server, tool)
				return nil
			}
			if args.Server == "" || args.Tool == "" {
				return fmt.Errorf("forward render requires --server and --tool (or use --resolve)")
			}
			fmt.Println(tmpl.Render(args.Server, args.Tool))
			return nil
		},
	})

	addNarrowingCommands(app)

	app.AddCommand(&command.Command{
		Name: "hook",
		Description: command.Description{
			Short: "Handle MCP hook protocol",
		},
		Hidden: true,
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return hook.Handle(app, os.Stdin, os.Stdout)
		},
	})

	app.AddCommand(&command.Command{
		Name: "show-claude-plugin-path",
		Description: command.Description{
			Short: "Print the path to the Claude Code plugin directory",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			dir, err := hook.PluginDir()
			if err != nil {
				return err
			}
			fmt.Println(dir)
			return nil
		},
	})

	app.AddCommand(&command.Command{
		Name: "install-claude-plugin",
		Description: command.Description{
			Short: "Install the moxy Claude Code plugin via claude CLI",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			dir, err := hook.PluginDir()
			if err != nil {
				return err
			}
			cmd := exec.Command("claude", "plugin", "install", dir)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
	})

	return app
}

func main() {
	// The binary keeps every diagnostic log it always had (debug, moxin.<pid>,
	// lifecycle, hook, streamhttp under $XDG_LOG_HOME/moxy); the packages no
	// longer open them at init, so an embedder of pkg/moxy writes none unless
	// it enables them itself (FDR 0014).
	diaglog.Enable(diaglog.DefaultLogHome())

	app := newApp()

	// install-mcp and generate-plugin use App methods directly.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "install-mcp":
			if err := app.InstallMCP(); err != nil {
				log.Fatalf("installing MCP: %v", err)
			}
			if err := hook.InstallSettingsHook(); err != nil {
				log.Fatalf("installing hook: %v", err)
			}
			return
		case "generate-plugin":
			if err := app.HandleGeneratePlugin(os.Args[2:], os.Stdout); err != nil {
				log.Fatalf("generating plugin: %v", err)
			}
			return
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := app.RunCLI(ctx, os.Args[1:], command.StubPrompter{}); err != nil {
		fmt.Fprintf(os.Stderr, "moxy: %v\n", err)
		// `narrows` reserves exit 1 for "the child widens". An error that
		// never reached the comparison — a flag with no value, say — must
		// not be mistaken for that verdict by a caller reading exit codes.
		if len(os.Args) > 1 && os.Args[1] == "narrows" {
			os.Exit(exitUnreadable)
		}
		os.Exit(1)
	}
}

// resolveSessionID returns a session identifier and its source label.
// Fallback chain: CLAUDE_SESSION_ID > SPINCLASS_SESSION_ID > generated UUID v7.
// Values from env vars are sanitized to be safe as both path segments and URI
// segments (only [A-Za-z0-9._-] retained).
func resolveSessionID() (id, source string) {
	for _, env := range []string{"CLAUDE_SESSION_ID", "SPINCLASS_SESSION_ID"} {
		if v := sanitizeSessionSegment(os.Getenv(env)); v != "" {
			return v, env
		}
	}
	return uuid.Must(uuid.NewV7()).String(), "generated"
}

// sanitizeSessionSegment strips characters outside [A-Za-z0-9._-] so the
// result is safe as a single path segment and URI segment.
func sanitizeSessionSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func boolPtr(b bool) *bool { return &b }

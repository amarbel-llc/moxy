package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
	"github.com/google/uuid"

	"github.com/amarbel-llc/moxy/internal/add"
	"github.com/amarbel-llc/moxy/internal/asyncjob"
	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/credentials"
	"github.com/amarbel-llc/moxy/internal/hook"
	"github.com/amarbel-llc/moxy/internal/mcpclient"
	"github.com/amarbel-llc/moxy/internal/native"
	"github.com/amarbel-llc/moxy/internal/oauth"
	"github.com/amarbel-llc/moxy/internal/permcheck"
	"github.com/amarbel-llc/moxy/internal/proxy"
	"github.com/amarbel-llc/moxy/internal/status"
	"github.com/amarbel-llc/moxy/internal/stderrlog"
	"github.com/amarbel-llc/moxy/internal/streamhttp"
)

// version and commit are set at build time via -ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

// asyncResultStoreID is the user-level (XDG) madder store async tool
// results are written to. Unprefixed ids resolve under
// $XDG_DATA_HOME/madder/blob_stores/, so results survive worktrees and
// sessions — see docs/features/0004-async-tool-dispatch.md.
const asyncResultStoreID = "moxy-async"

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
	} else {
		madderVer, err := madderVersion(ctx, madderClient.Bin())
		if err != nil {
			fmt.Fprintf(w, "madder\t(error: %v)\t%s\n", err, madderClient.Bin())
		} else {
			fmt.Fprintf(w, "madder\t%s\t%s\n", madderVer, madderClient.Bin())
		}
	}

	return w.Flush()
}

// madderVersion runs `<bin> version` and returns the trimmed first
// non-empty line of stdout.
func madderVersion(ctx context.Context, bin string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line, nil
		}
	}
	return "", fmt.Errorf("empty version output")
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
			return runServer(app, transportStdio)
		},
	})

	app.AddCommand(&command.Command{
		Name: "serve-http",
		Description: command.Description{
			Short: "Run as MCP proxy server over streamable HTTP",
			Long: "Loads the moxyfile hierarchy, spawns child MCP servers, binds an " +
				"ephemeral port on 127.0.0.1, prints a clown-plugin-protocol handshake " +
				"line to stdout, serves /healthz and /mcp, and shuts down on SIGINT/SIGTERM.",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return runServer(app, transportHTTP)
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
		os.Exit(1)
	}
}

type transportMode int

const (
	transportStdio transportMode = iota
	transportHTTP
)

func runServer(app *command.App, mode transportMode) error {
	// Redirect os.Stderr (including Go panic traces) to a per-session log
	// file so crashes that bypass the normal logging path can be recovered
	// after the fact. Rotate on clean return so a leftover entry in
	// active/ indicates the process was killed before it could shut down.
	if err := stderrlog.Init(app.Version); err != nil {
		log.Printf("stderrlog init: %v", err)
	}
	defer stderrlog.Rotate()

	// Resolve a session ID for native server cache scoping.
	// Fallback chain: CLAUDE_SESSION_ID > SPINCLASS_SESSION_ID > generated UUID.
	sessionID, sessionSource := resolveSessionID()
	fmt.Fprintf(os.Stderr, "moxy: session %s (from %s)\n", sessionID, sessionSource)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Build the credential store from a one-shot config read so the connect
	// closure has its dependencies resolved up front. Reload re-reads the
	// hierarchy on each call but the credential store is stable for the
	// process lifetime.
	hierarchyForCreds, err := config.LoadDefaultHierarchy()
	if err != nil {
		return err
	}
	credStore := credentials.NewStore(hierarchyForCreds.Merged.Credentials)

	connectServer := func(ctx context.Context, srvCfg config.ServerConfig) (proxy.ServerBackend, *protocol.InitializeResultV1, error) {
		if srvCfg.IsHTTP() {
			return connectHTTPServer(ctx, srvCfg, credStore)
		}
		exe, args := srvCfg.EffectiveCommand()
		return mcpclient.SpawnAndInitialize(ctx, srvCfg.Name, exe, args)
	}

	moxinPath := os.Getenv("MOXIN_PATH")

	madderClient, err := native.NewMadderClient()
	if err != nil {
		return fmt.Errorf("madder runtime: %w", err)
	}
	if err := madderClient.VerifyDefaultStore(ctx); err != nil {
		return fmt.Errorf("madder default store: %w", err)
	}

	bootInputs := bootstrapInputs{
		moxinPath: moxinPath,
		sessionID: sessionID,
		connect:   connectServer,
		madder:    madderClient,
	}

	bootRes, err := bootstrap(ctx, bootInputs)
	if err != nil {
		return err
	}
	cfg := bootRes.cfg
	children := bootRes.children
	failed := bootRes.failed
	activeServers := bootRes.activeServers
	systemDir := bootRes.systemDir

	p := proxy.New(children, failed, activeServers, cfg.Ephemeral, cfg.ProgressiveDisclosure, connectServer)
	p.SetMadderClient(madderClient)
	p.SetSessionID(sessionID)
	p.SetMoxinReloader(&moxinReloaderImpl{
		moxinPath: moxinPath,
		systemDir: systemDir,
	})
	// Capture systemDir from the first bootstrap so reload uses the same
	// resolved value (cfg.BuiltinNative may flip; we still want the same
	// system dir until the user opts out).
	bootInputs.systemDir = systemDir
	p.SetBootstrapper(&bootstrapperImpl{inputs: bootInputs})

	builtinRegistry := server.NewToolRegistryV1()
	app.RegisterMCPToolsV1(builtinRegistry)
	builtinRegistry.Register(
		protocol.ToolV1{
			Name:        "restart",
			Description: "Restart a configured child server or moxin by name. Omit `server` to reload the moxyfile hierarchy and MOXIN_PATH and rebuild every running child. Permission-gated (destructive). Note: Claude Code does not refresh the active conversation's tool list on notifications/tools/list_changed; follow with /mcp -> Reconnect to update the system prompt. See moxy-restart(7).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Server or moxin name. Omit to reload everything."}}}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:    boolPtr(false),
				DestructiveHint: boolPtr(true),
			},
		},
		p.HandleRestart,
	)
	resolver, resolverErr := permcheck.NewResolver()
	if resolverErr != nil {
		fmt.Fprintf(os.Stderr, "moxy: building permission resolver: %v\n", resolverErr)
	} else {
		p.SetResolver(resolver)
	}
	builtinRegistry.Register(
		protocol.ToolV1{
			Name:        "batch",
			Description: "Run a sequence of moxin sub-calls under a single permission prompt. Each sub-call must resolve to allow or ask via moxy's perms-request system; deny or unknown aborts the batch. Output is TAP-NDJSON. See moxy-batch(7). With async=true the whole batch backgrounds as ONE async job (allow-only preflight; the agent is woken on completion and fetches the TAP-NDJSON via async-result).",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["calls"],
				"properties":{
					"calls":{
						"type":"array",
						"minItems":1,
						"items":{
							"type":"object",
							"required":["tool"],
							"properties":{
								"tool":{"type":"string","description":"Namespaced tool name (e.g. grit.tag)"},
								"args":{"type":"object","description":"Sub-call arguments"}
							}
						}
					},
					"on_error":{"type":"string","enum":["stop","continue"],"default":"stop"},
					"async":{"type":"boolean","default":false,"description":"Background the batch as one async job; every sub-call must resolve to allow"}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:    boolPtr(false),
				DestructiveHint: boolPtr(true),
			},
		},
		p.HandleBatch,
	)

	// Async dispatch (FDR 0004): results go to the user-level madder store
	// so they survive worktrees and sessions. The store is provisioned by
	// home-manager — moxy only writes, NEVER creates (madder#227: init from
	// inside a worktree lands in the ancestor .madder, shadowing XDG scope).
	// Until the store exists, async degrades gracefully: jobs still reach
	// terminal states; only the digest is missing from wake messages.
	asyncManager := asyncjob.New(asyncjob.Options{
		WriteResult: func(ctx context.Context, content []byte) (string, error) {
			return madderClient.WriteToStore(ctx, asyncResultStoreID, bytes.NewReader(content))
		},
	})
	p.SetAsyncManager(asyncManager)
	builtinRegistry.Register(
		protocol.ToolV1{
			Name:        "async",
			Description: "Dispatch one tool call in the background. Returns {job_id, status:\"running\"} immediately; when the call reaches a terminal state the agent is woken via clown's job-wakeup channel with a summary, the result blob digest, and a result-ref pointing at async-result. Only calls whose permission resolves to allow may background (ask/deny/unknown are rejected synchronously). A job that exceeds its timeout (the optional per-call `timeout`, else the 30-minute server default) is killed — whole process tree — and terminalizes with status `timeout`. See docs/features/0004-async-tool-dispatch.md.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["tool"],
				"properties":{
					"tool":{"type":"string","description":"Namespaced tool name (e.g. rg.search)"},
					"args":{"type":"object","description":"Arguments for the tool call"},
					"timeout":{"type":"string","description":"Max wall-clock duration before the job's whole process tree is killed and it terminalizes with status \"timeout\" (e.g. \"10m\", \"90s\"). Defaults to the server max runtime (30m)."}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:    boolPtr(false),
				DestructiveHint: boolPtr(true),
			},
		},
		p.HandleAsync,
	)
	builtinRegistry.Register(
		protocol.ToolV1{
			Name:        "async-result",
			Description: "Fetch an async job's status, or its full stored tool result once terminal. For a running job it also surfaces live progress when the clown output spool is available — elapsed_sec, last_activity, spool_bytes, and a bounded output tail — so you can tell a working job from a wedged one without waiting for the terminal wake. Doubles as the poll surface when job wakeups are disabled (CLOWN_DISABLE_JOB_WAKEUP=1).",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["job_id"],
				"properties":{
					"job_id":{"type":"string","description":"Job id returned by async"}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:   boolPtr(true),
				IdempotentHint: boolPtr(true),
			},
		},
		p.HandleAsyncResult,
	)
	builtinRegistry.Register(
		protocol.ToolV1{
			Name:        "async-cancel",
			Description: "Cancel a running async job. The job reaches terminal state `cancelled` and wakes the agent like any other terminal state. Cancelling an already-terminal job is a no-op reporting that state.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["job_id"],
				"properties":{
					"job_id":{"type":"string","description":"Job id returned by async"}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:   boolPtr(false),
				IdempotentHint: boolPtr(true),
			},
		},
		p.HandleAsyncCancel,
	)
	p.SetBuiltinTools(builtinRegistry)

	// Wire notification forwarding for startup children
	for _, c := range children {
		c.Client.SetOnNotification(p.ForwardNotification)
	}

	p.ProbeEphemeral(ctx)

	summaries := p.CollectServerSummaries(ctx)
	instructions := proxy.FormatInstructions(summaries)

	switch mode {
	case transportHTTP:
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			for _, c := range children {
				c.Client.Close()
			}
			return fmt.Errorf("binding ephemeral port: %w", err)
		}
		fmt.Fprintf(os.Stdout, "1|1|tcp|%s|streamable-http\n", ln.Addr())
		fmt.Fprintf(os.Stderr, "moxy: serving streamable-http on %s (clown-plugin-protocol)\n", ln.Addr())
		return runHTTPServerOnListener(ctx, ln, p, children, instructions)

	default:
		if httpAddr := os.Getenv("MOXY_HTTP_ADDR"); httpAddr != "" {
			return runHTTPServer(ctx, httpAddr, p, children, instructions)
		}

		fmt.Fprintf(os.Stderr, "moxy: serving stdio\n")
		t := transport.NewStdio(os.Stdin, os.Stdout)
		p.SetNotifier(t.Write)

		srv, err := server.New(t, server.Options{
			ServerName:    "moxy",
			ServerVersion: version,
			Instructions:  instructions,
			Tools:         p,
			Resources:     p,
			Prompts:       p,
		})
		if err != nil {
			for _, c := range children {
				c.Client.Close()
			}
			return err
		}

		err = srv.Run(ctx)

		// Interrupt in-flight async jobs BEFORE tearing down children so
		// each emits a terminal `done` (interrupted) while its dispatch
		// path is still alive — no job left open in the clown journal.
		p.SweepAsyncJobs()

		closeChildrenWithDeadline(children, time.Second)

		return err
	}
}

func runHTTPServer(ctx context.Context, addr string, p *proxy.Proxy, children []proxy.ChildEntry, instructions string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	fmt.Fprintf(os.Stderr, "moxy: serving streamable-http on %s (MOXY_HTTP_ADDR)\n", ln.Addr())
	return runHTTPServerOnListener(ctx, ln, p, children, instructions)
}

func runHTTPServerOnListener(ctx context.Context, ln net.Listener, p *proxy.Proxy, children []proxy.ChildEntry, instructions string) error {
	httpSrv := streamhttp.New(streamhttp.Options{
		Tools:         p,
		Resources:     p,
		Prompts:       p,
		ServerName:    "moxy",
		ServerVersion: version,
		Instructions:  instructions,
	})
	p.SetNotifier(httpSrv.Notify)

	httpServer := &http.Server{
		Handler: httpSrv,
	}

	// Clown grants up to 5s between SIGTERM and SIGKILL. Budget: 3s for HTTP
	// graceful shutdown, 1s for child teardown, ~1s slack for stderrlog
	// rotation and runtime exit.
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			httpServer.Close()
		}
	}()

	err := httpServer.Serve(ln)

	// Same ordering as the stdio path: interrupt async jobs while their
	// dispatch path is still alive, then tear down children.
	p.SweepAsyncJobs()

	closeChildrenWithDeadline(children, time.Second)

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func closeChildrenWithDeadline(children []proxy.ChildEntry, deadline time.Duration) {
	done := make(chan struct{})
	go func() {
		for _, c := range children {
			c.Client.Close()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(deadline):
		fmt.Fprintf(os.Stderr, "moxy: child teardown exceeded %s; continuing shutdown\n", deadline)
	}
}

func connectHTTPServer(ctx context.Context, cfg config.ServerConfig, store credentials.Store) (*mcpclient.Client, *protocol.InitializeResultV1, error) {
	var opts []mcpclient.HTTPTransportOption

	// Static headers
	if len(cfg.Headers) > 0 {
		opts = append(opts, mcpclient.WithHeaders(cfg.Headers))
	}

	// Dynamic headers from helper command
	if cfg.HeadersHelper != nil {
		dynamicHeaders, err := runHeadersHelper(*cfg.HeadersHelper)
		if err != nil {
			return nil, nil, fmt.Errorf("headers-helper %q: %w", *cfg.HeadersHelper, err)
		}
		opts = append(opts, mcpclient.WithHeaders(dynamicHeaders))
	}

	// OAuth: load cached token
	if cfg.OAuth != nil {
		tok, err := store.Read(cfg.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "moxy: no cached OAuth token for %s: %v\n", cfg.Name, err)
			fmt.Fprintf(os.Stderr, "moxy: run 'moxy add' to authenticate %s\n", cfg.Name)
		} else if tok.Valid() {
			opts = append(opts, mcpclient.WithBearerToken(tok.AccessToken))
		} else if tok.RefreshToken != "" {
			clientID := ""
			if cfg.OAuth != nil {
				clientID = cfg.OAuth.ClientID
			}
			newTok, err := refreshOAuthToken(ctx, cfg.URL, clientID, tok.RefreshToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "moxy: token refresh failed for %s: %v\n", cfg.Name, err)
				fmt.Fprintf(os.Stderr, "moxy: run 'moxy add' to re-authenticate %s\n", cfg.Name)
			} else {
				if writeErr := store.Write(cfg.Name, newTok); writeErr != nil {
					fmt.Fprintf(os.Stderr, "moxy: warning: could not cache refreshed token for %s: %v\n", cfg.Name, writeErr)
				}
				opts = append(opts, mcpclient.WithBearerToken(newTok.AccessToken))
			}
		}
	}

	t := mcpclient.NewHTTPTransport(cfg.URL, opts...)
	return mcpclient.ConnectAndInitialize(ctx, cfg.Name, t)
}

func runHeadersHelper(command string) (map[string]string, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty headers-helper command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var headers map[string]string
	if err := json.Unmarshal(out, &headers); err != nil {
		return nil, fmt.Errorf("parsing headers-helper output as JSON: %w", err)
	}
	return headers, nil
}

func refreshOAuthToken(ctx context.Context, serverURL, clientID, refreshToken string) (credentials.Token, error) {
	return oauth.RefreshToken(ctx, serverURL, clientID, refreshToken)
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

// moxinReloaderImpl implements proxy.MoxinReloader. It re-reads the
// moxyfile hierarchy on each call so manual `disable-moxins` edits and
// `[[servers]]` collisions made between proxy boot and the restart call
// are honored.
type moxinReloaderImpl struct {
	moxinPath string
	systemDir string
}

func (r *moxinReloaderImpl) ReloadMoxin(name string) (*native.NativeConfig, error) {
	hierarchy, err := config.LoadDefaultHierarchy()
	if err != nil {
		return nil, fmt.Errorf("re-loading moxyfile hierarchy: %w", err)
	}
	cfg := hierarchy.Merged

	for _, srv := range cfg.Servers {
		if srv.Name == name {
			return nil, fmt.Errorf("name %q is owned by a [[servers]] entry, not a moxin", name)
		}
	}

	disable := cfg.BuildDisableMoxinSet()
	if disable.ServerDisabled(name) {
		return nil, fmt.Errorf("moxin %q is disabled by moxyfile", name)
	}

	configs, err := native.DiscoverConfigs(r.moxinPath, r.systemDir)
	if err != nil {
		return nil, fmt.Errorf("discovering moxins: %w", err)
	}

	for _, nc := range configs {
		if nc.Name != name {
			continue
		}
		filtered := nc.Tools[:0]
		for _, t := range nc.Tools {
			if disable.ToolDisabled(nc.Name, t.Name) {
				continue
			}
			filtered = append(filtered, t)
		}
		nc.Tools = filtered
		return nc, nil
	}

	return nil, fmt.Errorf("moxin %q not found in MOXIN_PATH or system moxin dir", name)
}

var (
	_ server.ToolProviderV1     = (*proxy.Proxy)(nil)
	_ server.ResourceProviderV1 = (*proxy.Proxy)(nil)
	_ server.PromptProviderV1   = (*proxy.Proxy)(nil)
)

package main

import (
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
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
	"github.com/google/uuid"

	"github.com/amarbel-llc/moxy/internal/add"
	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/credentials"
	"github.com/amarbel-llc/moxy/internal/hook"
	"github.com/amarbel-llc/moxy/internal/mcpclient"
	"github.com/amarbel-llc/moxy/internal/native"
	"github.com/amarbel-llc/moxy/internal/oauth"
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

func newApp() *command.App {
	app := command.NewApp("moxy", "MCP proxy that aggregates child MCP servers")
	app.Version = version + "+" + commit
	app.MCPArgs = []string{"serve", "mcp"}
	app.Description.Long = "Moxy spawns child MCP servers as subprocesses, communicates with them " +
		"via JSON-RPC over stdio, and presents their tools, resources, and prompts " +
		"through a single unified MCP server. Child server capabilities are namespaced " +
		"with a dot separator (e.g. grit.status, lux.hover). Configuration is loaded " +
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

	hierarchy, err := config.LoadDefaultHierarchy()
	if err != nil {
		return err
	}

	cfg := hierarchy.Merged

	for _, srv := range cfg.Servers {
		if srv.Name == "" {
			return fmt.Errorf("server has no name")
		}
		if srv.Command.IsEmpty() && !srv.IsHTTP() {
			return fmt.Errorf("server %q has no command or url", srv.Name)
		}
	}

	credStore := credentials.NewStore(cfg.Credentials)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// connectServer handles both stdio and HTTP servers.
	connectServer := func(ctx context.Context, srvCfg config.ServerConfig) (proxy.ServerBackend, *protocol.InitializeResultV1, error) {
		if srvCfg.IsHTTP() {
			return connectHTTPServer(ctx, srvCfg, credStore)
		}
		exe, args := srvCfg.EffectiveCommand()
		return mcpclient.SpawnAndInitialize(ctx, srvCfg.Name, exe, args)
	}

	var children []proxy.ChildEntry
	var failed []proxy.FailedServer
	for _, srvCfg := range cfg.Servers {
		if srvCfg.IsEphemeral(cfg.Ephemeral) {
			fmt.Fprintf(os.Stderr, "moxy: %s configured as ephemeral (on-demand)\n", srvCfg.Name)
			continue
		}

		client, result, err := connectServer(ctx, srvCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "moxy: failed to start %s: %v\n", srvCfg.Name, err)
			failed = append(failed, proxy.FailedServer{
				Name:  srvCfg.Name,
				Error: err.Error(),
			})
			continue
		}

		children = append(children, proxy.ChildEntry{
			Client:       client,
			Config:       srvCfg,
			Capabilities: result.Capabilities,
			ServerInfo:   result.ServerInfo,
			Instructions: result.Instructions,
		})

		fmt.Fprintf(os.Stderr, "moxy: connected to %s (%s %s)\n",
			srvCfg.Name, result.ServerInfo.Name, result.ServerInfo.Version)
	}

	// Discover moxins from MOXIN_PATH.
	// Moxin configs are additive — moxyfile servers win on name collision.
	var systemDir string
	if cfg.BuiltinNative == nil || *cfg.BuiltinNative {
		systemDir = native.SystemMoxinDir()
		fmt.Fprintf(os.Stderr, "moxy: bootstrap: builtin-native enabled, systemDir=%q\n", systemDir)
	} else {
		fmt.Fprintf(os.Stderr, "moxy: bootstrap: builtin-native DISABLED (cfg.BuiltinNative=%v)\n", *cfg.BuiltinNative)
	}

	moxinPath := os.Getenv("MOXIN_PATH")
	fmt.Fprintf(os.Stderr, "moxy: bootstrap: MOXIN_PATH=%q\n", moxinPath)
	nativeConfigs, err := native.DiscoverConfigs(moxinPath, systemDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: warning: moxin discovery: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "moxy: bootstrap: discovered %d moxin configs\n", len(nativeConfigs))
	for i, nc := range nativeConfigs {
		fmt.Fprintf(os.Stderr, "moxy: bootstrap:   [%d] name=%q tools=%d source=%q\n", i, nc.Name, len(nc.Tools), nc.SourceDir)
		for j, t := range nc.Tools {
			fmt.Fprintf(os.Stderr, "moxy: bootstrap:     tool[%d] %q (cmd=%q)\n", j, t.Name, t.Command)
		}
	}

	existingNames := make(map[string]bool)
	for _, c := range children {
		existingNames[c.Config.Name] = true
	}
	for _, f := range failed {
		existingNames[f.Name] = true
	}
	for _, s := range cfg.Servers {
		existingNames[s.Name] = true
	}
	fmt.Fprintf(os.Stderr, "moxy: bootstrap: existing server names (collision set): %v\n", existingNames)

	disableSet := cfg.BuildDisableMoxinSet()

	for _, nc := range nativeConfigs {
		if existingNames[nc.Name] {
			fmt.Fprintf(os.Stderr, "moxy: skipping moxin %q (name collision with moxyfile server)\n", nc.Name)
			continue
		}
		if disableSet.ServerDisabled(nc.Name) {
			fmt.Fprintf(os.Stderr, "moxy: skipping moxin %q (disabled by moxyfile)\n", nc.Name)
			continue
		}

		// Filter individual disabled tools.
		filtered := nc.Tools[:0]
		for _, t := range nc.Tools {
			if disableSet.ToolDisabled(nc.Name, t.Name) {
				fmt.Fprintf(os.Stderr, "moxy: disabling tool %s.%s (disabled by moxyfile)\n", nc.Name, t.Name)
				continue
			}
			filtered = append(filtered, t)
		}
		nc.Tools = filtered

		srv := native.NewServer(nc)
		initResult := srv.InitializeResult()
		hasCaps := initResult.Capabilities.Tools != nil
		fmt.Fprintf(os.Stderr, "moxy: bootstrap: moxin %q initResult.Capabilities.Tools=%v (hasCaps=%v)\n", nc.Name, initResult.Capabilities.Tools, hasCaps)
		children = append(children, proxy.ChildEntry{
			Client:       srv,
			Config:       config.ServerConfig{Name: nc.Name},
			Capabilities: initResult.Capabilities,
			ServerInfo:   initResult.ServerInfo,
			Instructions: nc.Description,
		})
		fmt.Fprintf(os.Stderr, "moxy: registered moxin %s (%d tools)\n", nc.Name, len(nc.Tools))
	}
	fmt.Fprintf(os.Stderr, "moxy: bootstrap: total children after moxin registration: %d\n", len(children))

	// Resolve a session ID for native server cache scoping.
	// Fallback chain: CLAUDE_SESSION_ID > SPINCLASS_SESSION_ID > generated UUID.
	sessionID, sessionSource := resolveSessionID()
	for _, c := range children {
		if ns, ok := c.Client.(*native.Server); ok {
			ns.SetSession(sessionID)
		}
	}
	fmt.Fprintf(os.Stderr, "moxy: session %s (from %s)\n", sessionID, sessionSource)

	p := proxy.New(children, failed, cfg.Servers, cfg.Ephemeral, cfg.ProgressiveDisclosure, connectServer)
	p.SetResultReader(native.NewResultReader())

	builtinRegistry := server.NewToolRegistryV1()
	app.RegisterMCPToolsV1(builtinRegistry)
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

var (
	_ server.ToolProviderV1     = (*proxy.Proxy)(nil)
	_ server.ResourceProviderV1 = (*proxy.Proxy)(nil)
	_ server.PromptProviderV1   = (*proxy.Proxy)(nil)
)

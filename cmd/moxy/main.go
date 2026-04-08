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

	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"

	"github.com/amarbel-llc/moxy/internal/add"
	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/credentials"
	"github.com/amarbel-llc/moxy/internal/mcpclient"
	"github.com/amarbel-llc/moxy/internal/oauth"
	"github.com/amarbel-llc/moxy/internal/proxy"
	"github.com/amarbel-llc/moxy/internal/validate"
)

func newApp() *command.App {
	app := command.NewApp("moxy", "MCP proxy that aggregates child MCP servers")
	app.Version = "0.1.0"
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
			Description: "Validate moxyfile configuration hierarchy",
			Command:     "moxy validate",
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
				"unified MCP server on stdin/stdout. Shuts down gracefully on SIGINT.",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return runServer()
		},
	})

	app.AddCommand(&command.Command{
		Name: "validate",
		Description: command.Description{
			Short: "Validate moxyfile hierarchy and output TAP-14",
			Long: "Loads all moxyfiles in the hierarchy, checks TOML syntax, " +
				"validates server names and commands, verifies executables exist " +
				"on $PATH, and outputs results in TAP version 14 format.",
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
			os.Exit(validate.Run(os.Stdout, home, cwd))
			return nil
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
		Name: "hook",
		Description: command.Description{
			Short: "Handle MCP hook protocol",
		},
		Hidden: true,
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return app.HandleHook(os.Stdin, os.Stdout)
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
			return
		case "generate-plugin":
			if err := app.HandleGeneratePlugin(os.Args[2:], os.Stdout); err != nil {
				log.Fatalf("generating plugin: %v", err)
			}
			return
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := app.RunCLI(ctx, os.Args[1:], command.StubPrompter{}); err != nil {
		fmt.Fprintf(os.Stderr, "moxy: %v\n", err)
		os.Exit(1)
	}
}

func runServer() error {
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// connectServer handles both stdio and HTTP servers.
	connectServer := func(ctx context.Context, srvCfg config.ServerConfig) (*mcpclient.Client, *protocol.InitializeResultV1, error) {
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

	p := proxy.New(children, failed, cfg.Servers, cfg.Ephemeral, cfg.ProgressiveDisclosure, connectServer)

	t := transport.NewStdio(os.Stdin, os.Stdout)
	p.SetNotifier(t.Write)

	// Wire notification forwarding for startup children
	for _, c := range children {
		c.Client.SetOnNotification(p.ForwardNotification)
	}

	p.ProbeEphemeral(ctx)

	summaries := p.CollectServerSummaries(ctx)
	instructions := proxy.FormatInstructions(summaries)

	srv, err := server.New(t, server.Options{
		ServerName:    "moxy",
		ServerVersion: "0.1.0",
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

	for _, c := range children {
		c.Client.Close()
	}

	return err
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

var (
	_ server.ToolProviderV1     = (*proxy.Proxy)(nil)
	_ server.ResourceProviderV1 = (*proxy.Proxy)(nil)
	_ server.PromptProviderV1   = (*proxy.Proxy)(nil)
)

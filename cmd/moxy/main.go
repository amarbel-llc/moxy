package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"

	"github.com/amarbel-llc/moxy/internal/add"
	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/mcpclient"
	"github.com/amarbel-llc/moxy/internal/proxy"
	"github.com/amarbel-llc/moxy/internal/validate"
)

func newApp() *command.App {
	app := command.NewApp("moxy", "MCP proxy that aggregates child MCP servers")
	app.Version = "0.1.0"
	return app
}

func main() {
	flag.Parse()

	if flag.NArg() >= 1 && flag.Arg(0) == "install-mcp" {
		app := newApp()
		if err := app.InstallMCP(); err != nil {
			log.Fatalf("installing MCP: %v", err)
		}
		return
	}

	if flag.NArg() >= 1 && flag.Arg(0) == "generate-plugin" {
		app := newApp()
		if err := app.HandleGeneratePlugin(flag.Args()[1:], os.Stdout); err != nil {
			log.Fatalf("generating plugin: %v", err)
		}
		return
	}

	if flag.NArg() >= 1 && flag.Arg(0) == "validate" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("getting home dir: %v", err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("getting cwd: %v", err)
		}
		os.Exit(validate.Run(os.Stdout, home, cwd))
	}

	if flag.NArg() >= 1 && flag.Arg(0) == "add" {
		path := "moxyfile"
		if flag.NArg() >= 2 {
			path = flag.Arg(1)
		}
		if err := add.Run(path); err != nil {
			log.Fatalf("add: %v", err)
		}
		return
	}

	if flag.NArg() >= 1 && flag.Arg(0) == "hook" {
		app := newApp()
		if err := app.HandleHook(os.Stdin, os.Stdout); err != nil {
			log.Fatalf("handling hook: %v", err)
		}
		return
	}

	if err := runServer(); err != nil {
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
		if srv.Command.IsEmpty() {
			return fmt.Errorf("server %q has no command", srv.Name)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var children []proxy.ChildEntry
	var failed []proxy.FailedServer
	for _, srvCfg := range cfg.Servers {
		if srvCfg.IsEphemeral(cfg.Ephemeral) {
			fmt.Fprintf(os.Stderr, "moxy: %s configured as ephemeral (on-demand)\n", srvCfg.Name)
			continue
		}

		exe, args := srvCfg.EffectiveCommand()
		client, result, err := mcpclient.SpawnAndInitialize(ctx, srvCfg.Name, exe, args)
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
		})

		fmt.Fprintf(os.Stderr, "moxy: connected to %s (%s %s)\n",
			srvCfg.Name, result.ServerInfo.Name, result.ServerInfo.Version)
	}

	p := proxy.New(children, failed, cfg.Servers, cfg.Ephemeral, cfg.ProgressiveDisclosure, cfg.Exec)

	t := transport.NewStdio(os.Stdin, os.Stdout)
	p.SetNotifier(t.Write)

	// Wire notification forwarding for startup children
	for _, c := range children {
		c.Client.SetOnNotification(p.ForwardNotification)
	}

	p.ProbeEphemeral(ctx)

	srv, err := server.New(t, server.Options{
		ServerName:    "moxy",
		ServerVersion: "0.1.0",
		Instructions:  "MCP proxy aggregating tools, resources, and prompts from child servers.",
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

var (
	_ server.ToolProviderV1     = (*proxy.Proxy)(nil)
	_ server.ResourceProviderV1 = (*proxy.Proxy)(nil)
	_ server.PromptProviderV1   = (*proxy.Proxy)(nil)
)

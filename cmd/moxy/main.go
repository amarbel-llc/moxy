package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/mcpclient"
	"github.com/amarbel-llc/moxy/internal/proxy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "moxy: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("moxyfile")
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var children []proxy.ChildEntry
	for name, srvCfg := range cfg.Servers {
		client, result, err := mcpclient.SpawnAndInitialize(ctx, name, srvCfg.Command, srvCfg.Args)
		if err != nil {
			for _, c := range children {
				c.Client.Close()
			}
			return fmt.Errorf("starting server %s: %w", name, err)
		}

		children = append(children, proxy.ChildEntry{
			Client:       client,
			Config:       srvCfg,
			Capabilities: result.Capabilities,
		})

		fmt.Fprintf(os.Stderr, "moxy: connected to %s (%s %s)\n",
			name, result.ServerInfo.Name, result.ServerInfo.Version)
	}

	p := proxy.New(children)

	t := transport.NewStdio(os.Stdin, os.Stdout)

	srv, err := server.New(t, server.Options{
		ServerName:    "moxy",
		ServerVersion: "0.1.0",
		Instructions:  "MCP proxy aggregating tools and resources from child servers.",
		Tools:         p,
		Resources:     p,
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

var _ server.ToolProviderV1 = (*proxy.Proxy)(nil)
var _ server.ResourceProviderV1 = (*proxy.Proxy)(nil)

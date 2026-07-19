package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/statsd"
)

// instrumentedToolAdapter emits the same statsd dispatch metrics for
// standalone-moxin serving as the proxy's CallToolV1 wrapper does for
// proxied dispatch (#311). Tool names are unprefixed in this mode, so
// the served moxin's name is the <server> metric segment.
type instrumentedToolAdapter struct {
	*native.ToolAdapter
	server string
}

func (a *instrumentedToolAdapter) CallToolV1(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	start := time.Now()
	result, err := a.ToolAdapter.CallToolV1(ctx, name, args)
	statsd.EmitToolDispatch(
		a.server, name,
		time.Since(start),
		statsd.OutcomeFor(ctx.Err(), err, result != nil && result.IsError),
	)
	return result, err
}

func runMoxinServer(name string) error {
	systemDir := native.SystemMoxinDir()
	configs, err := native.DiscoverConfigs(os.Getenv("MOXIN_PATH"), systemDir)
	if err != nil {
		return fmt.Errorf("discovering moxins: %w", err)
	}

	var found *native.NativeConfig
	for _, cfg := range configs {
		if cfg.Name == name {
			found = cfg
			break
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "available moxins:\n")
		for _, cfg := range configs {
			fmt.Fprintf(os.Stderr, "  %s\n", cfg.Name)
		}
		return fmt.Errorf("moxin %q not found", name)
	}

	srv := native.NewServer(found)
	sessionID, _ := resolveSessionID()
	srv.SetSession(sessionID)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	madderClient, err := native.NewMadderClient()
	if err != nil {
		return fmt.Errorf("madder runtime: %w", err)
	}
	if err := madderClient.VerifyDefaultStore(ctx); err != nil {
		return fmt.Errorf("madder default store: %w", err)
	}
	srv.SetMadder(madderClient)

	adapter := &instrumentedToolAdapter{
		ToolAdapter: &native.ToolAdapter{Srv: srv},
		server:      found.Name,
	}

	t := transport.NewStdio(os.Stdin, os.Stdout)
	mcpSrv, err := server.New(t, server.Options{
		ServerName:    found.Name,
		ServerVersion: "0.1.0",
		Instructions:  found.Description,
		Tools:         adapter,
	})
	if err != nil {
		return fmt.Errorf("creating MCP server: %w", err)
	}

	fmt.Fprintf(os.Stderr, "moxy: serving moxin %q (%d tools)\n", found.Name, len(found.Tools))
	return mcpSrv.Run(ctx)
}

func listMoxins() error {
	configs, err := native.DiscoverConfigs(os.Getenv("MOXIN_PATH"), native.SystemMoxinDir())
	if err != nil {
		return fmt.Errorf("discovering moxins: %w", err)
	}

	if len(configs) == 0 {
		fmt.Println("no moxins found")
		return nil
	}

	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Name < configs[j].Name
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTOOLS\tDESCRIPTION")
	for _, cfg := range configs {
		fmt.Fprintf(w, "%s\t%d\t%s\n", cfg.Name, len(cfg.Tools), cfg.Description)
	}
	return w.Flush()
}

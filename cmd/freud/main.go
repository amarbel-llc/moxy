package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "serve":
		if flag.NArg() < 2 || flag.Arg(1) != "mcp" {
			fmt.Fprintf(os.Stderr, "usage: freud serve mcp\n")
			os.Exit(1)
		}
		runServeMCP()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "usage: freud <command>\n\n")
	fmt.Fprintf(os.Stderr, "commands:\n")
	fmt.Fprintf(os.Stderr, "  serve mcp    run as MCP server\n")
}

func runServeMCP() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg, err := LoadDefaultFreudHierarchy()
	if err != nil {
		fmt.Fprintf(os.Stderr, "freud: loading config: %v\n", err)
		os.Exit(1)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "freud: resolving home: %v\n", err)
		os.Exit(1)
	}

	t := transport.NewStdio(os.Stdin, os.Stdout)
	f := &freudServer{
		projectsDir: effectiveProjectsDir(cfg, home),
		listCfg:     effectiveListConfig(cfg.List),
		cache:       newProjectCache(),
	}

	srv, err := server.New(t, server.Options{
		ServerName:    "freud",
		ServerVersion: "0.1.0",
		Instructions:  "Past Claude Code session transcripts. List sessions across projects via freud://sessions (columnar text; supports ?offset=N&limit=M). Filter to one project via freud://sessions/{project}, where {project} is either a raw project directory name under ~/.claude/projects or a URL-encoded absolute path matched against the session's cwd. Phase 1a — list only; per-session content reads and search are future phases.",
		Resources:     f,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "freud: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "freud: %v\n", err)
		os.Exit(1)
	}
}

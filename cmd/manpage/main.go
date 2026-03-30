package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

type manServer struct{}

var templates = []protocol.ResourceTemplate{
	{
		URITemplate: "man://{page}",
		Name:        "Man page",
		Description: "Read a Unix man page by name",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{section}/{page}",
		Name:        "Man page (specific section)",
		Description: "Read a Unix man page by section and name",
		MimeType:    "text/plain",
	},
}

var templatesV1 = []protocol.ResourceTemplateV1{
	{
		URITemplate: "man://{page}",
		Name:        "Man page",
		Description: "Read a Unix man page by name",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{section}/{page}",
		Name:        "Man page (specific section)",
		Description: "Read a Unix man page by section and name",
		MimeType:    "text/plain",
	},
}

// ResourceProvider (base interface)

func (m *manServer) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (m *manServer) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	section, page, err := parseManURI(uri)
	if err != nil {
		return nil, err
	}

	var args []string
	if section != "" {
		args = []string{section, page}
	} else {
		args = []string{page}
	}

	cmd := exec.Command("man", args...)
	cmd.Env = append(os.Environ(), "MANPAGER=cat", "MANWIDTH=80")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("man %s: %w", page, err)
	}

	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "text/plain", Text: string(out)},
		},
	}, nil
}

func (m *manServer) ListResourceTemplates(_ context.Context) ([]protocol.ResourceTemplate, error) {
	return templates, nil
}

// ResourceProviderV1 (V1 extensions)

func (m *manServer) ListResourcesV1(_ context.Context, _ string) (*protocol.ResourcesListResultV1, error) {
	return &protocol.ResourcesListResultV1{}, nil
}

func (m *manServer) ListResourceTemplatesV1(_ context.Context, _ string) (*protocol.ResourceTemplatesListResultV1, error) {
	return &protocol.ResourceTemplatesListResultV1{ResourceTemplates: templatesV1}, nil
}

// URI parsing

func parseManURI(uri string) (section, page string, err error) {
	path := strings.TrimPrefix(uri, "man://")
	if path == "" {
		return "", "", fmt.Errorf("empty man page URI")
	}

	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 1 {
		return "", parts[0], nil
	}

	// Section numbers are single digits, optionally followed by a letter (e.g., 3p)
	if len(parts[0]) <= 2 && parts[0][0] >= '1' && parts[0][0] <= '9' {
		return parts[0], parts[1], nil
	}

	return "", path, nil
}

var _ server.ResourceProviderV1 = (*manServer)(nil)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	t := transport.NewStdio(os.Stdin, os.Stdout)
	m := &manServer{}

	srv, err := server.New(t, server.Options{
		ServerName:    "manpage",
		ServerVersion: "0.1.0",
		Instructions:  "Unix man page server. Read man pages as resources using man://{page} or man://{section}/{page} URIs.",
		Resources:     m,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "manpage: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "manpage: %v\n", err)
		os.Exit(1)
	}
}

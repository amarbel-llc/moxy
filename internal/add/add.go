package add

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/credentials"
	"github.com/amarbel-llc/moxy/internal/oauth"
)

func Run(path string, credStore credentials.Store) error {
	var serverType string

	typeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Server type").
				Description("How does this MCP server communicate?").
				Options(
					huh.NewOption("Local command (stdio)", "command"),
					huh.NewOption("Remote URL (HTTP)", "url"),
				).
				Value(&serverType),
		),
	)

	if err := typeForm.Run(); err != nil {
		return err
	}

	switch serverType {
	case "command":
		return addCommandServer(path)
	case "url":
		return addURLServer(path, credStore)
	default:
		return fmt.Errorf("unknown server type: %s", serverType)
	}
}

func addCommandServer(path string) error {
	var name, command string
	var annotations []string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Server name").
				Description("Unique name for this MCP server").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("name is required")
					}
					return nil
				}),

			huh.NewInput().
				Title("Command").
				Description("Executable to run (must be on $PATH)").
				Value(&command).
				Validate(func(s string) error {
					fields := strings.Fields(s)
					if len(fields) == 0 {
						return fmt.Errorf("command is required")
					}
					if _, err := exec.LookPath(fields[0]); err != nil {
						return fmt.Errorf("%q not found on $PATH", fields[0])
					}
					return nil
				}),

			huh.NewMultiSelect[string]().
				Title("Annotations").
				Description("Select annotation hints for this server's tools").
				Options(
					huh.NewOption("readOnlyHint", "readOnlyHint"),
					huh.NewOption("destructiveHint", "destructiveHint"),
					huh.NewOption("idempotentHint", "idempotentHint"),
					huh.NewOption("openWorldHint", "openWorldHint"),
				).
				Value(&annotations),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	srv := buildCommandServerConfig(name, command, annotations)
	return config.WriteServer(path, srv)
}

func addURLServer(path string, credStore credentials.Store) error {
	var name, serverURL string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Server name").
				Description("Unique name for this MCP server").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("name is required")
					}
					if strings.Contains(s, ".") {
						return fmt.Errorf("name must not contain dots")
					}
					return nil
				}),

			huh.NewInput().
				Title("URL").
				Description("HTTP endpoint for the MCP server").
				Value(&serverURL).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("url is required")
					}
					if _, err := url.ParseRequestURI(s); err != nil {
						return fmt.Errorf("invalid URL: %v", err)
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	srv := config.ServerConfig{
		Name: name,
		URL:  serverURL,
	}

	// Probe for OAuth
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if oauth.ProbeRequiresAuth(ctx, serverURL) {
		fmt.Println("Server requires authentication. Starting OAuth flow...")

		var clientID string
		clientIDForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Client ID (optional)").
					Description("Leave blank for dynamic client registration").
					Value(&clientID),
			),
		)
		if err := clientIDForm.Run(); err != nil {
			return err
		}

		oauthCfg := oauth.Config{
			ClientID: strings.TrimSpace(clientID),
		}

		authCtx, authCancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer authCancel()

		tok, err := oauth.DiscoverAndAuthorize(authCtx, serverURL, oauthCfg)
		if err != nil {
			return fmt.Errorf("OAuth authorization failed: %w", err)
		}

		// Store token
		if err := credStore.Write(name, tok); err != nil {
			return fmt.Errorf("storing credentials: %w", err)
		}

		fmt.Println("Authentication successful. Token stored.")

		// Save OAuth config in moxyfile
		srv.OAuth = &config.OAuthConfig{
			ClientID: oauthCfg.ClientID,
		}
	}

	return config.WriteServer(path, srv)
}

func buildCommandServerConfig(name, command string, annotations []string) config.ServerConfig {
	srv := config.ServerConfig{
		Name:    name,
		Command: config.MakeCommand(strings.Fields(command)...),
	}

	if len(annotations) > 0 {
		af := &config.AnnotationFilter{}
		for _, a := range annotations {
			t := true
			switch a {
			case "readOnlyHint":
				af.ReadOnlyHint = &t
			case "destructiveHint":
				af.DestructiveHint = &t
			case "idempotentHint":
				af.IdempotentHint = &t
			case "openWorldHint":
				af.OpenWorldHint = &t
			}
		}
		srv.Annotations = af
	}

	return srv
}

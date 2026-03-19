package add

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/amarbel-llc/moxy/internal/config"
)

func Run(path string) error {
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

	srv := buildServerConfig(name, command, annotations)
	return config.WriteServer(path, srv)
}

func buildServerConfig(name, command string, annotations []string) config.ServerConfig {
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

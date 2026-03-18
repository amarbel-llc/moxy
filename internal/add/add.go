package add

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/amarbel-llc/moxy/internal/config"
)

func FormatServerBlock(srv config.ServerConfig) string {
	var b strings.Builder
	b.WriteString("[[servers]]\n")
	fmt.Fprintf(&b, "name = %q\n", srv.Name)
	fmt.Fprintf(&b, "command = %q\n", srv.Command.String())

	if srv.Annotations != nil {
		var parts []string
		if srv.Annotations.ReadOnlyHint != nil && *srv.Annotations.ReadOnlyHint {
			parts = append(parts, "readOnlyHint = true")
		}
		if srv.Annotations.DestructiveHint != nil && *srv.Annotations.DestructiveHint {
			parts = append(parts, "destructiveHint = true")
		}
		if srv.Annotations.IdempotentHint != nil && *srv.Annotations.IdempotentHint {
			parts = append(parts, "idempotentHint = true")
		}
		if srv.Annotations.OpenWorldHint != nil && *srv.Annotations.OpenWorldHint {
			parts = append(parts, "openWorldHint = true")
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, "annotations = { %s }\n", strings.Join(parts, ", "))
		}
	}

	return b.String()
}

func AppendServerToFile(path string, srv config.ServerConfig) error {
	block := FormatServerBlock(srv)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var content string
	if len(existing) > 0 {
		s := string(existing)
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		content = s + "\n" + block
	} else {
		content = block
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

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
	return AppendServerToFile(path, srv)
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

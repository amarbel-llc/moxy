package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"code.linenisgreat.com/purse-first/libs/go-mcp/command"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/permsmatrix"
)

func addPermsMatrixCommand(app *command.App) {
	app.AddCommand(&command.Command{
		Name: "perms-matrix",
		Description: command.Description{
			Short: "Print the moxin tool permissions matrix as markdown",
			Long: "Prints one row per moxin tool: its permission tier, who judges its " +
				"inputs (broker and floor, floor only, nothing, a prompt), its " +
				"[resources] stanzas, async eligibility and annotations, and the " +
				"tools that are always-allow, name no resources and take a " +
				"path-like input. With --moxins DIR, DIR's subdirectories are the " +
				"moxins (the source tree's moxins/); otherwise the moxins this moxy " +
				"would discover. See docs/plans/2026-11-03-permissions-audit.md.",
		},
		Params: []command.Param{
			{
				Name:        "moxins",
				Type:        command.String,
				Description: "Directory whose subdirectories are moxins; default: discover as the server does.",
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				Moxins string `json:"moxins"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			configs, err := permsMatrixConfigs(args.Moxins)
			if err != nil {
				return err
			}
			fmt.Print(permsmatrix.Render(permsmatrix.Build(configs)))
			return nil
		},
	})
}

func permsMatrixConfigs(dir string) ([]*native.NativeConfig, error) {
	if dir == "" {
		return native.DiscoverConfigs(os.Getenv("MOXIN_PATH"), native.SystemMoxinDir())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var configs []*native.NativeConfig
	for _, e := range entries {
		sub := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "_moxin.toml")); err != nil {
			continue
		}
		cfg, err := native.ParseMoxinDir(sub)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

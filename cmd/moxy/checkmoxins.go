package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"code.linenisgreat.com/purse-first/libs/go-mcp/command"

	"code.linenisgreat.com/moxy/internal/native"
)

func addCheckMoxinsCommand(app *command.App) {
	app.AddCommand(&command.Command{
		Name: "check-moxins",
		Description: command.Description{
			Short: "Check that a directory of moxins loads, listing every failure",
			Long: "Loads the moxin at --dir, or every moxin in its subdirectories, with " +
				"the loader the server uses, and prints every tool file that fails " +
				"with the reason. Exits 1 if any moxin fails, 0 otherwise; it reads " +
				"files only and starts nothing. This is the intended per-repo gate " +
				"for shipped moxin directories: a repo that ships moxins runs it on " +
				"them (a justfile recipe or a flake check), so a file the loader " +
				"would refuse fails that repo's merge gate, not every session " +
				"that later loads the moxin.",
		},
		Params: []command.Param{
			{
				Name:        "dir",
				Type:        command.String,
				Description: "A moxin directory (holds _moxin.toml) or a directory whose subdirectories are moxins.",
				Required:    true,
			},
		},
		RunCLI: func(_ context.Context, argsJSON json.RawMessage) error {
			var args struct {
				Dir string `json:"dir"`
			}
			if err := json.Unmarshal(argsJSON, &args); err != nil {
				return err
			}
			return checkMoxins(args.Dir)
		},
	})
}

// checkMoxins loads dir (one moxin) or each subdirectory of dir that holds a
// _moxin.toml, prints the outcome per moxin, and fails if any did not load.
func checkMoxins(dir string) error {
	moxinDirs, err := moxinDirsUnder(dir)
	if err != nil {
		return err
	}
	if len(moxinDirs) == 0 {
		return fmt.Errorf("%s: no moxin found (no _moxin.toml here or in a subdirectory)", dir)
	}
	failed := 0
	tools := 0
	for _, d := range moxinDirs {
		cfg, err := native.ParseMoxinDir(d)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "FAIL %s: %s\n", d, err)
			continue
		}
		tools += len(cfg.Tools)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d moxins failed to load", failed, len(moxinDirs))
	}
	fmt.Printf("ok: %d moxins, %d tools\n", len(moxinDirs), tools)
	return nil
}

func moxinDirsUnder(dir string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(dir, "_moxin.toml")); err == nil {
		return []string{dir}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		sub := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "_moxin.toml")); err == nil {
			out = append(out, sub)
		}
	}
	sort.Strings(out)
	return out, nil
}

package native

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemMoxinDir returns the path to the moxin configs shipped with the moxy
// binary. It resolves os.Executable() to find <prefix>/share/moxy/moxins/.
// Returns "" if it doesn't exist (graceful degradation).
func SystemMoxinDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}

	// exe is <prefix>/bin/moxy → prefix is two levels up
	prefix := filepath.Dir(filepath.Dir(exe))
	dir := filepath.Join(prefix, "share", "moxy", "moxins")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// ParseMoxinPath splits a colon-separated MOXIN_PATH into directory entries.
// Empty entries are skipped.
func ParseMoxinPath(path string) []string {
	if path == "" {
		return nil
	}
	var dirs []string
	for _, d := range strings.Split(path, ":") {
		d = strings.TrimSpace(d)
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// DefaultMoxinPath computes a MOXIN_PATH from the legacy hierarchy convention:
//
//	<cwd>/.moxy/moxins:<intermediate>/.moxy/moxins:~/.config/moxy/moxins:<systemDir>
//
// Only directories that exist on disk are included. Used by the
// `moxy moxin-path` subcommand.
func DefaultMoxinPath(home, cwd, systemDir string) string {
	var dirs []string

	cleanHome := filepath.Clean(home)
	cleanCwd := filepath.Clean(cwd)

	// Project-local (highest priority → first in path)
	if d := filepath.Join(cleanCwd, ".moxy", "moxins"); dirExists(d) {
		dirs = append(dirs, d)
	}

	// Intermediate parent directories between home and cwd (closer to cwd = higher priority)
	rel, err := filepath.Rel(cleanHome, cleanCwd)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		// Walk from cwd toward home (reverse order for priority)
		for i := len(parts) - 1; i >= 1; i-- {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			if d := filepath.Join(parentDir, ".moxy", "moxins"); dirExists(d) {
				dirs = append(dirs, d)
			}
		}
	}

	// Global user config
	if d := filepath.Join(home, ".config", "moxy", "moxins"); dirExists(d) {
		dirs = append(dirs, d)
	}

	// System moxins (lowest priority → last in path)
	if systemDir != "" && dirExists(systemDir) {
		dirs = append(dirs, systemDir)
	}

	return strings.Join(dirs, ":")
}

// DiscoverConfigs loads moxin configs from MOXIN_PATH directories.
// Each moxin is a subdirectory containing _moxin.toml. Dirs are processed
// from last to first; earlier path entries override later ones by server name.
// systemDir is appended as the lowest-priority entry (pass "" to omit).
//
// When moxinPath is empty, the default hierarchy is computed from the current
// working directory (same directories as `moxy moxin-path`), so discovery
// works without an explicit MOXIN_PATH env var.
// MoxinError records a moxin directory that failed to load.
type MoxinError struct {
	Dir string
	Err error
}

// DiscoverResult holds both successfully loaded moxins and load failures.
type DiscoverResult struct {
	Configs []*NativeConfig
	Errors  []MoxinError
}

// DiscoverAll loads moxin configs and collects load failures instead of
// logging them to stderr. Used by `moxy list` to show error details.
func DiscoverAll(moxinPath string, systemDir string) (DiscoverResult, error) {
	dirs := resolveMoxinDirs(moxinPath, systemDir)

	byName := make(map[string]*NativeConfig)
	var order []string
	var loadErrors []MoxinError

	for i := len(dirs) - 1; i >= 0; i-- {
		moxyDir := dirs[i]
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return DiscoverResult{}, fmt.Errorf("reading %s: %w", moxyDir, err)
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dirPath := filepath.Join(moxyDir, e.Name())
			metaPath := filepath.Join(dirPath, "_moxin.toml")
			if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
				continue
			}
			cfg, err := ParseMoxinDir(dirPath)
			if err != nil {
				loadErrors = append(loadErrors, MoxinError{Dir: dirPath, Err: err})
				continue
			}
			cfg.SourceDir = dirPath
			if _, exists := byName[cfg.Name]; !exists {
				order = append(order, cfg.Name)
			}
			byName[cfg.Name] = cfg
		}
	}

	result := make([]*NativeConfig, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	return DiscoverResult{Configs: result, Errors: loadErrors}, nil
}

func resolveMoxinDirs(moxinPath, systemDir string) []string {
	if moxinPath == "" {
		home, _ := os.UserHomeDir()
		cwd, _ := os.Getwd()
		if home != "" && cwd != "" {
			moxinPath = DefaultMoxinPath(home, cwd, "")
		}
	}
	dirs := ParseMoxinPath(moxinPath)
	if systemDir != "" {
		dirs = append(dirs, systemDir)
	}
	return dirs
}

func DiscoverConfigs(moxinPath string, systemDir string) ([]*NativeConfig, error) {
	dirs := resolveMoxinDirs(moxinPath, systemDir)

	byName := make(map[string]*NativeConfig)
	var order []string

	// Load from last to first so earlier entries override later ones.
	for i := len(dirs) - 1; i >= 0; i-- {
		moxyDir := dirs[i]
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", moxyDir, err)
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dirPath := filepath.Join(moxyDir, e.Name())
			metaPath := filepath.Join(dirPath, "_moxin.toml")
			if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
				continue
			}
			cfg, err := ParseMoxinDir(dirPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "moxy: skipping moxin %s: %v\n", dirPath, err)
				continue
			}
			cfg.SourceDir = dirPath
			if _, exists := byName[cfg.Name]; !exists {
				order = append(order, cfg.Name)
			}
			byName[cfg.Name] = cfg
		}
	}

	result := make([]*NativeConfig, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	return result, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

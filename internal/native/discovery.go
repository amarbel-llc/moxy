package native

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var moxinLogger *log.Logger

func init() {
	logHome := os.Getenv("XDG_LOG_HOME")
	if logHome == "" {
		home, _ := os.UserHomeDir()
		logHome = filepath.Join(home, ".local", "log")
	}
	logDir := filepath.Join(logHome, "moxy")
	os.MkdirAll(logDir, 0o755)
	logFile := filepath.Join(logDir, fmt.Sprintf("moxin.%d.log", os.Getpid()))
	f, err := os.OpenFile(
		logFile,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err == nil {
		moxinLogger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	}
}

func debugMoxin(format string, args ...any) {
	if moxinLogger != nil {
		moxinLogger.Printf(format, args...)
	}
}

// defaultSystemMoxinDir is set at build time via -ldflags:
//
//	-X github.com/amarbel-llc/moxy/internal/native.defaultSystemMoxinDir=/nix/store/.../share/moxy/moxins
//
// This allows nix builds (where the binary and moxins live in separate store
// paths) to locate builtin moxins without relying on executable path resolution.
var defaultSystemMoxinDir string

// SystemMoxinDir returns the path to the moxin configs shipped with the moxy
// binary. It checks (in order):
//  1. Compile-time injected path (nix builds)
//  2. Executable-relative <prefix>/share/moxy/moxins/ (dev builds)
//
// Returns "" if neither exists (graceful degradation).
func SystemMoxinDir() string {
	if defaultSystemMoxinDir != "" {
		if info, err := os.Stat(defaultSystemMoxinDir); err == nil && info.IsDir() {
			debugMoxin("SystemMoxinDir: compile-time path %q", defaultSystemMoxinDir)
			return defaultSystemMoxinDir
		}
		debugMoxin("SystemMoxinDir: compile-time path %q not found or not a dir", defaultSystemMoxinDir)
	}

	exe, err := os.Executable()
	if err != nil {
		debugMoxin("SystemMoxinDir: os.Executable error: %v", err)
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		debugMoxin("SystemMoxinDir: EvalSymlinks error: %v", err)
		return ""
	}

	// exe is <prefix>/bin/moxy → prefix is two levels up
	prefix := filepath.Dir(filepath.Dir(exe))
	dir := filepath.Join(prefix, "share", "moxy", "moxins")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		debugMoxin("SystemMoxinDir: exe-relative path %q", dir)
		return dir
	}
	debugMoxin("SystemMoxinDir: no system moxin dir found (exe=%s)", exe)
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
	// Dirs lists the MOXIN_PATH directories that were scanned, in priority
	// order (highest first). Useful for status display.
	Dirs []string
}

// DiscoverAll loads moxin configs and collects load failures instead of
// logging them to stderr. Uses the same resolution and merge logic as
// DiscoverConfigs (the server runtime path).
func DiscoverAll(moxinPath string, systemDir string) (DiscoverResult, error) {
	dirs := resolveMoxinDirs(moxinPath, systemDir)

	byName := make(map[string]*NativeConfig)
	var order []string
	var loadErrors []MoxinError

	for i := len(dirs) - 1; i >= 0; i-- {
		moxyDir := dirs[i]
		debugMoxin("DiscoverAll: scanning dir %s", moxyDir)
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			debugMoxin("DiscoverAll: dir %s does not exist, skipping", moxyDir)
			continue
		}
		if err != nil {
			debugMoxin("DiscoverAll: ReadDir error %s: %v", moxyDir, err)
			return DiscoverResult{}, fmt.Errorf("reading %s: %w", moxyDir, err)
		}

		for _, e := range entries {
			dirPath := filepath.Join(moxyDir, e.Name())
			if !dirExists(dirPath) {
				continue
			}
			metaPath := filepath.Join(dirPath, "_moxin.toml")
			if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
				debugMoxin("DiscoverAll: no _moxin.toml in %s, skipping", dirPath)
				continue
			}
			debugMoxin("DiscoverAll: parsing %s", dirPath)
			cfg, err := ParseMoxinDir(dirPath)
			if err != nil {
				debugMoxin("DiscoverAll: parse error %s: %v", dirPath, err)
				loadErrors = append(loadErrors, MoxinError{Dir: dirPath, Err: err})
				continue
			}
			cfg.SourceDir = dirPath
			if _, exists := byName[cfg.Name]; !exists {
				order = append(order, cfg.Name)
			}
			byName[cfg.Name] = cfg
			debugMoxin("DiscoverAll: loaded %s (name=%s, %d tools)", e.Name(), cfg.Name, len(cfg.Tools))
		}
	}

	result := make([]*NativeConfig, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	debugMoxin("DiscoverAll: result: %d configs, %d errors", len(result), len(loadErrors))
	return DiscoverResult{Configs: result, Errors: loadErrors, Dirs: dirs}, nil
}

func resolveMoxinDirs(moxinPath, systemDir string) []string {
	if moxinPath == "" {
		home, _ := os.UserHomeDir()
		cwd, _ := os.Getwd()
		if home != "" && cwd != "" {
			moxinPath = DefaultMoxinPath(home, cwd, "")
		}
		debugMoxin("resolveMoxinDirs: computed default moxinPath=%q", moxinPath)
	} else {
		debugMoxin("resolveMoxinDirs: explicit MOXIN_PATH=%q", moxinPath)
	}
	dirs := ParseMoxinPath(moxinPath)
	if systemDir != "" {
		dirs = append(dirs, systemDir)
	}
	debugMoxin("resolveMoxinDirs: systemDir=%q dirs=%v", systemDir, dirs)
	return dirs
}

func DiscoverConfigs(moxinPath string, systemDir string) ([]*NativeConfig, error) {
	dirs := resolveMoxinDirs(moxinPath, systemDir)

	byName := make(map[string]*NativeConfig)
	var order []string

	// Load from last to first so earlier entries override later ones.
	for i := len(dirs) - 1; i >= 0; i-- {
		moxyDir := dirs[i]
		debugMoxin("DiscoverConfigs: scanning dir %s", moxyDir)
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			debugMoxin("DiscoverConfigs: dir %s does not exist, skipping", moxyDir)
			continue
		}
		if err != nil {
			debugMoxin("DiscoverConfigs: ReadDir error %s: %v", moxyDir, err)
			return nil, fmt.Errorf("reading %s: %w", moxyDir, err)
		}

		for _, e := range entries {
			dirPath := filepath.Join(moxyDir, e.Name())
			if !dirExists(dirPath) {
				debugMoxin("DiscoverConfigs: %s not a dir, skipping", dirPath)
				continue
			}
			metaPath := filepath.Join(dirPath, "_moxin.toml")
			if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
				debugMoxin("DiscoverConfigs: no _moxin.toml in %s, skipping", dirPath)
				continue
			}
			debugMoxin("DiscoverConfigs: parsing %s", dirPath)
			cfg, err := ParseMoxinDir(dirPath)
			if err != nil {
				debugMoxin("DiscoverConfigs: parse error %s: %v", dirPath, err)
				fmt.Fprintf(os.Stderr, "moxy: skipping moxin %s: %v\n", dirPath, err)
				continue
			}
			cfg.SourceDir = dirPath
			if prev, exists := byName[cfg.Name]; exists {
				debugMoxin("DiscoverConfigs: %s (name=%s) overrides previous from %s", dirPath, cfg.Name, prev.SourceDir)
			} else {
				order = append(order, cfg.Name)
			}
			byName[cfg.Name] = cfg
			debugMoxin("DiscoverConfigs: loaded %s (name=%s, %d tools, source=%s)", e.Name(), cfg.Name, len(cfg.Tools), dirPath)
		}
	}

	result := make([]*NativeConfig, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	debugMoxin("DiscoverConfigs: result: %d configs", len(result))
	return result, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

package native

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
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
//	-X code.linenisgreat.com/moxy/internal/native.defaultSystemMoxinDir=/nix/store/.../share/moxy/moxins
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

// SystemMoxinDirFor applies the moxyfile's builtin-native key to
// SystemMoxinDir: an explicit false omits the system dir ("").
func SystemMoxinDirFor(builtinNative *bool) string {
	if builtinNative != nil && !*builtinNative {
		debugMoxin("SystemMoxinDirFor: builtin-native = false, omitting system dir")
		return ""
	}
	return SystemMoxinDir()
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

// MoxinError records a moxin directory that failed to load.
type MoxinError struct {
	// Name is the moxin name the directory claims: `name` from its
	// _moxin.toml when that much is readable, else the directory's own name.
	Name string
	Dir  string
	Err  error
}

// DiscoverResult holds both successfully loaded moxins and load failures.
type DiscoverResult struct {
	Configs []*NativeConfig
	Errors  []MoxinError
	// Dirs lists the MOXIN_PATH directories that were scanned, in priority
	// order (highest first). Useful for status display.
	Dirs []string
	// Shadowed lists moxins hidden by a same-named moxin in a
	// higher-priority dir.
	Shadowed []ShadowedMoxin
	// Refused is the subset of Errors whose directory won its name: the
	// moxin a caller asked for by putting it first, which failed to load.
	// Such a name is served by nothing — see discover.
	Refused []MoxinError
}

// ShadowedMoxin is a moxin at Dir that lost to the same-named moxin at By.
type ShadowedMoxin struct {
	Name string
	Dir  string
	By   string
}

// DiscoverAll loads moxin configs and collects load failures instead of
// logging them to stderr. Uses the same resolution and merge logic as
// DiscoverConfigs (the server runtime path).
func DiscoverAll(moxinPath string, systemDir string) (DiscoverResult, error) {
	return discover(resolveMoxinDirs(moxinPath, systemDir), false)
}

// DiscoverServing is discovery as the serve paths need it: like
// DiscoverConfigs it steps over an unreadable MOXIN_PATH entry, and like
// DiscoverAll it returns the load failures, so the caller can report a
// refused moxin instead of serving without it.
func DiscoverServing(moxinPath string, systemDir string) (DiscoverResult, error) {
	return discover(resolveMoxinDirs(moxinPath, systemDir), true)
}

// discover is the one implementation of moxin discovery. dirs are in
// priority order, highest first; it walks them lowest first so each name's
// last claimant is its highest-priority one.
//
// A directory that fails to load still claims its name. The moxin a caller
// put first is the one they meant to serve: if it is refused, a same-named
// moxin further down the path must not be served in its place, because that
// is a different set of tools and permission tiers under the same name. The
// name is then served by nothing, the lower moxin is reported as shadowed,
// and the failure is in Refused.
//
// skipUnreadableDirs makes an unreadable MOXIN_PATH entry a stderr line
// instead of an error: a stale entry (a `result` symlink that now points at
// a regular file after a partial `nix build`) must not take down discovery
// for the other dirs, or hook permission lookups fall through for every
// moxin tool.
func discover(dirs []string, skipUnreadableDirs bool) (DiscoverResult, error) {
	byName := make(map[string]*NativeConfig)
	claimedBy := make(map[string]string) // name → the dir that currently wins it
	var order []string
	var loadErrors []MoxinError
	var shadowed []ShadowedMoxin

	for i := len(dirs) - 1; i >= 0; i-- {
		moxyDir := dirs[i]
		debugMoxin("discover: scanning dir %s", moxyDir)
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			debugMoxin("discover: dir %s does not exist, skipping", moxyDir)
			continue
		}
		if err != nil {
			debugMoxin("discover: ReadDir error %s: %v", moxyDir, err)
			if !skipUnreadableDirs {
				return DiscoverResult{}, fmt.Errorf("reading %s: %w", moxyDir, err)
			}
			fmt.Fprintf(os.Stderr, "moxy: skipping unreadable moxin dir %s: %v\n", moxyDir, err)
			continue
		}

		for _, e := range entries {
			dirPath := filepath.Join(moxyDir, e.Name())
			if !dirExists(dirPath) {
				continue
			}
			metaPath := filepath.Join(dirPath, "_moxin.toml")
			if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
				debugMoxin("discover: no _moxin.toml in %s, skipping", dirPath)
				continue
			}
			debugMoxin("discover: parsing %s", dirPath)
			cfg, err := ParseMoxinDir(dirPath)

			name := claimedMoxinName(dirPath)
			if err == nil {
				name = cfg.Name
			}
			if prevDir, claimed := claimedBy[name]; claimed {
				shadowed = append(shadowed, ShadowedMoxin{Name: name, Dir: prevDir})
			} else {
				order = append(order, name)
			}
			claimedBy[name] = dirPath

			if err != nil {
				debugMoxin("discover: parse error %s: %v", dirPath, err)
				loadErrors = append(loadErrors, MoxinError{Name: name, Dir: dirPath, Err: err})
				delete(byName, name)
				continue
			}
			cfg.SourceDir = dirPath
			byName[name] = cfg
			debugMoxin("discover: loaded %s (name=%s, %d tools)", e.Name(), name, len(cfg.Tools))
		}
	}

	result := make([]*NativeConfig, 0, len(order))
	for _, name := range order {
		if cfg, loaded := byName[name]; loaded {
			result = append(result, cfg)
		}
	}
	for i := range shadowed {
		shadowed[i].By = claimedBy[shadowed[i].Name]
	}
	var refused []MoxinError
	for _, loadErr := range loadErrors {
		if claimedBy[loadErr.Name] == loadErr.Dir {
			refused = append(refused, loadErr)
		}
	}
	debugMoxin("discover: result: %d configs, %d errors (%d refused), %d shadowed",
		len(result), len(loadErrors), len(refused), len(shadowed))
	return DiscoverResult{
		Configs: result, Errors: loadErrors, Dirs: dirs, Shadowed: shadowed, Refused: refused,
	}, nil
}

// claimedMoxinName is the name a moxin directory claims when the directory as
// a whole could not be loaded: `name` from its _moxin.toml if that file
// parses and names one a moxin may have, else the directory's own name. A
// name with a dot is not one: it is refused at load, and a failed server
// advertised as `a.b.status` could not be routed back to `a.b`.
func claimedMoxinName(dirPath string) string {
	var meta MoxinMeta
	data, err := os.ReadFile(filepath.Join(dirPath, "_moxin.toml"))
	if err == nil && toml.Unmarshal(data, &meta) == nil &&
		meta.Name != "" && !strings.Contains(meta.Name, ".") {
		return meta.Name
	}
	return filepath.Base(dirPath)
}

func resolveMoxinDirs(moxinPath, systemDir string) []string {
	cwd, _ := os.Getwd()
	return resolveMoxinDirsFor(moxinPath, cwd, systemDir)
}

// resolveMoxinDirsFor is resolveMoxinDirs with the directory the default
// hierarchy is computed from made explicit, for callers asking about a
// directory other than the working one.
func resolveMoxinDirsFor(moxinPath, cwd, systemDir string) []string {
	if moxinPath == "" {
		home, _ := os.UserHomeDir()
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

// DiscoverConfigs loads moxin configs from MOXIN_PATH directories.
// Each moxin is a subdirectory containing _moxin.toml. Dirs are processed
// from last to first; earlier path entries override later ones by server name.
// systemDir is appended as the lowest-priority entry (pass "" to omit).
//
// When moxinPath is empty, the default hierarchy is computed from the current
// working directory (same directories as `moxy moxin-path`), so discovery
// works without an explicit MOXIN_PATH env var.
func DiscoverConfigs(moxinPath string, systemDir string) ([]*NativeConfig, error) {
	return discoverConfigsIn(resolveMoxinDirs(moxinPath, systemDir))
}

// ResolvedMoxinSources reports which directory each moxin name resolves to,
// as discovery from dir would find it: the winning directory per name, after
// MOXIN_PATH order and shadowing, with symlinks resolved so two spellings of
// one directory compare equal. This is the identity of a moxin as far as its
// tools and their permission tiers are concerned — both are read from that
// directory.
func ResolvedMoxinSources(moxinPath, dir, systemDir string) (map[string]string, error) {
	configs, err := discoverConfigsIn(resolveMoxinDirsFor(moxinPath, dir, systemDir))
	if err != nil {
		return nil, err
	}
	return MoxinSources(configs), nil
}

// MoxinSources maps each discovered moxin's name to the directory it was
// loaded from, symlinks resolved. Never nil: no moxins is an empty map.
func MoxinSources(configs []*NativeConfig) map[string]string {
	sources := make(map[string]string, len(configs))
	for _, cfg := range configs {
		source := cfg.SourceDir
		if resolved, err := filepath.EvalSymlinks(source); err == nil {
			source = resolved
		}
		sources[cfg.Name] = source
	}
	return sources
}

func discoverConfigsIn(dirs []string) ([]*NativeConfig, error) {
	discovered, err := discover(dirs, true)
	if err != nil {
		return nil, err
	}
	for _, loadErr := range discovered.Errors {
		fmt.Fprintf(os.Stderr, "moxy: skipping moxin %s: %v\n", loadErr.Dir, loadErr.Err)
	}
	return discovered.Configs, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

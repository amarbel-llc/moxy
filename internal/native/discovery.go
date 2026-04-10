package native

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuiltinDir returns the path to the builtin native server configs shipped
// with the moxy binary. It resolves os.Executable() to find
// <prefix>/share/moxy/builtin-servers/. The MOXY_BUILTIN_DIR env var
// overrides for development/testing. Returns "" if the directory does not
// exist (graceful degradation).
func BuiltinDir() string {
	if dir := os.Getenv("MOXY_BUILTIN_DIR"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		return ""
	}

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
	dir := filepath.Join(prefix, "share", "moxy", "builtin-servers")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// HierarchyDirs returns the ordered list of directories that native server
// configs are loaded from, lowest to highest priority:
//
//	0. builtinDir (shipped with the binary)
//	1. ~/.config/moxy/servers/ (global)
//	2. Each parent directory between home and dir (.moxy/servers/)
//	3. dir/.moxy/servers/ (project-local)
func HierarchyDirs(builtinDir, home, dir string) []string {
	var dirs []string

	if builtinDir != "" {
		dirs = append(dirs, builtinDir)
	}

	dirs = append(dirs, filepath.Join(home, ".config", "moxy", "servers"))

	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)
	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			dirs = append(dirs, filepath.Join(parentDir, ".moxy", "servers"))
		}
	}

	dirs = append(dirs, filepath.Join(cleanDir, ".moxy", "servers"))
	return dirs
}

// DiscoverConfigs walks the servers/ directory hierarchy, loading *.toml
// files and merging by server name (later overrides earlier).
func DiscoverConfigs(builtinDir, home, dir string) ([]*NativeConfig, error) {
	byName := make(map[string]*NativeConfig)
	var order []string

	for _, moxyDir := range HierarchyDirs(builtinDir, home, dir) {
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", moxyDir, err)
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
				continue
			}
			path := filepath.Join(moxyDir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "moxy: skipping native config %s: %v\n", path, err)
				continue
			}
			cfg, err := ParseConfig(data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "moxy: skipping native config %s: %v\n", path, err)
				continue
			}
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

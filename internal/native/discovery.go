package native

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverConfigs walks the servers/ directory hierarchy from home to dir,
// loading *.toml files and merging by server name (later overrides earlier).
// The walk order matches LoadHierarchy in internal/config:
//  1. ~/.config/moxy/servers/ (global)
//  2. Each parent directory between home and dir (.moxy/servers/)
//  3. dir/.moxy/servers/ (project-local)
func DiscoverConfigs(home, dir string) ([]*NativeConfig, error) {
	byName := make(map[string]*NativeConfig)
	var order []string

	loadDir := func(moxyDir string) error {
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", moxyDir, err)
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(moxyDir, e.Name()))
			if err != nil {
				return fmt.Errorf("reading %s: %w", e.Name(), err)
			}
			cfg, err := ParseConfig(data)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", moxyDir, e.Name(), err)
			}
			if _, exists := byName[cfg.Name]; !exists {
				order = append(order, cfg.Name)
			}
			byName[cfg.Name] = cfg
		}
		return nil
	}

	// 1. Global: ~/.config/moxy/servers/
	globalDir := filepath.Join(home, ".config", "moxy", "servers")
	if err := loadDir(globalDir); err != nil {
		return nil, err
	}

	// 2. Intermediate parent directories walking down from home to dir
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)
	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			if err := loadDir(filepath.Join(parentDir, ".moxy", "servers")); err != nil {
				return nil, err
			}
		}
	}

	// 3. Target directory
	if err := loadDir(filepath.Join(cleanDir, ".moxy", "servers")); err != nil {
		return nil, err
	}

	result := make([]*NativeConfig, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	return result, nil
}

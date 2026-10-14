package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"code.linenisgreat.com/moxy/internal/config/schema"
)

// The moxyfile TOML data model and its tommy-generated codec live in the
// schema subpackage. They are isolated there so that regenerating the
// generated file never depends on (and never breaks) the code that consumes
// it. These aliases keep existing config.* references working unchanged.
type (
	Config           = schema.Config
	ServerConfig     = schema.ServerConfig
	OAuthConfig      = schema.OAuthConfig
	AnnotationFilter = schema.AnnotationFilter
	Command          = schema.Command
	DisableMoxinSet  = schema.DisableMoxinSet
	DisableServerSet = schema.DisableServerSet
	AllowToolSet     = schema.AllowToolSet
	ConfigDocument   = schema.ConfigDocument
)

var (
	MakeCommand  = schema.MakeCommand
	DecodeConfig = schema.DecodeConfig
)

type LoadSource struct {
	Path  string
	Found bool
	File  Config
}

type Hierarchy struct {
	Sources []LoadSource
	Merged  Config
}

// Parse decodes and validates a moxyfile, expanding $VAR references in server
// header values against the process environment.
func Parse(data []byte) (Config, error) {
	cfg, err := ParseLiteral(data)
	if err != nil {
		return Config{}, err
	}
	return ExpandHeaders(cfg), nil
}

// ParseLiteral decodes and validates a moxyfile without expanding anything:
// header values keep their $VAR references. It is what narrowing compares and
// what the effective render writes, so neither ever carries an expanded
// secret.
func ParseLiteral(data []byte) (Config, error) {
	if len(data) == 0 {
		return Config{}, nil
	}

	doc, err := DecodeConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("parsing moxyfile: %w", err)
	}

	cfg := doc.Data()

	// The decoder skips a value of the wrong type instead of failing, which
	// for these keys would read a typo as "no restriction": a string where
	// allow-tools wants an array would allow everything. Unknown keys stay
	// tolerated; a known restriction that did not decode is an error.
	for _, path := range doc.Undecoded() {
		key, _, _ := strings.Cut(path, ".")
		if restrictionKeys[key] {
			return Config{}, fmt.Errorf("%s: wrong type (at %q); refusing to read it as unset", key, path)
		}
	}

	if err := validateNameOrToolEntries("disable-moxins", cfg.DisableMoxins); err != nil {
		return Config{}, err
	}
	if err := validateNameOrToolEntries("allow-tools", cfg.AllowTools); err != nil {
		return Config{}, err
	}
	if err := validatePassTools(cfg.AllowTools, cfg.PassTools); err != nil {
		return Config{}, err
	}

	// disable-servers is whole-server only. Per-tool granularity is not
	// supported for [[servers]] entries because their tool lists come from
	// the initialize handshake at runtime rather than static config.
	for _, entry := range cfg.DisableServers {
		if entry == "" {
			return Config{}, fmt.Errorf("disable-servers: entries must not be empty")
		}
		if strings.Contains(entry, ".") {
			return Config{}, fmt.Errorf("disable-servers: invalid entry %q (per-tool disable not supported; use the bare server name)", entry)
		}
	}

	for _, srv := range cfg.Servers {
		if strings.Contains(srv.Name, ".") {
			return Config{}, fmt.Errorf(
				"server name %q must not contain '.' (dots are used as the namespace separator)",
				srv.Name,
			)
		}

		hasCommand := !srv.Command.IsEmpty()
		hasURL := srv.URL != ""

		if hasCommand && hasURL {
			return Config{}, fmt.Errorf(
				"server %q has both command and url (only one is allowed)",
				srv.Name,
			)
		}

		if hasURL {
			if _, err := url.ParseRequestURI(srv.URL); err != nil {
				return Config{}, fmt.Errorf(
					"server %q has invalid url %q: %w",
					srv.Name, srv.URL, err,
				)
			}
			if srv.NixDevshell != nil {
				return Config{}, fmt.Errorf(
					"server %q: nix-devshell is not valid for url servers",
					srv.Name,
				)
			}
			if srv.Ephemeral != nil && *srv.Ephemeral {
				return Config{}, fmt.Errorf(
					"server %q: ephemeral is not supported for url servers",
					srv.Name,
				)
			}
		}

		if !hasURL {
			if srv.Headers != nil {
				return Config{}, fmt.Errorf(
					"server %q: headers is only valid for url servers",
					srv.Name,
				)
			}
			if srv.HeadersHelper != nil {
				return Config{}, fmt.Errorf(
					"server %q: headers-helper is only valid for url servers",
					srv.Name,
				)
			}
			if srv.OAuth != nil {
				return Config{}, fmt.Errorf(
					"server %q: oauth is only valid for url servers",
					srv.Name,
				)
			}
		}
	}

	return *cfg, nil
}

// restrictionKeys are the top-level keys that decide what a moxyfile grants.
// For most, absence means "unrestricted", so silently dropping a mistyped one
// would widen what is served; pass-tools fails closed when dropped, but a key
// that decides rights is still never read as unset by accident.
var restrictionKeys = map[string]bool{
	"allow-tools":     true,
	"pass-tools":      true,
	"disable-moxins":  true,
	"disable-servers": true,
	"builtin-native":  true,
	"resolved-moxins": true,
}

// validateNameOrToolEntries checks the grammar disable-moxins and allow-tools
// share: each entry is a bare name or a single "name.tool" pair.
func validateNameOrToolEntries(key string, entries []string) error {
	for _, entry := range entries {
		if entry == "" {
			return fmt.Errorf("%s: entries must not be empty", key)
		}
		name, tool, dotted := strings.Cut(entry, ".")
		if name == "" {
			return fmt.Errorf("%s: invalid entry %q", key, entry)
		}
		if dotted && tool == "" {
			return fmt.Errorf("%s: invalid entry %q (tool name is empty)", key, entry)
		}
		if strings.Contains(tool, ".") {
			return fmt.Errorf("%s: invalid entry %q (tool names must not contain dots)", key, entry)
		}
	}
	return nil
}

// validatePassTools enforces what makes pass-tools mean "may confer, never
// calls". Without an allowlist every tool is usable, so a pass tool would be
// callable; and an entry allow-tools already covers is usable too, so listing
// it at pass contradicts the file. A bare pass entry over a dotted allow entry
// is fine: the holder calls that one tool and may confer the rest.
func validatePassTools(allow, pass []string) error {
	if pass == nil {
		return nil
	}
	if err := validateNameOrToolEntries("pass-tools", pass); err != nil {
		return err
	}
	if allow == nil {
		return fmt.Errorf("pass-tools is set without allow-tools: with no allowlist every tool is usable, so a pass tool would be callable")
	}
	allowed := stringSet(allow)
	for _, entry := range pass {
		if CoveredBy(entry, allowed) {
			return fmt.Errorf("pass-tools: %q is also allowed by allow-tools; a tool is held at one level, use or pass", entry)
		}
	}
	return nil
}

// CoveredBy reports whether set names entry itself or, for a "name.tool"
// entry, the whole name. It is the subsumption rule disable-moxins and
// allow-tools share: a whole-server entry covers each of its tools.
func CoveredBy(entry string, set map[string]bool) bool {
	if set[entry] {
		return true
	}
	if name, _, dotted := strings.Cut(entry, "."); dotted {
		return set[name]
	}
	return false
}

// IntersectAllowTools combines two allow-tools lists into the entries both
// allow. Each layer of the hierarchy that sets allow-tools can therefore only
// take tools away. The result is sorted and never nil: two lists with nothing
// in common allow nothing, which is different from no list at all.
func IntersectAllowTools(a, b []string) []string {
	inA, inB := stringSet(a), stringSet(b)
	both := make(map[string]bool)
	for entry := range inA {
		if CoveredBy(entry, inB) {
			both[entry] = true
		}
	}
	for entry := range inB {
		if CoveredBy(entry, inA) {
			both[entry] = true
		}
	}
	out := make([]string, 0, len(both))
	for entry := range both {
		out = append(out, entry)
	}
	sort.Strings(out)
	return out
}

// meetToolLevels combines two layers' (allow-tools, pass-tools) pairs into the
// pair holding each tool at the lower of its two levels. A tool is at use in
// the result when both allow it, and at least at pass when both hold it at
// pass or use; the pass list is the latter minus what the allow list covers,
// so the result is itself a valid pair.
func meetToolLevels(allowA, passA, allowB, passB []string) (allow, pass []string) {
	allow = IntersectAllowTools(allowA, allowB)
	allowed := stringSet(allow)
	held := IntersectAllowTools(
		append(append([]string(nil), allowA...), passA...),
		append(append([]string(nil), allowB...), passB...),
	)
	for _, entry := range held {
		if !CoveredBy(entry, allowed) {
			pass = append(pass, entry)
		}
	}
	return allow, pass
}

// nonEmptyOrNil normalises a pass-tools list: an empty one holds nothing at
// pass, exactly as an absent one does, so both are kept as nil.
func nonEmptyOrNil(entries []string) []string {
	if len(entries) == 0 {
		return nil
	}
	return sortedUnique(entries)
}

func stringSet(entries []string) map[string]bool {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[e] = true
	}
	return set
}

func Load(path string) (Config, error) {
	return loadWith(path, Parse)
}

func loadWith(path string, parse func([]byte) (Config, error)) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading moxyfile: %w", err)
	}
	return parse(data)
}

func Merge(base, overlay Config) Config {
	merged := base

	if overlay.Ephemeral != nil {
		merged.Ephemeral = overlay.Ephemeral
	}

	if overlay.ProgressiveDisclosure != nil {
		merged.ProgressiveDisclosure = overlay.ProgressiveDisclosure
	}

	if overlay.BuiltinNative != nil {
		merged.BuiltinNative = overlay.BuiltinNative
	}

	if len(overlay.DisableMoxins) > 0 {
		seen := make(map[string]bool, len(merged.DisableMoxins))
		for _, d := range merged.DisableMoxins {
			seen[d] = true
		}
		for _, d := range overlay.DisableMoxins {
			if !seen[d] {
				merged.DisableMoxins = append(merged.DisableMoxins, d)
				seen[d] = true
			}
		}
	}

	// allow-tools and pass-tools narrow down the hierarchy together. A layer
	// that sets allow-tools holds every tool at use, pass or none; the first
	// such layer establishes the levels, and every later one can only lower
	// them: per tool, the merge keeps the lowest level either side holds it
	// at. A layer without allow-tools says nothing about tools.
	if overlay.AllowTools != nil {
		if merged.AllowTools == nil {
			merged.AllowTools = IntersectAllowTools(overlay.AllowTools, overlay.AllowTools)
			merged.PassTools = nonEmptyOrNil(overlay.PassTools)
		} else {
			merged.AllowTools, merged.PassTools = meetToolLevels(
				merged.AllowTools, merged.PassTools,
				overlay.AllowTools, overlay.PassTools,
			)
		}
	}

	// A record of a past discovery, not configuration: nothing to combine, the
	// closest file that carries one is the one that was written last.
	if overlay.ResolvedMoxins != nil {
		merged.ResolvedMoxins = overlay.ResolvedMoxins
	}

	if len(overlay.DisableServers) > 0 {
		seen := make(map[string]bool, len(merged.DisableServers))
		for _, d := range merged.DisableServers {
			seen[d] = true
		}
		for _, d := range overlay.DisableServers {
			if !seen[d] {
				merged.DisableServers = append(merged.DisableServers, d)
				seen[d] = true
			}
		}
	}

	for _, srv := range overlay.Servers {
		found := false
		for i, existing := range merged.Servers {
			if existing.Name == srv.Name {
				merged.Servers[i] = srv
				found = true
				break
			}
		}
		if !found {
			merged.Servers = append(merged.Servers, srv)
		}
	}

	return merged
}

func LoadHierarchy(home, dir string) (Hierarchy, error) {
	return loadHierarchy(home, dir, Parse)
}

// LoadDefaultHierarchyLiteral is LoadHierarchyLiteral for the user's home and
// the working directory.
func LoadDefaultHierarchyLiteral() (Hierarchy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Hierarchy{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Hierarchy{}, err
	}

	return LoadHierarchyLiteral(home, cwd)
}

// ExpandHeaders returns cfg with $VAR references in server header values
// expanded against the process environment, leaving cfg itself untouched. It
// turns a literally-loaded config into the one Parse would have produced, so
// a caller can judge the literal form and then serve the very same read.
func ExpandHeaders(cfg Config) Config {
	expanded := cfg
	expanded.Servers = append([]ServerConfig(nil), cfg.Servers...)
	for i, srv := range expanded.Servers {
		if srv.Headers == nil {
			continue
		}
		headers := make(map[string]string, len(srv.Headers))
		for k, v := range srv.Headers {
			headers[k] = os.ExpandEnv(v)
		}
		expanded.Servers[i].Headers = headers
	}
	return expanded
}

// LoadHierarchyLiteral is LoadHierarchy without $VAR expansion of header
// values (see ParseLiteral).
func LoadHierarchyLiteral(home, dir string) (Hierarchy, error) {
	return loadHierarchy(home, dir, ParseLiteral)
}

// LoadFileLiteral loads exactly one moxyfile plus whatever it includes — no
// global config, no $HOME→CWD walk — without $VAR expansion of header values.
// Unlike the hierarchy walk, where every level is optional, a missing path is
// an error: the caller named this file.
func LoadFileLiteral(path string) (Hierarchy, error) {
	if _, found := fileExists(path); !found {
		return Hierarchy{}, fmt.Errorf("reading moxyfile: %s: no such file", path)
	}
	l := hierarchyLoader{parse: ParseLiteral}
	if err := l.loadAndMerge(path); err != nil {
		return Hierarchy{}, err
	}
	return Hierarchy{Sources: l.sources, Merged: l.merged}, nil
}

// LoadReferenceLiteral loads a moxyfile reference that may be either a
// directory (walked as if it were the working directory, global config
// included) or a single file (that file plus its includes only).
func LoadReferenceLiteral(home, path string) (Hierarchy, error) {
	info, found := fileExists(path)
	if !found {
		return Hierarchy{}, fmt.Errorf("reading moxyfile: %s: no such file or directory", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Hierarchy{}, err
	}
	if info.IsDir() {
		return LoadHierarchyLiteral(home, abs)
	}
	return LoadFileLiteral(abs)
}

type hierarchyLoader struct {
	parse   func([]byte) (Config, error)
	sources []LoadSource
	merged  Config
}

// mergeIncludes recursively loads and merges the `include` files declared
// by cfg, in listed order, BEFORE the including file itself is merged — so
// an include acts as defaults the including file (and the rest of the
// $HOME→CWD walk) can still override. baseDir is the directory of the
// including file, against which relative include paths are resolved.
// visiting tracks the resolved absolute paths currently on the include
// stack so an include cycle (A→B→A, or a self-include) is reported instead
// of looping forever (#407).
func (l *hierarchyLoader) mergeIncludes(cfg Config, baseDir string, visiting map[string]bool) error {
	for _, raw := range cfg.Include {
		incPath := resolveIncludePath(raw, baseDir)
		if visiting[incPath] {
			return fmt.Errorf("moxyfile include cycle detected at %q", incPath)
		}
		incCfg, err := loadWith(incPath, l.parse)
		if err != nil {
			return fmt.Errorf("loading included moxyfile %q: %w", incPath, err)
		}
		_, found := fileExists(incPath)
		visiting[incPath] = true
		if err := l.mergeIncludes(incCfg, filepath.Dir(incPath), visiting); err != nil {
			return err
		}
		delete(visiting, incPath)
		l.sources = append(
			l.sources,
			LoadSource{Path: incPath, Found: found, File: incCfg},
		)
		if found {
			l.merged = Merge(l.merged, incCfg)
		}
	}
	return nil
}

func (l *hierarchyLoader) loadAndMerge(path string) error {
	cfg, err := loadWith(path, l.parse)
	if err != nil {
		return err
	}
	_, found := fileExists(path)
	if found {
		visiting := map[string]bool{filepath.Clean(path): true}
		if err := l.mergeIncludes(cfg, filepath.Dir(path), visiting); err != nil {
			return err
		}
	}
	l.sources = append(
		l.sources,
		LoadSource{Path: path, Found: found, File: cfg},
	)
	if found {
		l.merged = Merge(l.merged, cfg)
	}
	return nil
}

func loadHierarchy(home, dir string, parse func([]byte) (Config, error)) (Hierarchy, error) {
	l := hierarchyLoader{parse: parse}
	loadAndMerge := l.loadAndMerge

	// 1. Global config
	globalPath := filepath.Join(home, ".config", "moxy", "moxyfile")
	if err := loadAndMerge(globalPath); err != nil {
		return Hierarchy{}, err
	}

	// 2. Intermediate parent directories walking down from home to dir
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)

	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			parentPath := filepath.Join(parentDir, "moxyfile")
			if err := loadAndMerge(parentPath); err != nil {
				return Hierarchy{}, err
			}
		}
	}

	// 3. Target directory moxyfile
	dirPath := filepath.Join(cleanDir, "moxyfile")
	if err := loadAndMerge(dirPath); err != nil {
		return Hierarchy{}, err
	}

	return Hierarchy{Sources: l.sources, Merged: l.merged}, nil
}

func fileExists(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	return info, true
}

// resolveIncludePath expands $VAR and a leading ~/ in an `include` entry, then
// resolves a still-relative path against baseDir (the directory of the file
// that declared the include). The result is cleaned so cycle detection keys on
// a canonical spelling.
func resolveIncludePath(raw, baseDir string) string {
	p := os.ExpandEnv(raw)
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	return filepath.Clean(p)
}

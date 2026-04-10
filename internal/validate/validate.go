package validate

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/native"
)

type tapWriter struct {
	w      io.Writer
	n      int
	failed bool
}

func newTapWriter(w io.Writer) *tapWriter {
	fmt.Fprintln(w, "TAP version 14")
	return &tapWriter{w: w}
}

func (tw *tapWriter) ok(description string) {
	tw.n++
	fmt.Fprintf(tw.w, "ok %d - %s\n", tw.n, description)
}

func (tw *tapWriter) notOk(description string, diag map[string]string) {
	tw.n++
	tw.failed = true
	fmt.Fprintf(tw.w, "not ok %d - %s\n", tw.n, description)
	if len(diag) > 0 {
		fmt.Fprintln(tw.w, "  ---")
		for k, v := range diag {
			fmt.Fprintf(tw.w, "  %s: %s\n", k, v)
		}
		fmt.Fprintln(tw.w, "  ...")
	}
}

func (tw *tapWriter) skip(description, reason string) {
	tw.n++
	fmt.Fprintf(tw.w, "ok %d - %s # SKIP %s\n", tw.n, description, reason)
}

func (tw *tapWriter) plan() {
	fmt.Fprintf(tw.w, "1..%d\n", tw.n)
}

func checkServers(servers []config.ServerConfig, checkPath bool) []string {
	var issues []string
	seen := make(map[string]bool)
	for _, srv := range servers {
		if srv.Name == "" {
			issues = append(issues, "server has no name")
		}

		if srv.IsHTTP() {
			if _, err := url.ParseRequestURI(srv.URL); err != nil {
				issues = append(issues, fmt.Sprintf("server %q has invalid url %q", srv.Name, srv.URL))
			}
		} else if srv.Command.IsEmpty() {
			issues = append(issues, fmt.Sprintf("server %q has no command or url", srv.Name))
		} else if checkPath {
			if _, err := exec.LookPath(srv.Command.Executable()); err != nil {
				issues = append(issues, fmt.Sprintf("server %q command %q not found on $PATH", srv.Name, srv.Command.Executable()))
			}
		}

		if seen[srv.Name] {
			issues = append(issues, fmt.Sprintf("duplicate server name %q", srv.Name))
		}
		seen[srv.Name] = true
	}
	return issues
}

// Run validates the moxyfile hierarchy and writes TAP output.
// Returns 0 if all checks pass, 1 if any fail.
func Run(w io.Writer, home, dir string) int {
	tw := newTapWriter(w)

	result, err := config.LoadHierarchy(home, dir)
	if err != nil {
		tw.notOk("load hierarchy", map[string]string{
			"message": err.Error(),
		})
		tw.plan()
		return 1
	}

	for _, src := range result.Sources {
		if !src.Found {
			tw.skip(src.Path, "not found")
			continue
		}

		data, readErr := os.ReadFile(src.Path)
		if readErr != nil {
			tw.notOk(src.Path, map[string]string{
				"message": readErr.Error(),
			})
			continue
		}

		_, parseErr := config.Parse(data)
		if parseErr != nil {
			tw.notOk(src.Path+" valid TOML", map[string]string{
				"message": parseErr.Error(),
			})
			continue
		}

		tw.ok(src.Path + " valid")

		if issues := checkServers(src.File.Servers, false); len(issues) > 0 {
			for _, iss := range issues {
				tw.notOk(src.Path+" servers", map[string]string{
					"message": iss,
				})
			}
		}
	}

	// Validate moxin configs from MOXIN_PATH + system moxins.
	moxinDirs := native.ParseMoxinPath(os.Getenv("MOXIN_PATH"))
	if sysDir := native.SystemMoxinDir(); sysDir != "" {
		moxinDirs = append(moxinDirs, sysDir)
	}
	var moxinCount int
	moxinNames := make(map[string]bool)
	for _, moxinDir := range moxinDirs {
		entries, err := os.ReadDir(moxinDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			tw.notOk(moxinDir, map[string]string{
				"message": err.Error(),
			})
			continue
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
				continue
			}
			path := filepath.Join(moxinDir, e.Name())
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				tw.notOk(path, map[string]string{
					"message": readErr.Error(),
				})
				continue
			}

			result, parseErr := native.ParseConfigFull(data)
			if parseErr != nil {
				tw.notOk(path+" valid", map[string]string{
					"message": parseErr.Error(),
				})
				continue
			}

			tw.ok(path + " valid")

			if len(result.Undecoded) > 0 {
				tw.notOk(path+" undecoded keys", map[string]string{
					"message": strings.Join(result.Undecoded, ", "),
					"hint":    "unknown keys in moxin config",
				})
			}

			moxinNames[result.Config.Name] = true
			moxinCount++
		}
	}

	if moxinCount > 0 {
		tw.ok(fmt.Sprintf("moxin: %d server(s)", moxinCount))
	}

	// Validate merged result
	merged := result.Merged
	if len(merged.Servers) == 0 {
		tw.notOk("merged: has servers", map[string]string{
			"message": "no servers configured in any moxyfile",
		})
	} else {
		tw.ok(fmt.Sprintf("merged: %d server(s)", len(merged.Servers)))
	}

	if issues := checkServers(merged.Servers, true); len(issues) > 0 {
		for _, iss := range issues {
			tw.notOk("merged: "+iss, nil)
		}
	} else if len(merged.Servers) > 0 {
		tw.ok("merged: all servers valid")
	}

	tw.plan()

	if tw.failed {
		return 1
	}
	return 0
}

package status

import (
	"bytes"
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

// Run prints a unified status view of the moxyfile hierarchy, moxin
// directories, and validation results. Uses the same discovery path as the
// server runtime (native.DiscoverAll). Returns 0 if all checks pass, 1 if any
// fail.
func Run(w io.Writer, home, dir string) int {
	hierarchy, err := config.LoadHierarchy(home, dir)
	if err != nil {
		fmt.Fprintf(w, "error: loading moxyfile hierarchy: %v\n", err)
		return 1
	}

	moxinPath := os.Getenv("MOXIN_PATH")
	systemDir := native.SystemMoxinDir()

	// Same discovery path the server runtime uses.
	discovered, err := native.DiscoverAll(moxinPath, systemDir)
	if err != nil {
		fmt.Fprintf(w, "error: discovering moxins: %v\n", err)
		return 1
	}

	effectivePath := moxinPath
	if effectivePath == "" {
		if home != "" && dir != "" {
			effectivePath = native.DefaultMoxinPath(home, dir, systemDir)
		}
	}

	failed := false

	// --- Moxyfile hierarchy ---
	disableServerSet := hierarchy.Merged.BuildDisableServerSet()

	fmt.Fprintln(w, "Moxyfile hierarchy:")
	for _, src := range hierarchy.Sources {
		fmt.Fprintln(w)
		if !src.Found {
			fmt.Fprintf(w, "  %s (not found)\n", src.Path)
			continue
		}
		fmt.Fprintf(w, "  %s\n", src.Path)
		if len(src.File.Servers) == 0 && len(src.File.DisableServers) == 0 {
			fmt.Fprintln(w, "    (no servers)")
		}
		for _, srv := range src.File.Servers {
			suffix := ""
			if disableServerSet.ServerDisabled(srv.Name) {
				suffix = " [disabled]"
			}
			fmt.Fprintf(w, "    %-24s %s%s\n", srv.Name, serverSummary(srv), suffix)
		}
		if len(src.File.DisableServers) > 0 {
			fmt.Fprintf(w, "    disable-servers: %s\n", strings.Join(src.File.DisableServers, ", "))
		}
	}

	fmt.Fprintln(w)
	disabledServerCount := 0
	for _, srv := range hierarchy.Merged.Servers {
		if disableServerSet.ServerDisabled(srv.Name) {
			disabledServerCount++
		}
	}
	activeServers := len(hierarchy.Merged.Servers) - disabledServerCount
	if disabledServerCount > 0 {
		fmt.Fprintf(w, "  Merged: %d server(s) (%d disabled)\n", activeServers, disabledServerCount)
	} else if len(hierarchy.Merged.Servers) == 0 {
		fmt.Fprintln(w, "  Merged: 0 server(s)")
	} else {
		fmt.Fprintf(w, "  Merged: %d server(s)\n", len(hierarchy.Merged.Servers))
	}

	// --- Moxin directories ---
	// Group merged results by their parent directory (SourceDir) to show
	// per-directory breakdown. The configs are already merged (last-wins by
	// name), so what we display is exactly what the server would register.
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Moxin path:")

	moxinsByDir := make(map[string][]*native.NativeConfig)
	for _, nc := range discovered.Configs {
		parentDir := filepath.Dir(nc.SourceDir)
		moxinsByDir[parentDir] = append(moxinsByDir[parentDir], nc)
	}
	errorsByDir := make(map[string][]native.MoxinError)
	for _, me := range discovered.Errors {
		parentDir := filepath.Dir(me.Dir)
		errorsByDir[parentDir] = append(errorsByDir[parentDir], me)
	}

	disableSet := hierarchy.Merged.BuildDisableMoxinSet()

	var totalTools int
	var disabledServers, disabledTools int
	for _, d := range discovered.Dirs {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", d)

		configs := moxinsByDir[d]
		errors := errorsByDir[d]

		if len(configs) == 0 && len(errors) == 0 {
			fmt.Fprintln(w, "    (none active)")
		}
		for _, nc := range configs {
			if disableSet.ServerDisabled(nc.Name) {
				fmt.Fprintf(w, "    %-24s %d tools [disabled]\n", nc.Name, len(nc.Tools))
				disabledServers++
				continue
			}
			activeTools := 0
			var disabledToolNames []string
			for _, t := range nc.Tools {
				if disableSet.ToolDisabled(nc.Name, t.Name) {
					disabledToolNames = append(disabledToolNames, t.Name)
					disabledTools++
				} else {
					activeTools++
				}
			}
			totalTools += activeTools
			if len(disabledToolNames) > 0 {
				fmt.Fprintf(w, "    %-24s %d tools (%d disabled: %s)\n",
					nc.Name, activeTools, len(disabledToolNames),
					strings.Join(disabledToolNames, ", "))
			} else {
				fmt.Fprintf(w, "    %-24s %d tools\n", nc.Name, len(nc.Tools))
			}
		}
		for _, me := range errors {
			fmt.Fprintf(w, "    %-24s FAILED: %v\n", filepath.Base(me.Dir), me.Err)
		}
	}

	fmt.Fprintln(w)
	activeMoxins := len(discovered.Configs) - disabledServers
	if disabledServers > 0 || disabledTools > 0 {
		fmt.Fprintf(w, "  Merged: %d moxin(s), %d tools (%d server(s) disabled, %d tool(s) disabled)\n",
			activeMoxins, totalTools, disabledServers, disabledTools)
	} else {
		fmt.Fprintf(w, "  Merged: %d moxin(s), %d tools\n", activeMoxins, totalTools)
	}

	// --- MOXIN_PATH ---
	fmt.Fprintln(w)
	fmt.Fprintf(w, "MOXIN_PATH: %s\n", effectivePath)

	// --- Validation ---
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Validation:")

	// Validate each moxyfile source
	for _, src := range hierarchy.Sources {
		if !src.Found {
			continue
		}

		data, readErr := os.ReadFile(src.Path)
		if readErr != nil {
			printFail(w, "%s: %v", src.Path, readErr)
			failed = true
			continue
		}

		if _, parseErr := config.Parse(data); parseErr != nil {
			printFail(w, "%s valid TOML: %v", src.Path, parseErr)
			failed = true
			continue
		}

		printOk(w, "%s valid", src.Path)

		if issues := checkServers(src.File.Servers, false); len(issues) > 0 {
			for _, iss := range issues {
				printFail(w, "%s: %s", src.Path, iss)
				failed = true
			}
		}
	}

	// Validate merged server commands on PATH
	if len(hierarchy.Merged.Servers) == 0 {
		printFail(w, "merged: no servers configured in any moxyfile")
		failed = true
	} else {
		if issues := checkServers(hierarchy.Merged.Servers, true); len(issues) > 0 {
			for _, iss := range issues {
				printFail(w, "merged: %s", iss)
				failed = true
			}
		} else {
			printOk(w, "merged: all servers valid")
		}
	}

	// Validate moxin configs (uses the same dirs the discovery scanned)
	var moxinCount int
	for _, moxinDir := range discovered.Dirs {
		entries, err := os.ReadDir(moxinDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			printFail(w, "%s: %v", moxinDir, err)
			failed = true
			continue
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dirPath := filepath.Join(moxinDir, e.Name())
			metaPath := filepath.Join(dirPath, "_moxin.toml")
			if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
				continue
			}

			moxinResult, parseErr := native.ParseMoxinDirFull(dirPath)
			if parseErr != nil {
				printFail(w, "%s valid: %v", dirPath, parseErr)
				failed = true
				continue
			}

			printOk(w, "%s valid", dirPath)

			if len(moxinResult.Undecoded) > 0 {
				printFail(w, "%s undecoded keys: %s", dirPath, strings.Join(moxinResult.Undecoded, ", "))
				failed = true
			}

			for _, warn := range moxinResult.Warnings {
				printWarn(w, "%s: %s", dirPath, warn)
			}

			moxinCount++
		}
	}

	if moxinCount > 0 {
		printOk(w, "moxin: %d server(s)", moxinCount)
	}

	if !failed {
		printOk(w, "all checks passed")
	}

	if failed {
		return 1
	}
	return 0
}

// Format returns the status view as a string (for MCP tool output).
func Format(home, dir string) (string, error) {
	var buf bytes.Buffer
	Run(&buf, home, dir)
	return buf.String(), nil
}

func serverSummary(srv config.ServerConfig) string {
	if srv.IsHTTP() {
		return "url=" + srv.URL
	}
	return "command=" + srv.Command.Executable()
}

func printOk(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "  ok   %s\n", fmt.Sprintf(format, args...))
}

func printFail(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "  FAIL %s\n", fmt.Sprintf(format, args...))
}

func printWarn(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "  WARN %s\n", fmt.Sprintf(format, args...))
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

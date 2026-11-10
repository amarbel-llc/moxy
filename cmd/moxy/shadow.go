package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"code.linenisgreat.com/moxy/pkg/moxy"
	"code.linenisgreat.com/moxy/pkg/shadow"
)

// shadowEnv turns ajour shadow mode on (moxy#489): every tool dispatch is
// turned into a Call, evaluated, and logged as {tool, field names and types,
// verdict}, never values, to <home>/.local/log/moxy/shadow/<session>.jsonl
// (mode 0600). It enforces nothing and changes no call's outcome. Unset (or
// anything but "1") is off, which is the default.
const shadowEnv = "MOXY_SHADOW"

// shadowQueue is how many dispatches wait for the log writer before further
// ones are dropped.
const shadowQueue = 256

// shadowFromEnv builds the observer when MOXY_SHADOW=1 and a closer that
// drains it. It returns a nil observer (and a no-op closer) when shadow mode
// is off or its log cannot be opened: shadow mode never stops moxy from
// serving.
func shadowFromEnv(getenv func(string) string, home, cwd, sessionID string, stderr io.Writer) (moxy.CallObserver, func()) {
	noop := func() {}
	if getenv(shadowEnv) != "1" {
		return nil, noop
	}
	f, err := shadow.OpenLog(filepath.Join(home, ".local", "log", "moxy"), sessionID)
	if err != nil {
		fmt.Fprintf(stderr, "moxy: shadow mode requested but the log could not be opened: %v\n", err)
		return nil, noop
	}
	builder, err := shadow.NewBuilder(shadow.Env{
		Cwd:    cwd,
		Home:   home,
		Remote: gitRemoteOf(cwd),
	})
	if err != nil {
		f.Close()
		fmt.Fprintf(stderr, "moxy: shadow mode requested but the catalog could not be loaded: %v\n", err)
		return nil, noop
	}
	// No evaluator yet: ajour.Evaluate plugs in when cutting-garden is a
	// flake input; until then each dispatch is logged "unevaluated".
	async := shadow.NewAsync(shadow.NewObserver(builder, nil, f), shadowQueue)
	fmt.Fprintf(stderr, "moxy: shadow mode on: logging to %s (evaluates and enforces nothing)\n", f.Name())
	return async, func() {
		async.Close()
		f.Close()
	}
}

// gitRemoteOf resolves the working directory's origin remote once, on first
// use, for an omitted smith repo/host.
func gitRemoteOf(cwd string) func() (string, string, bool) {
	var (
		once       sync.Once
		repo, host string
		ok         bool
	)
	return func() (string, string, bool) {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "git", "-C", cwd, "remote", "get-url", "origin").Output()
			if err != nil {
				return
			}
			repo, host, ok = shadow.ParseRemote(string(out))
		})
		return repo, host, ok
	}
}

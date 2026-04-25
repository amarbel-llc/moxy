// Added for moxy POC dynamic-perms
package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DynamicPermsDecision is one of "allow", "ask", "deny", or "" (fall-through).
// The empty string lets the hook caller delegate to the client (matching the
// existing delegate-to-client convention when no decision is asserted).
type DynamicPermsDecision string

const (
	DynPermsAllow       DynamicPermsDecision = "allow"
	DynPermsAsk         DynamicPermsDecision = "ask"
	DynPermsDeny        DynamicPermsDecision = "deny"
	DynPermsFallThrough DynamicPermsDecision = ""
)

// DefaultDynamicPermsTimeout is used when the spec sets timeout-ms = 0.
const DefaultDynamicPermsTimeout = 2 * time.Second

// EvalDynamicPerms runs the dynamic-perms predicate for one tool call and
// returns a decision plus a human-readable reason for the hook to surface.
//
// Exit code mapping:
//
//	0 → allow
//	1 → ask
//	2 → deny
//	other → fall-through (empty decision; client decides)
//
// On timeout, spawn error, or unmapped non-zero exit, the call collapses to
// "ask" and the reason describes what went wrong so the user understands why
// they're being prompted.
func EvalDynamicPerms(
	ctx context.Context,
	spec *DynamicPermsSpec,
	mainInputSchema json.RawMessage,
	arguments json.RawMessage,
) (DynamicPermsDecision, string) {
	if spec == nil {
		return DynPermsFallThrough, "no dynamic-perms spec configured"
	}

	timeout := DefaultDynamicPermsTimeout
	if spec.TimeoutMS > 0 {
		timeout = time.Duration(spec.TimeoutMS) * time.Millisecond
	}

	stdinContent, argv, err := shapeDynamicPermsInput(spec, arguments)
	if err != nil {
		return DynPermsAsk, fmt.Sprintf("dynamic-perms input shaping failed: %v", err)
	}

	allArgs := make([]string, 0, len(spec.Args)+len(argv))
	allArgs = append(allArgs, spec.Args...)
	allArgs = append(allArgs, argv...)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Command, allArgs...)
	if stdinContent != "" {
		cmd.Stdin = strings.NewReader(stdinContent)
	}

	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		// Timeout takes precedence: when the context deadline kills the
		// child, exec also returns an ExitError with code -1. Check the
		// context first so we surface the timeout reason instead of the
		// signal-exit code.
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return DynPermsAsk, fmt.Sprintf("dynamic-perms script timed out after %s", timeout)
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return mapExitCode(exitErr.ExitCode(), out)
		}
		// Spawn-time failure (binary missing, permission denied).
		return DynPermsAsk, fmt.Sprintf("dynamic-perms script failed to run: %v", runErr)
	}
	// Exit 0 — script succeeded with no error.
	return DynPermsAllow, reasonFromOutput(0, out)
}

// shapeDynamicPermsInput mirrors server.go's stdin+argv extraction so the
// dynamic-perms script sees the same shape as the main tool would.
func shapeDynamicPermsInput(spec *DynamicPermsSpec, arguments json.RawMessage) (string, []string, error) {
	var stdinContent string
	args := arguments

	if spec.StdinParam != "" && len(args) > 0 {
		var argMap map[string]json.RawMessage
		if err := json.Unmarshal(args, &argMap); err == nil {
			if raw, ok := argMap[spec.StdinParam]; ok {
				var val string
				if err := json.Unmarshal(raw, &val); err == nil {
					stdinContent = val
				}
				delete(argMap, spec.StdinParam)
				args, _ = json.Marshal(argMap)
			}
		}
	}

	extra, err := BuildExtraArgs(args, nil, spec.ArgOrder)
	if err != nil {
		return "", nil, err
	}
	return stdinContent, extra, nil
}

func mapExitCode(code int, output []byte) (DynamicPermsDecision, string) {
	switch code {
	case 0:
		return DynPermsAllow, reasonFromOutput(0, output)
	case 1:
		return DynPermsAsk, reasonFromOutput(1, output)
	case 2:
		return DynPermsDeny, reasonFromOutput(2, output)
	default:
		return DynPermsAsk, fmt.Sprintf("dynamic-perms script exited with unmapped code %d: %s",
			code, truncate(string(output), 200))
	}
}

func reasonFromOutput(code int, output []byte) string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return fmt.Sprintf("dynamic-perms exit %d", code)
	}
	return fmt.Sprintf("dynamic-perms exit %d: %s", code, truncate(trimmed, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

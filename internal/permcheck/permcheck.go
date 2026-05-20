// Package permcheck resolves moxin tool permission decisions.
//
// It mirrors the resolver previously embedded in internal/hook. The
// PreToolUse hook adapter and the proxy's batch meta-tool both call
// Resolver.Resolve to decide whether a given tool call is allowed,
// asked, denied, or unknown (deny-by-default for non-moxin tools).
package permcheck

import (
	"context"
	"encoding/json"
	"os"

	"github.com/amarbel-llc/moxy/internal/native"
)

// Decision is the resolved permission outcome for one tool call.
type Decision string

const (
	Allow   Decision = "allow"
	Ask     Decision = "ask"
	Deny    Decision = "deny"
	Unknown Decision = "" // fall-through: tool has no moxin perm-request
)

// ToolPermInfo carries the resolver inputs for one tool.
type ToolPermInfo struct {
	Perm         native.PermsRequest
	DynamicPerms *native.DynamicPermsSpec
}

// Resolver caches the moxin perms map and resolves decisions per call.
type Resolver struct {
	perms map[string]ToolPermInfo
}

// NewResolver walks MOXIN_PATH (and the system moxin dir) once and
// caches every tool's perms-request. Tools without an explicit
// perms-request are omitted from the map.
func NewResolver() (*Resolver, error) {
	perms, err := discoverPermissions()
	if err != nil {
		return nil, err
	}
	return &Resolver{perms: perms}, nil
}

// Resolve returns the decision for toolName ("<server>.<tool>" form).
// args is the sub-call's JSON args, fed to dynamic-perms when relevant.
// cwd is the working directory the dynamic-perms script runs in.
func (r *Resolver) Resolve(
	ctx context.Context,
	toolName string,
	args json.RawMessage,
	cwd string,
) (Decision, string) {
	info, ok := r.perms[toolName]
	if !ok {
		return Unknown, "no moxin perm-request for " + toolName
	}
	switch info.Perm {
	case native.PermsAlwaysAllow:
		return Allow, "always-allow by moxin config"
	case native.PermsEachUse:
		return Ask, "each-use: requires explicit approval"
	case native.PermsDynamic:
		return evalDynamic(ctx, info.DynamicPerms, args, cwd)
	default:
		return Unknown, "delegate-to-client or unrecognized perms-request"
	}
}

// evalDynamic runs the per-tool dynamic-perms predicate and maps its
// decision into (Decision, reason).
func evalDynamic(
	ctx context.Context,
	spec *native.DynamicPermsSpec,
	args json.RawMessage,
	cwd string,
) (Decision, string) {
	if spec == nil {
		return Unknown, "dynamic-perms: no [dynamic-perms] spec on tool"
	}
	dec, reason := native.EvalDynamicPermsInDir(ctx, spec, nil, args, cwd)
	switch dec {
	case native.DynPermsAllow:
		return Allow, reason
	case native.DynPermsAsk:
		return Ask, reason
	case native.DynPermsDeny:
		return Deny, reason
	default:
		return Unknown, reason
	}
}

// discoverPermissions walks MOXIN_PATH and the system moxin dir, then
// returns a map of "server.tool" names to their perm info.
func discoverPermissions() (map[string]ToolPermInfo, error) {
	moxinPath := os.Getenv("MOXIN_PATH")
	systemDir := native.SystemMoxinDir()
	configs, err := native.DiscoverConfigs(moxinPath, systemDir)
	if err != nil {
		return nil, err
	}
	perms := make(map[string]ToolPermInfo)
	for _, cfg := range configs {
		for _, tool := range cfg.Tools {
			if tool.PermsRequest != "" {
				perms[cfg.Name+"."+tool.Name] = ToolPermInfo{
					Perm:         tool.PermsRequest,
					DynamicPerms: tool.DynamicPerms,
				}
			}
		}
	}
	return perms, nil
}

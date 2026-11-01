package perms

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"code.linenisgreat.com/moxy/internal/native"
)

// Op is the operation a tool performs on a resource.
type Op string

const (
	Read  Op = "read"
	Write Op = "write"
	Exec  Op = "exec"
)

// Resource kinds.
const (
	KindFile = "file" // URI is file:///<absolute path>
	KindBlob = "blob" // URI is madder://blobs/<digest>
)

// Resource names a file or blob a tool call touches (moxy#491, RFC 0002).
type Resource struct {
	Kind string
	URI  string
}

// OpResource is a resource with the operation applied to it.
type OpResource struct {
	Op       Op
	Resource Resource
}

// Gate answers whether an operation on a resource may proceed. It is the
// policy-broker client's seat in the resolver (RFC 0002). answered=false is
// "no answer": the connection is down, the hello was refused, or the deadline
// passed; it is the only case in which the static floor decides. A Gate never
// resolves absence to a decision itself.
type Gate interface {
	Check(ctx context.Context, op Op, res Resource) (dec Decision, reason string, answered bool)
}

// SetGate installs the gate consulted for tools that declare resources.
// A nil gate (the default) is today's behaviour: the floor decides alone.
func (r *Resolver) SetGate(g Gate) { r.gate = g }

// HasGate reports whether a gate is installed.
func (r *Resolver) HasGate() bool { return r.gate != nil }

// ResourcesOf extracts the resources a call names from its JSON arguments,
// per the tool's declared resource inputs. Relative paths are made absolute
// against cwd. Empty values and /dev/fd/* (the pipe a blob URI is substituted
// with) name nothing.
func ResourcesOf(specs []native.ResourceArg, args json.RawMessage, cwd string) []OpResource {
	if len(specs) == 0 || len(args) == 0 {
		return nil
	}
	var argMap map[string]json.RawMessage
	if err := json.Unmarshal(args, &argMap); err != nil {
		return nil
	}
	var out []OpResource
	for _, spec := range specs {
		raw, ok := argMap[spec.Param]
		if !ok {
			continue
		}
		var values []string
		var one string
		var many []string
		switch {
		case json.Unmarshal(raw, &one) == nil:
			values = []string{one}
		case json.Unmarshal(raw, &many) == nil:
			values = many
		}
		for _, v := range values {
			if spec.Split {
				for _, piece := range strings.Fields(v) {
					out = appendResource(out, Op(spec.Op), piece, cwd)
				}
				continue
			}
			out = appendResource(out, Op(spec.Op), v, cwd)
		}
	}
	return out
}

func appendResource(out []OpResource, op Op, value, cwd string) []OpResource {
	switch {
	case value == "", strings.HasPrefix(value, "/dev/fd/"):
		return out
	case strings.HasPrefix(value, "madder://blobs/"):
		return append(out, OpResource{Op: op, Resource: Resource{Kind: KindBlob, URI: value}})
	}
	path := strings.TrimPrefix(value, "file://")
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return append(out, OpResource{Op: op, Resource: Resource{Kind: KindFile, URI: "file://" + canonicalPath(path)}})
}

// canonicalPath cleans path and resolves symlinks in everything above the
// leaf, like `realpath -m` on the parent: the leaf itself is not followed (a
// `result -> /nix/store/...` link in the worktree is a worktree path), and a
// parent that does not exist yet keeps its missing tail.
func canonicalPath(path string) string {
	path = filepath.Clean(path)
	dir, leaf := filepath.Split(path)
	dir = filepath.Clean(dir)
	if leaf == "" {
		return dir
	}
	var tail []string
	for p := dir; ; p = filepath.Dir(p) {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			parts := append([]string{resolved}, tail...)
			return filepath.Join(append(parts, leaf)...)
		}
		if _, err := os.Lstat(p); err == nil || p == filepath.Dir(p) {
			// unresolvable but present, or the root: give up resolving.
			break
		}
		tail = append([]string{filepath.Base(p)}, tail...)
	}
	return path
}

// rank orders decisions by strictness; Unknown (the client decides) sits
// between Ask and Allow so that merging with it never widens.
func rank(d Decision) int {
	switch d {
	case Deny:
		return 3
	case Ask:
		return 2
	case Unknown:
		return 1
	}
	return 0
}

// gateResult is what the gate said about one call's resources.
type gateResult struct {
	dec      Decision
	reason   string
	any      bool // at least one resource was answered
	complete bool // every resource was answered
}

// consult asks the gate about each resource the call names. It reports
// nothing (any=false) when no gate is installed, the tool is not on the
// dynamic tier (a broker verdict never lifts each-use or always-allow), or
// the call names no resources.
func (r *Resolver) consult(ctx context.Context, info ToolPermInfo, args json.RawMessage, cwd string) gateResult {
	if r.gate == nil || info.Perm != native.PermsDynamic {
		return gateResult{}
	}
	resources := ResourcesOf(info.Resources, args, absCwd(cwd))
	if len(resources) == 0 {
		return gateResult{}
	}
	res := gateResult{dec: Allow, complete: true}
	for _, or := range resources {
		dec, reason, answered := r.gate.Check(ctx, or.Op, or.Resource)
		if !answered {
			res.complete = false
			continue
		}
		if !res.any || rank(dec) > rank(res.dec) {
			res.dec, res.reason = dec, reason
		}
		res.any = true
		if dec == Deny {
			// Deny is final for the call, answered or not for the rest.
			return gateResult{dec: Deny, reason: reason, any: true, complete: res.complete}
		}
	}
	if !res.any {
		return gateResult{}
	}
	return res
}

func absCwd(cwd string) string {
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

// ConsultGate returns the gate's verdict for a call, when it gave one: the
// strictest verdict among the resources it answered. answered=false means the
// gate said nothing (no gate, no resources, no answer). The funnel uses it
// under Delegate to refuse a broker deny that a client without a hook, or a
// hook that cannot see the broker, would have let through (moxy#485).
func (r *Resolver) ConsultGate(ctx context.Context, toolName string, args json.RawMessage, cwd string) (Decision, string, bool) {
	info, ok := r.perms[toolName]
	if !ok {
		return Unknown, "", false
	}
	res := r.consult(ctx, info, args, cwd)
	return res.dec, res.reason, res.any
}

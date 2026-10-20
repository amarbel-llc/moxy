package proxy

import (
	"context"
	"encoding/json"
	"fmt"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/pkg/perms"
)

// Serve-time permission enforcement (moxy#486, FDR 0015). Under a delegating
// policy the funnel resolves nothing, as it always did: a hook in front of
// moxy decides, and a client without one runs every tool it can name. Under
// an enforcing policy the funnel resolves the tool's tier right after the
// allow-tools gate and runs only what resolves to allow; ask, deny and
// unknown come back as an isError result that says nobody can answer, in the
// same shape as the hook's dontAsk denial (#484).

type (
	callCwdKey        struct{}
	policyOverrideKey struct{}
	decisionSinkKey   struct{}
)

// WithCallCwd records the directory a dynamic-perms predicate runs in for
// this call (the caller's working directory). Unset means ".", the
// batch/async convention.
func WithCallCwd(ctx context.Context, cwd string) context.Context {
	if cwd == "" {
		return ctx
	}
	return context.WithValue(ctx, callCwdKey{}, cwd)
}

func callCwd(ctx context.Context) string {
	if v, ok := ctx.Value(callCwdKey{}).(string); ok && v != "" {
		return v
	}
	return "."
}

// WithPolicyOverride tightens the funnel's policy for this call. The caller
// (pkg/moxy's Instance.Call) has already checked the override narrows the
// instance policy; the funnel applies whichever is stricter.
func WithPolicyOverride(ctx context.Context, p perms.Policy) context.Context {
	return context.WithValue(ctx, policyOverrideKey{}, p)
}

// WithDecisionSink asks the funnel to record the permission decision it
// resolved for this call into dec; it stays empty when nothing was consulted.
func WithDecisionSink(ctx context.Context, dec *perms.Decision) context.Context {
	return context.WithValue(ctx, decisionSinkKey{}, dec)
}

func recordDecision(ctx context.Context, dec perms.Decision) {
	if sink, ok := ctx.Value(decisionSinkKey{}).(*perms.Decision); ok && sink != nil {
		*sink = dec
	}
}

// carryCallValues captures the per-call values (cwd, policy override) of
// ctx and returns a function that re-attaches them to a detached job
// context, so an async job is judged as the call that dispatched it.
func carryCallValues(ctx context.Context) func(context.Context) context.Context {
	cwd, _ := ctx.Value(callCwdKey{}).(string)
	override, hasOverride := ctx.Value(policyOverrideKey{}).(perms.Policy)
	return func(jobCtx context.Context) context.Context {
		jobCtx = WithCallCwd(jobCtx, cwd)
		if hasOverride {
			jobCtx = WithPolicyOverride(jobCtx, override)
		}
		return jobCtx
	}
}

// SetPolicy installs the serve-time permission policy. Delegate (the zero
// value) is today's behaviour.
func (p *Proxy) SetPolicy(policy perms.Policy) {
	p.mu.Lock()
	p.policy = policy
	p.mu.Unlock()
}

// Policy returns the installed serve-time policy.
func (p *Proxy) Policy() perms.Policy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.policy
}

// effectivePolicy is the instance policy tightened by any per-call override.
func (p *Proxy) effectivePolicy(ctx context.Context) perms.Policy {
	policy := p.Policy()
	if override, ok := ctx.Value(policyOverrideKey{}).(perms.Policy); ok {
		policy = perms.Tighten(policy, override)
	}
	return policy
}

// metaToolDecision is what the funnel decides for moxy's own tools under
// enforce, mirroring the hook's treatment: status, async-result and
// async-cancel only read or cancel this session's own handles (allow); async
// and batch resolve the tools they carry when those are dispatched through
// this same funnel (allow here, each inner call judged on its own); restart
// churns children and has always prompted (ask, which under enforce nobody
// can answer).
func metaToolDecision(name string) (perms.Decision, string) {
	switch name {
	case "restart":
		return perms.Ask, "restart requires explicit approval"
	case "status", "async-result", "async-cancel", "async", "batch":
		return perms.Allow, "moxy meta tool"
	}
	return perms.Unknown, "no permission tier for builtin " + name
}

// enforceDecision resolves name's tier for an enforcing funnel. canonical is
// the "server.tool" name the resolver indexes by (empty for a builtin).
func (p *Proxy) enforceDecision(ctx context.Context, policy perms.Policy, name, canonical string, isBuiltin bool, args json.RawMessage) (perms.Decision, string) {
	if isBuiltin {
		return metaToolDecision(name)
	}
	if p.resolver == nil {
		return perms.Unknown, "no permission resolver configured"
	}
	dec, reason := p.resolver.Resolve(ctx, canonical, args, callCwd(ctx))
	if dec == perms.Unknown && policy.Grants(canonical) {
		return perms.Allow, "granted by enforce-allow (no perms-request declared)"
	}
	return dec, reason
}

// maxRefusalArgsBytes bounds how much of the arguments a refusal quotes back.
const maxRefusalArgsBytes = 400

// enforceRefusal is the isError result an enforcing funnel returns. It
// starts with "moxy denied", the marker the dontAsk hook denial uses, so a
// client can tell a permission refusal from an ordinary tool error.
func enforceRefusal(name string, args json.RawMessage, dec perms.Decision, reason string) *protocol.ToolCallResultV1 {
	argText := string(args)
	if argText == "" {
		argText = "{}"
	}
	if len(argText) > maxRefusalArgsBytes {
		argText = argText[:maxRefusalArgsBytes] + "…"
	}
	if reason == "" {
		reason = "no reason given"
	}
	var why string
	switch dec {
	case perms.Deny:
		why = fmt.Sprintf("its permission check denied it (%s).", reason)
	case perms.Ask:
		why = fmt.Sprintf("its permission check needs a human to approve this call (%s), "+
			"and this server enforces permissions itself, so nobody can answer. "+
			"Retry with arguments the check allows (for example a path inside the working tree), "+
			"or ask the operator to run it.", reason)
	default:
		why = fmt.Sprintf("it declares no permission tier (%s), and this server enforces permissions itself, "+
			"so an unreviewed tool does not run unattended. "+
			"The operator can grant it with enforce-allow in the moxyfile.", reason)
	}
	return protocol.ErrorResultV1(fmt.Sprintf("moxy denied %s with arguments %s: %s", name, argText, why))
}

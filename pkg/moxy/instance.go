package moxy

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
	"code.linenisgreat.com/purse-first/libs/go-mcp/server"

	"code.linenisgreat.com/moxy/internal/asyncjob"
	"code.linenisgreat.com/moxy/internal/credentials"
	"code.linenisgreat.com/moxy/internal/naming"
	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/proxy"
	"code.linenisgreat.com/moxy/internal/toolexclude"
	"code.linenisgreat.com/moxy/internal/toolfilter"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
	"code.linenisgreat.com/moxy/pkg/perms"
)

// Prepared is a moxy whose configuration has been loaded, parsed and judged
// against the parent reference, with nothing started yet. The binary splits
// the two halves so a refusal to serve reaches the launcher's stderr before
// stderr is redirected to the session log; an embedder usually calls Open.
type Prepared struct {
	opts    Options
	serving *serving
	cfg     moxyfile.Config // the judged config, header $VARs expanded
	filter  toolfilter.Filter
	tmpl    naming.Template
}

// Prepare resolves Options into a Prepared: the moxyfile is loaded in its
// literal form and judged against Parent together with the moxins discovery
// finds for it; --expose and --name-template selectors are parsed. Nothing is
// spawned and no port is bound. A moxyfile that widens Parent is a
// *WideningError.
func Prepare(o Options) (*Prepared, error) {
	filter, err := toolfilter.Parse(o.Expose)
	if err != nil {
		return nil, fmt.Errorf("expose: %w", err)
	}
	tmpl, err := naming.Parse(o.NameTemplate)
	if err != nil {
		return nil, fmt.Errorf("name-template: %w", err)
	}
	if _, ok := perms.ParseMode(string(o.Permissions.Mode)); !ok {
		return nil, fmt.Errorf("permissions: unknown mode %q", o.Permissions.Mode)
	}
	sv, err := newServing(o)
	if err != nil {
		return nil, err
	}
	literal, err := sv.src.load()
	if err != nil {
		return nil, err
	}
	// Judge once now, against the moxins discovery would find, so a refusal
	// is the first thing a launcher sees; bootstrap judges again against the
	// moxins it actually loads, and that later call pins the baseline.
	var moxins map[string]string
	if sv.judgesMoxins() {
		systemDir := o.SystemMoxinDir
		if systemDir == "" {
			systemDir = native.SystemMoxinDirFor(literal.BuiltinNative)
		} else if literal.BuiltinNative != nil && !*literal.BuiltinNative {
			systemDir = ""
		}
		if moxins, err = native.ResolvedMoxinSources(o.MoxinPath, sv.src.dirForDiscovery(), systemDir); err != nil {
			return nil, fmt.Errorf("discovering moxins: %w", err)
		}
	}
	if err := sv.refuseUnlessNarrows(literal, moxins, false); err != nil {
		return nil, err
	}
	return &Prepared{
		opts:    o,
		serving: sv,
		cfg:     moxyfile.ExpandHeaders(literal),
		filter:  filter,
		tmpl:    tmpl,
	}, nil
}

// Config is the judged, expanded configuration Prepare loaded. A Reload
// re-reads the source; this value does not change.
func (pr *Prepared) Config() moxyfile.Config { return pr.cfg }

// Open is Prepare followed by (*Prepared).Open.
func Open(ctx context.Context, o Options) (*Instance, error) {
	pr, err := Prepare(o)
	if err != nil {
		return nil, err
	}
	return pr.Open(ctx)
}

// Instance is a running moxy: its child servers (subprocess and moxin), the
// meta tools if enabled, and the call funnel every tool call goes through.
// It implements go-mcp's tool, resource and prompt provider interfaces, so a
// transport serves it as it would the proxy. Safe for concurrent use.
type Instance struct {
	p      *proxy.Proxy
	opts   Options
	blobs  BlobStore
	policy perms.Policy

	mu           sync.Mutex
	instructions string
	systemPrompt string
	closed       atomic.Bool
}

// Open starts the moxy Prepare described: discovers moxins, judges the
// configuration again against the moxins it will actually serve (pinning
// the allow-tools baseline), spawns every active [[servers]] entry, builds a
// native server per moxin, probes ephemeral servers, and checks the name
// template for collisions. Children that fail to start are not an error:
// they appear in Servers as failed and their <name>.status tool reports why.
func (pr *Prepared) Open(ctx context.Context) (*Instance, error) {
	o := pr.opts
	stderr := o.stderr()

	blobs := o.Blobs
	if blobs == nil {
		mc, err := native.NewMadderClient()
		if err != nil {
			return nil, fmt.Errorf("madder runtime: %w", err)
		}
		if err := mc.VerifyDefaultStore(ctx); err != nil {
			return nil, fmt.Errorf("madder default store: %w", err)
		}
		blobs = mc
	}

	connect := o.Connect
	if connect == nil {
		connect = defaultConnect(credentials.NewStore(pr.cfg.Credentials), stderr)
	}

	inputs := bootstrapInputs{
		serving:   pr.serving,
		moxinPath: o.MoxinPath,
		systemDir: o.SystemMoxinDir,
		sessionID: o.SessionID,
		connect:   connect,
		blobs:     blobs,
		stderr:    stderr,
	}
	res, err := bootstrap(ctx, inputs)
	if err != nil {
		return nil, err
	}
	cfg := res.cfg
	// Reloads keep the system dir the first bootstrap resolved: the moxyfile
	// may flip builtin-native later, and the reload honours that flip itself,
	// but it must not re-derive a different build default.
	inputs.systemDir = res.systemDir

	p := proxy.New(res.children, res.failed, res.activeServers, cfg.Ephemeral, cfg.ProgressiveDisclosure, connect)
	p.SetToolAllow(cfg.BuildAllowToolSet())
	if cfg.AllowTools != nil {
		fmt.Fprintf(stderr, "moxy: allow-tools active: %d entries; unlisted tools do not exist\n", len(cfg.AllowTools))
	}
	p.SetToolFilter(pr.filter)
	if !pr.filter.IsAll() {
		fmt.Fprintf(stderr, "moxy: tool-exposure filter active (--expose): %s\n", pr.filter)
	}
	p.SetNameTemplate(pr.tmpl)
	if !pr.tmpl.IsDefault() {
		fmt.Fprintf(stderr, "moxy: name template active (--name-template): %s\n", pr.tmpl)
	}
	p.SetMadderClient(blobs)
	if o.SessionID != "" {
		p.SetSessionID(o.SessionID)
	}
	p.SetMoxinReloader(&moxinReloader{in: inputs})
	p.SetResolver(perms.NewResolverFromConfigs(res.moxins))
	p.SetBootstrapper(&bootstrapper{
		in: inputs,
		// A reload serves a possibly different moxin set; the resolver the
		// batch/async preflights and an enforcing funnel consult follows it.
		onReload: func(r *bootstrapResult) {
			p.SetResolver(perms.NewResolverFromConfigs(r.moxins))
		},
	})
	if o.Notify != nil {
		p.SetNotifier(o.Notify)
	}

	if o.MetaTools || o.RegisterTools != nil {
		registry := server.NewToolRegistryV1()
		if o.RegisterTools != nil {
			o.RegisterTools(registry)
		}
		if o.MetaTools {
			registerMetaTools(registry, p, newAsyncManager(blobs, o.asyncResultStore()))
		}
		p.SetBuiltinTools(registry)
	}

	for _, c := range res.children {
		c.Client.SetOnNotification(p.ForwardNotification)
	}

	p.ProbeEphemeral(ctx)

	// A custom name template may map two distinct (server, tool) pairs to one
	// rendered name. Detect it now that the full server set is known and
	// refuse rather than silently shadow a tool; the default template never
	// collides.
	if !pr.tmpl.IsDefault() {
		if err := p.CheckNameCollisions(ctx); err != nil {
			p.CloseChildren()
			return nil, fmt.Errorf("name-template %q: %w", pr.tmpl, err)
		}
	}

	inst := &Instance{p: p, opts: o, blobs: blobs, policy: o.Permissions}
	inst.refreshInstructions(ctx)
	return inst, nil
}

// newAsyncManager builds the async job manager over the blob store's
// named-store writer when it has one (the default madder client does); a
// store without one still runs jobs to a terminal state, only the stored
// result is missing from the wake, which is how async degrades today when
// the moxy-async store is absent.
func newAsyncManager(blobs BlobStore, storeID string) *asyncjob.Manager {
	write := func(ctx context.Context, content []byte) (string, error) {
		rs, ok := blobs.(ResultStore)
		if !ok {
			return "", fmt.Errorf("blob store has no named-store writer; async result not persisted")
		}
		return rs.WriteToStore(ctx, storeID, strings.NewReader(string(content)))
	}
	return asyncjob.New(asyncjob.Options{WriteResult: write})
}

func (i *Instance) refreshInstructions(ctx context.Context) {
	summaries := i.p.CollectServerSummaries(ctx)
	instructions := proxy.FormatInstructions(summaries)
	systemPrompt, _ := proxy.FormatSystemPromptFragment(summaries)
	i.mu.Lock()
	i.instructions, i.systemPrompt = instructions, systemPrompt
	i.mu.Unlock()
}

// Instructions is the initialize-response text describing the live
// children, computed at Open.
func (i *Instance) Instructions() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.instructions
}

// SystemPromptFragment is the dynamic system-prompt contribution clown
// fetches over /clown/system-prompt (FDR 0007); ok is false when there is
// nothing to add.
func (i *Instance) SystemPromptFragment() (string, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.systemPrompt, i.systemPrompt != ""
}

// SetNotifier installs the sink for tools/list_changed and forwarded child
// notifications; a transport calls it with its write function.
func (i *Instance) SetNotifier(fn func(*jsonrpc.Message) error) { i.p.SetNotifier(fn) }

// Blobs is the blob store results reference and arguments are resolved
// against, for reading a madder://blobs/<digest> a Result names.
func (i *Instance) Blobs() BlobStore { return i.blobs }

// Policy is the serve-time permission policy this instance was opened with.
func (i *Instance) Policy() perms.Policy { return i.policy }

// Tools lists the tools a client would see: children, moxins, ephemeral
// metadata, meta tools if enabled, after the exposure filter, the exclude
// set and allow-tools.
func (i *Instance) Tools(ctx context.Context) ([]protocol.ToolV1, error) {
	if i.closed.Load() {
		return nil, ErrClosed
	}
	res, err := i.p.ListToolsV1(ctx, "")
	if err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// Call is one tool call with a caller: the rendered tool name, its JSON
// arguments, and who is asking. It runs the same funnel a tools/call over
// MCP does — exposure filter, exclude set, allow-tools, then dispatch — and
// returns the result with the two things the wire cannot carry: the
// permission decision the funnel resolved, and the blob references the
// content names.
type Call struct {
	Name   string
	Args   json.RawMessage
	Caller Caller
}

// Caller identifies who is calling and where dynamic predicates run.
type Caller struct {
	// ID is an opaque principal, recorded for diagnostics.
	ID string
	// Cwd is the working directory a dynamic-perms predicate runs in (the
	// hook passes the agent's cwd). Empty means the process's.
	Cwd string
	// Policy, when set, narrows the instance policy for this call: Delegate
	// may be tightened to Enforce; Enforce cannot be loosened.
	Policy *perms.Policy
}

// Result is a tool call's outcome.
type Result struct {
	protocol.ToolCallResultV1
	// Decision is what the funnel resolved for the call under an enforcing
	// policy; empty when the policy delegates and nothing was consulted.
	Decision perms.Decision
	// Blobs lists every madder://blobs/<digest> the content references.
	Blobs []BlobRef
}

// BlobRef is one madder://blobs/<digest> reference found in a result.
type BlobRef struct {
	URI    string
	Digest string
}

var blobURIPattern = regexp.MustCompile(`madder://blobs/([A-Za-z0-9][A-Za-z0-9_-]*)`)

// Call dispatches c through the funnel. A transport-class failure (a child
// that died, a context error) is the returned error; a tool-level failure,
// including a permission refusal, is a Result with IsError set.
func (i *Instance) Call(ctx context.Context, c Call) (*Result, error) {
	if i.closed.Load() {
		return nil, ErrClosed
	}
	if c.Caller.Policy != nil && !c.Caller.Policy.Narrower(i.policy) {
		return nil, fmt.Errorf("moxy: a per-call policy may not loosen the instance policy %q to %q",
			i.policy.Effective(), c.Caller.Policy.Effective())
	}
	res, err := i.p.CallToolV1(ctx, c.Name, c.Args)
	if err != nil {
		return nil, err
	}
	out := &Result{ToolCallResultV1: *res}
	out.Blobs = blobRefsIn(res.Content)
	return out, nil
}

func blobRefsIn(blocks []protocol.ContentBlockV1) []BlobRef {
	seen := map[string]bool{}
	var refs []BlobRef
	add := func(uri string) {
		m := blobURIPattern.FindStringSubmatch(uri)
		if m == nil || seen[m[0]] {
			return
		}
		seen[m[0]] = true
		refs = append(refs, BlobRef{URI: m[0], Digest: m[1]})
	}
	for _, b := range blocks {
		if b.URI != "" {
			add(b.URI)
		}
		if b.Resource != nil && b.Resource.URI != "" {
			add(b.Resource.URI)
		}
		for _, m := range blobURIPattern.FindAllString(b.Text, -1) {
			add(m)
		}
	}
	return refs
}

// Servers describes every child: running ones with capability counts,
// failed ones with their error.
func (i *Instance) Servers(ctx context.Context) []ServerSummary {
	if i.closed.Load() {
		return nil
	}
	return i.p.CollectServerSummaries(ctx)
}

// Restart restarts one child by name: a persistent child is respawned
// make-before-break (the old one serves until the new one answers
// tools/list), an ephemeral child is re-probed, a moxin is re-discovered. A
// failed restart leaves the old child in place.
func (i *Instance) Restart(ctx context.Context, server string) error {
	if i.closed.Load() {
		return ErrClosed
	}
	if err := i.p.RestartServer(ctx, server); err != nil {
		return err
	}
	i.refreshInstructions(ctx)
	return nil
}

// Reload re-reads the moxyfile source and the moxin path, judges the result
// against the pinned parent reference and startup baseline, and replaces
// the whole child set. A refusal leaves the running set untouched and is
// returned as a *WideningError.
func (i *Instance) Reload(ctx context.Context) error {
	if i.closed.Load() {
		return ErrClosed
	}
	if err := i.p.Reload(ctx); err != nil {
		return err
	}
	i.refreshInstructions(ctx)
	return nil
}

// Close interrupts running async jobs so each writes a terminal journal
// record while its dispatch path is still alive, then closes every child
// with the configured deadline. After Close, every method returns ErrClosed.
func (i *Instance) Close(ctx context.Context) error {
	if !i.closed.CompareAndSwap(false, true) {
		return ErrClosed
	}
	i.p.SweepAsyncJobs()
	done := make(chan struct{})
	go func() {
		i.p.CloseChildren()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(i.opts.closeTimeout()):
		fmt.Fprintf(i.opts.stderr(), "moxy: child teardown exceeded %s; continuing shutdown\n", i.opts.closeTimeout())
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- go-mcp provider interfaces, so a transport serves an Instance. ---

var (
	_ server.ToolProviderV1     = (*Instance)(nil)
	_ server.ResourceProviderV1 = (*Instance)(nil)
	_ server.PromptProviderV1   = (*Instance)(nil)
)

func (i *Instance) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	return i.p.ListTools(ctx)
}

func (i *Instance) CallTool(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResult, error) {
	return i.p.CallTool(ctx, name, args)
}

func (i *Instance) ListToolsV1(ctx context.Context, cursor string) (*protocol.ToolsListResultV1, error) {
	return i.p.ListToolsV1(ctx, cursor)
}

func (i *Instance) CallToolV1(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	return i.p.CallToolV1(ctx, name, args)
}

func (i *Instance) ListResources(ctx context.Context) ([]protocol.Resource, error) {
	return i.p.ListResources(ctx)
}

func (i *Instance) ReadResource(ctx context.Context, uri string) (*protocol.ResourceReadResult, error) {
	return i.p.ReadResource(ctx, uri)
}

func (i *Instance) ListResourceTemplates(ctx context.Context) ([]protocol.ResourceTemplate, error) {
	return i.p.ListResourceTemplates(ctx)
}

func (i *Instance) ListResourcesV1(ctx context.Context, cursor string) (*protocol.ResourcesListResultV1, error) {
	return i.p.ListResourcesV1(ctx, cursor)
}

func (i *Instance) ListResourceTemplatesV1(ctx context.Context, cursor string) (*protocol.ResourceTemplatesListResultV1, error) {
	return i.p.ListResourceTemplatesV1(ctx, cursor)
}

func (i *Instance) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	return i.p.ListPrompts(ctx)
}

func (i *Instance) GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.PromptGetResult, error) {
	return i.p.GetPrompt(ctx, name, args)
}

func (i *Instance) ListPromptsV1(ctx context.Context, cursor string) (*protocol.PromptsListResultV1, error) {
	return i.p.ListPromptsV1(ctx, cursor)
}

func (i *Instance) GetPromptV1(ctx context.Context, name string, args map[string]string) (*protocol.PromptGetResultV1, error) {
	return i.p.GetPromptV1(ctx, name, args)
}

// SetToolExclude and ToolExclude back streamhttp's /clown/exclude-tools
// (FDR 0010): the dynamic per-session deny-set.
func (i *Instance) SetToolExclude(s toolexclude.Set) { i.p.SetToolExclude(s) }

// ToolExclude reads the dynamic deny-set back.
func (i *Instance) ToolExclude() toolexclude.Set { return i.p.ToolExclude() }

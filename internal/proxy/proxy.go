package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"

	"code.linenisgreat.com/moxy/internal/asyncjob"
	"code.linenisgreat.com/moxy/internal/config"
	"code.linenisgreat.com/moxy/internal/naming"
	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/paginate"
	"code.linenisgreat.com/moxy/internal/permcheck"
	"code.linenisgreat.com/moxy/internal/statsd"
	"code.linenisgreat.com/moxy/internal/toolexclude"
	"code.linenisgreat.com/moxy/internal/toolfilter"
)

// MoxinReloader re-discovers a single moxin's config by name from the
// configured MOXIN_PATH / system moxin dir. Implementations live in
// cmd/moxy.
type MoxinReloader interface {
	ReloadMoxin(name string) (*native.NativeConfig, error)
}

// BootstrapResult is the data Bootstrapper returns: the freshly-spawned
// child set, failed-startup list, active server configs, and the global
// ephemeral default. Reload uses this to wholesale-replace the proxy's
// running state.
type BootstrapResult struct {
	Children      []ChildEntry
	Failed        []FailedServer
	ActiveServers []config.ServerConfig
	Ephemeral     *bool
}

// Bootstrapper produces a fresh BootstrapResult from the moxyfile hierarchy
// and MOXIN_PATH. Implementations live in cmd/moxy and re-spawn subprocess
// children + rebuild moxin children on every call.
type Bootstrapper interface {
	Bootstrap(ctx context.Context) (*BootstrapResult, error)
}

var debugLogger *log.Logger

func init() {
	logHome := os.Getenv("XDG_LOG_HOME")
	if logHome == "" {
		home, _ := os.UserHomeDir()
		logHome = filepath.Join(home, ".local", "log")
	}
	logDir := filepath.Join(logHome, "moxy")
	os.MkdirAll(logDir, 0o755)
	f, err := os.OpenFile(
		filepath.Join(logDir, "debug.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err == nil {
		debugLogger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	}
}

func debugLog(format string, args ...any) {
	if debugLogger != nil {
		debugLogger.Printf(format, args...)
	}
}

type ChildEntry struct {
	Client       ServerBackend
	Config       config.ServerConfig
	Capabilities protocol.ServerCapabilitiesV1
	ServerInfo   protocol.ImplementationV1
	Instructions string
}

type FailedServer struct {
	Name  string
	Error string
}

type EphemeralMeta struct {
	Config       config.ServerConfig
	Capabilities protocol.ServerCapabilitiesV1
	ServerInfo   protocol.ImplementationV1
	Instructions string
	Tools        []protocol.ToolV1
	Resources    []protocol.ResourceV1
	Templates    []protocol.ResourceTemplateV1
	Prompts      []protocol.PromptV1
}

// ConnectFunc creates and initializes a client for a given server config.
// This abstraction allows the proxy to reconnect servers without knowing
// transport details (stdio vs HTTP, credentials, etc.).
type ConnectFunc func(ctx context.Context, cfg config.ServerConfig) (ServerBackend, *protocol.InitializeResultV1, error)

type Proxy struct {
	children                    []ChildEntry
	failed                      []FailedServer
	configs                     map[string]config.ServerConfig
	ephemeral                   map[string]*EphemeralMeta
	globalEphemeral             *bool
	globalProgressiveDisclosure *bool
	connectFunc                 ConnectFunc
	madder                      native.MadderBackend
	moxyProvider                *moxyResourceProvider
	resourceProviders           []resourceProviderEntry
	builtinTools                *server.ToolRegistryV1
	asyncManager                *asyncjob.Manager
	notifier                    func(*jsonrpc.Message) error
	sessionID                   string
	moxinReloader               MoxinReloader
	bootstrapper                Bootstrapper
	resolver                    *permcheck.Resolver
	toolFilter                  toolfilter.Filter // which tool categories to expose; default All()
	toolExclude                 toolexclude.Set   // dynamic per-name/per-server deny-set; default excludes nothing
	nameTemplate                naming.Template   // how tool/prompt names are rendered; default {server}.{tool}
	toolRegistry                naming.Registry   // rendered tool name → canonical Entry; rebuilt on every ListToolsV1
	promptRegistry              naming.Registry   // rendered prompt name → canonical Entry; rebuilt on every ListPromptsV1
	toolCollision               error             // last tool-name collision under a custom template (nil if none)
	promptCollision             error             // last prompt-name collision under a custom template (nil if none)
	dispatchSubCall             subCallDispatcher // test seam; nil → use CallToolV1
	mu                          sync.RWMutex
}

type resourceProviderEntry struct {
	prefix   string
	provider ResourceProvider
}

func (p *Proxy) SetNotifier(fn func(*jsonrpc.Message) error) {
	p.notifier = fn
}

// SetMadderClient wires the madder backend into the proxy. The
// backend powers the madder://blobs/{digest} resource provider and
// is also threaded into freshly-spawned native servers (see
// ReloadMoxin).
func (p *Proxy) SetMadderClient(m native.MadderBackend) {
	p.madder = m
	p.resourceProviders = append(p.resourceProviders, resourceProviderEntry{
		prefix:   "madder://blobs/",
		provider: &madderBlobProvider{madder: m},
	})
}

func (p *Proxy) SetBuiltinTools(registry *server.ToolRegistryV1) {
	p.builtinTools = registry
}

// SetToolFilter restricts which tool categories the proxy advertises and
// dispatches. The default (New) is toolfilter.All(); serve-http --expose
// passes a narrower filter. Excluded categories are neither listed nor
// callable — see docs/features/0006-tool-exposure-filter.md.
func (p *Proxy) SetToolFilter(f toolfilter.Filter) {
	p.toolFilter = f
}

// SetToolExclude replaces the dynamic per-name/per-server deny-set and, if the
// resulting set differs from the previous one, emits
// notifications/tools/list_changed. Unlike SetToolFilter (a startup-only,
// --expose-derived category filter), this is called at runtime — from the
// POST /clown/exclude-tools handler (internal/streamhttp) — so it is
// mutex-guarded. Excluded names are neither listed nor callable — see
// docs/features/0010-tool-exclude-endpoint.md.
func (p *Proxy) SetToolExclude(s toolexclude.Set) {
	p.mu.Lock()
	changed := !sameNames(p.toolExclude.Names(), s.Names())
	p.toolExclude = s
	p.mu.Unlock()
	if changed {
		p.notifyToolsChanged()
	}
}

// ToolExclude returns the current dynamic deny-set, for the GET
// /clown/exclude-tools readback.
func (p *Proxy) ToolExclude() toolexclude.Set {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.toolExclude
}

// sameNames reports whether two name lists contain the same set of names,
// order-independent. Used to skip a spurious list_changed notification when
// SetToolExclude is called with an equivalent set.
func sameNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, n := range a {
		set[n] = true
	}
	for _, n := range b {
		if !set[n] {
			return false
		}
	}
	return true
}

// SetNameTemplate sets the template used to render child tool/prompt names. The
// default (New) is naming.DefaultTemplate() ("{server}.{tool}"), which renders
// byte-identically to the historical dot join and keeps the splitPrefix dispatch
// fast path. serve-http --name-template passes a custom template; under one,
// dispatch resolves via the rendered→canonical registries instead of parsing.
func (p *Proxy) SetNameTemplate(t naming.Template) {
	p.nameTemplate = t
}

// CheckNameCollisions enumerates the full tool and prompt surface once and
// returns the first name-template collision (a *naming.CollisionError), if any.
// serve-http calls it at startup under a custom template to fail fast rather
// than silently shadowing a tool or prompt on a public origin. The default
// template never collides, so callers skip it there.
func (p *Proxy) CheckNameCollisions(ctx context.Context) error {
	if _, err := p.ListToolsV1(ctx, ""); err != nil {
		return err
	}
	if _, err := p.ListPromptsV1(ctx, ""); err != nil {
		return err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.toolCollision != nil {
		return p.toolCollision
	}
	if p.promptCollision != nil {
		return p.promptCollision
	}
	return nil
}

func (p *Proxy) SetSessionID(id string) {
	p.sessionID = id
}

func (p *Proxy) SetMoxinReloader(r MoxinReloader) {
	p.moxinReloader = r
}

func (p *Proxy) SetBootstrapper(b Bootstrapper) {
	p.bootstrapper = b
}

// SetResolver wires the permcheck.Resolver used by the batch builtin
// (and any other consumer that needs to resolve a moxin tool's perm
// decision). Resolvers are constructed at startup in cmd/moxy and
// passed in via this setter so the proxy can be tested without doing
// a MOXIN_PATH walk.
func (p *Proxy) SetResolver(r *permcheck.Resolver) {
	p.resolver = r
}

func (p *Proxy) hasBuiltinTool(name string) bool {
	if p.builtinTools == nil {
		return false
	}
	result, _ := p.builtinTools.ListToolsV1(context.Background(), "")
	if result == nil {
		return false
	}
	for _, t := range result.Tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func (p *Proxy) ForwardNotification(msg *jsonrpc.Message) {
	if p.notifier != nil {
		p.notifier(msg)
	}
}

func (p *Proxy) notifyToolsChanged() {
	if p.notifier == nil {
		return
	}
	msg, _ := jsonrpc.NewNotification(protocol.MethodNotificationsToolsListChanged, nil)
	_ = p.notifier(msg)
}

func New(
	children []ChildEntry,
	failed []FailedServer,
	allConfigs []config.ServerConfig,
	globalEphemeral *bool,
	globalProgressiveDisclosure *bool,
	connectFunc ConnectFunc,
) *Proxy {
	configs := make(map[string]config.ServerConfig, len(allConfigs))
	ephemeral := make(map[string]*EphemeralMeta)
	for _, cfg := range allConfigs {
		configs[cfg.Name] = cfg
		if cfg.IsEphemeral(globalEphemeral) {
			ephemeral[cfg.Name] = &EphemeralMeta{Config: cfg}
		}
	}
	p := &Proxy{
		children:                    children,
		failed:                      failed,
		configs:                     configs,
		ephemeral:                   ephemeral,
		globalEphemeral:             globalEphemeral,
		globalProgressiveDisclosure: globalProgressiveDisclosure,
		connectFunc:                 connectFunc,
		toolFilter:                  toolfilter.All(),
		nameTemplate:                naming.DefaultTemplate(),
	}
	moxy := &moxyResourceProvider{proxy: p}
	p.moxyProvider = moxy
	p.resourceProviders = []resourceProviderEntry{
		{prefix: "moxy://", provider: moxy},
	}
	return p
}

func (p *Proxy) ProbeEphemeral(ctx context.Context) {
	for name, meta := range p.ephemeral {
		cfg := meta.Config
		client, result, err := p.connectFunc(ctx, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "moxy: failed to probe ephemeral %s: %v\n", name, err)
			p.markFailed(name, err)
			continue
		}

		meta.Capabilities = result.Capabilities
		meta.ServerInfo = result.ServerInfo
		meta.Instructions = result.Instructions

		if result.Capabilities.Tools != nil {
			raw, err := client.Call(ctx, protocol.MethodToolsList, nil)
			if err == nil {
				if tools, err := decodeToolsList(raw); err == nil {
					meta.Tools = tools
				}
			}
		}

		if result.Capabilities.Resources != nil {
			raw, err := client.Call(ctx, protocol.MethodResourcesList, nil)
			if err == nil {
				if resources, err := decodeResourcesList(raw); err == nil {
					meta.Resources = resources
				}
			}
			raw, err = client.Call(ctx, protocol.MethodResourcesTemplates, nil)
			if err == nil {
				if templates, err := decodeResourceTemplatesList(raw); err == nil {
					meta.Templates = templates
				}
			}
		}

		if result.Capabilities.Prompts != nil {
			raw, err := client.Call(ctx, protocol.MethodPromptsList, nil)
			if err == nil {
				if prompts, err := decodePromptsList(raw); err == nil {
					meta.Prompts = prompts
				}
			}
		}

		client.Close()
		fmt.Fprintf(os.Stderr, "moxy: probed ephemeral %s (%s %s)\n",
			name, result.ServerInfo.Name, result.ServerInfo.Version)
	}
}

func (p *Proxy) reprobeEphemeral(ctx context.Context, meta *EphemeralMeta) error {
	cfg := meta.Config
	client, result, err := p.connectFunc(ctx, cfg)
	if err != nil {
		return fmt.Errorf("re-probing ephemeral %s: %w", cfg.Name, err)
	}
	defer client.Close()

	meta.Capabilities = result.Capabilities
	meta.ServerInfo = result.ServerInfo
	meta.Instructions = result.Instructions
	meta.Tools = nil
	meta.Resources = nil
	meta.Templates = nil
	meta.Prompts = nil

	if result.Capabilities.Tools != nil {
		raw, err := client.Call(ctx, protocol.MethodToolsList, nil)
		if err == nil {
			if tools, err := decodeToolsList(raw); err == nil {
				meta.Tools = tools
			}
		}
	}

	if result.Capabilities.Resources != nil {
		raw, err := client.Call(ctx, protocol.MethodResourcesList, nil)
		if err == nil {
			if resources, err := decodeResourcesList(raw); err == nil {
				meta.Resources = resources
			}
		}
		raw, err = client.Call(ctx, protocol.MethodResourcesTemplates, nil)
		if err == nil {
			if templates, err := decodeResourceTemplatesList(raw); err == nil {
				meta.Templates = templates
			}
		}
	}

	if result.Capabilities.Prompts != nil {
		raw, err := client.Call(ctx, protocol.MethodPromptsList, nil)
		if err == nil {
			if prompts, err := decodePromptsList(raw); err == nil {
				meta.Prompts = prompts
			}
		}
	}

	p.notifyToolsChanged()

	return nil
}

func (p *Proxy) spawnEphemeral(ctx context.Context, serverName string) (ServerBackend, error) {
	debugLog("spawnEphemeral %s", serverName)
	cfg, ok := p.configs[serverName]
	if !ok {
		return nil, fmt.Errorf("unknown server %q", serverName)
	}
	client, _, err := p.connectFunc(ctx, cfg)
	if err != nil {
		debugLog("spawnEphemeral FAIL %s: %v", serverName, err)
		return nil, fmt.Errorf("spawning ephemeral %s: %w", serverName, err)
	}
	client.SetOnNotification(func(msg *jsonrpc.Message) {
		p.ForwardNotification(msg)
	})
	return client, nil
}

func (p *Proxy) markFailed(name string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range p.failed {
		if f.Name == name {
			return
		}
	}
	p.failed = append(p.failed, FailedServer{
		Name:  name,
		Error: err.Error(),
	})
}

func (p *Proxy) CollectServerSummaries(ctx context.Context) []ServerSummary {
	p.mu.RLock()
	children := p.children
	failed := p.failed
	p.mu.RUnlock()

	var summaries []ServerSummary

	for _, child := range children {
		s := ServerSummary{
			Name:         child.Config.Name,
			Version:      child.ServerInfo.Version,
			Instructions: child.Instructions,
			Status:       "running",
		}

		if child.Capabilities.Tools != nil {
			raw, err := child.Client.Call(ctx, protocol.MethodToolsList, nil)
			if err == nil {
				if tools, err := decodeToolsList(raw); err == nil {
					s.Tools = len(tools)
				}
			}
		}

		if child.Capabilities.Resources != nil {
			raw, err := child.Client.Call(ctx, protocol.MethodResourcesList, nil)
			if err == nil {
				if resources, err := decodeResourcesList(raw); err == nil {
					s.Resources = len(resources)
				}
			}
			raw, err = child.Client.Call(ctx, protocol.MethodResourcesTemplates, nil)
			if err == nil {
				if templates, err := decodeResourceTemplatesList(raw); err == nil {
					s.ResourceTemplates = len(templates)
				}
			}
		}

		if child.Capabilities.Prompts != nil {
			raw, err := child.Client.Call(ctx, protocol.MethodPromptsList, nil)
			if err == nil {
				if prompts, err := decodePromptsList(raw); err == nil {
					s.Prompts = len(prompts)
				}
			}
		}

		summaries = append(summaries, s)
	}

	for name, meta := range p.ephemeral {
		summaries = append(summaries, ServerSummary{
			Name:              name,
			Version:           meta.ServerInfo.Version,
			Instructions:      meta.Instructions,
			Status:            "running",
			Tools:             len(meta.Tools),
			Resources:         len(meta.Resources),
			ResourceTemplates: len(meta.Templates),
			Prompts:           len(meta.Prompts),
		})
	}

	for _, f := range failed {
		summaries = append(summaries, ServerSummary{
			Name:   f.Name,
			Status: "failed",
			Error:  f.Error,
		})
	}

	return summaries
}

// --- ToolProvider (V0) ---

func (p *Proxy) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	v1, err := p.ListToolsV1(ctx, "")
	if err != nil {
		return nil, err
	}
	tools := make([]protocol.Tool, len(v1.Tools))
	for i, t := range v1.Tools {
		tools[i] = protocol.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return tools, nil
}

func (p *Proxy) CallTool(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (*protocol.ToolCallResult, error) {
	debugLog("CallTool V0 path hit for tool %q", name)
	v1, err := p.CallToolV1(ctx, name, args)
	if err != nil {
		return nil, err
	}
	for _, b := range v1.Content {
		debugLog("  content block: type=%q mimeType=%q resource=%v", b.Type, b.MimeType, b.Resource != nil)
	}
	return &protocol.ToolCallResult{
		Content: downgradeContentBlocks(v1.Content),
		IsError: v1.IsError,
	}, nil
}

// --- ToolProviderV1 ---

func (p *Proxy) ListToolsV1(
	ctx context.Context,
	cursor string,
) (*protocol.ToolsListResultV1, error) {
	p.mu.RLock()
	children := p.children
	failed := p.failed
	p.mu.RUnlock()

	allTools := make([]protocol.ToolV1, 0)

	// Render each advertised name through the configured template and, in the
	// same pass, record its canonical (server, original, category) so dispatch
	// can reverse-resolve a custom template without re-parsing the rendered
	// string. seen drops any duplicate rendered name (impossible under the
	// default template; possible under a name-dropping custom template — the
	// later entry is dropped, first-wins, matching the registry).
	tb := naming.NewBuilder(p.nameTemplate)
	seen := make(map[string]bool)
	addTool := func(server, original string, cat naming.Category, tool protocol.ToolV1) {
		rendered := p.nameTemplate.Render(server, original)
		tb.Add(naming.Entry{Server: server, Original: original, Kind: naming.KindTool, Category: cat})
		if seen[rendered] {
			debugLog("ListToolsV1: dropping duplicate rendered tool name %q (server %q, tool %q)", rendered, server, original)
			return
		}
		seen[rendered] = true
		tool.Name = rendered
		allTools = append(allTools, tool)
	}

	debugLog("ListToolsV1: %d children, %d failed", len(children), len(failed))
	for _, child := range children {
		debugLog("ListToolsV1: child %q caps.Tools=%v progressive=%v",
			child.Client.Name(),
			child.Capabilities.Tools != nil,
			child.Config.IsProgressiveDisclosure(p.globalProgressiveDisclosure))

		if child.Capabilities.Tools == nil {
			debugLog("ListToolsV1: SKIP %q — Capabilities.Tools is nil", child.Client.Name())
			continue
		}
		if child.Config.IsProgressiveDisclosure(p.globalProgressiveDisclosure) {
			debugLog("ListToolsV1: SKIP %q — progressive disclosure", child.Client.Name())
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodToolsList,
			cursorParams(cursor),
		)
		if err != nil {
			debugLog("ListToolsV1: ERROR listing tools for %q: %v", child.Client.Name(), err)
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing tools: %w", err),
			)
			continue
		}

		tools, err := decodeToolsList(raw)
		if err != nil {
			debugLog("ListToolsV1: ERROR decoding tools for %q: %v", child.Client.Name(), err)
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding tools: %w", err),
			)
			continue
		}

		debugLog("ListToolsV1: %q returned %d tools", child.Client.Name(), len(tools))
		for _, tool := range tools {
			if !matchesAnnotationFilter(
				tool.Annotations,
				child.Config.Annotations,
			) {
				debugLog("ListToolsV1: FILTERED %q.%q by annotation", child.Client.Name(), tool.Name)
				continue
			}
			original := tool.Name
			prefixToolTitle(&tool, child.Client.Name())
			addTool(child.Client.Name(), original, naming.CategoryChild, tool)
		}
	}

	// Inject synthetic resource tools for resource-capable children. A child
	// that already advertises a tool rendering to the same name (seen) wins,
	// so the synthetic is skipped — the same collision check as before, now
	// keyed by rendered name.
	for _, child := range children {
		if child.Capabilities.Resources == nil {
			continue
		}
		if child.Config.GenerateResourceTools != nil && !*child.Config.GenerateResourceTools {
			continue
		}
		if child.Config.IsProgressiveDisclosure(p.globalProgressiveDisclosure) {
			continue
		}

		serverName := child.Client.Name()
		p.addSyntheticResourceTools(serverName, seen, addTool)
	}

	// Append cached tools from ephemeral servers
	for serverName, meta := range p.ephemeral {
		if !meta.Config.IsProgressiveDisclosure(p.globalProgressiveDisclosure) {
			for _, tool := range meta.Tools {
				if !matchesAnnotationFilter(tool.Annotations, meta.Config.Annotations) {
					continue
				}
				original := tool.Name
				prefixToolTitle(&tool, serverName)
				addTool(serverName, original, naming.CategoryChild, tool)
			}
			if meta.Capabilities.Resources != nil {
				grt := meta.Config.GenerateResourceTools
				if grt == nil || *grt {
					p.addSyntheticResourceTools(serverName, seen, addTool)
				}
			}
		}
	}

	for _, f := range failed {
		addTool(f.Name, "status", naming.CategoryChild, protocol.ToolV1{
			Title: f.Name + ": Server Status",
			Description: fmt.Sprintf(
				"Server %q failed to start: %s",
				f.Name,
				f.Error,
			),
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Annotations: readOnlyAnnotations(),
		})
	}

	// Builtins (restart, batch, async*, status) bypass the template entirely —
	// they have no server prefix, keep their literal names, and never enter the
	// registry (the meta-tool bypass).
	if p.builtinTools != nil {
		builtinResult, _ := p.builtinTools.ListToolsV1(ctx, "")
		if builtinResult != nil {
			allTools = append(allTools, builtinResult.Tools...)
		}
	}

	reg, collErr := tb.Build()
	p.mu.Lock()
	p.toolRegistry = reg
	p.toolCollision = collErr
	p.mu.Unlock()
	if collErr != nil {
		debugLog("ListToolsV1: name-template collision: %v", collErr)
		fmt.Fprintf(os.Stderr, "moxy: name-template tool collision (later tool dropped): %v\n", collErr)
	}

	allTools = p.applyToolFilter(allTools, reg)
	allTools = p.applyToolExclude(allTools, reg)

	debugLog("ListToolsV1: returning %d total tools", len(allTools))
	return &protocol.ToolsListResultV1{Tools: allTools}, nil
}

// addSyntheticResourceTools appends the resource-read / resource-templates bridge
// tools for a resource-capable server via addTool (so they land in both the list
// and the registry, classified ResourceBridge). addTool's seen check skips either
// one whose rendered name a real child tool already claimed.
func (p *Proxy) addSyntheticResourceTools(
	serverName string,
	seen map[string]bool,
	addTool func(server, original string, cat naming.Category, tool protocol.ToolV1),
) {
	if !seen[p.nameTemplate.Render(serverName, "resource-read")] {
		addTool(serverName, "resource-read", naming.CategoryResourceBridge, protocol.ToolV1{
			Title:       serverName + ": Read Resource",
			Description: fmt.Sprintf("Read a resource from %s by URI", serverName),
			InputSchema: json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI"}},"required":["uri"]}`),
			Annotations: readOnlyAnnotations(),
		})
	}
	if !seen[p.nameTemplate.Render(serverName, "resource-templates")] {
		addTool(serverName, "resource-templates", naming.CategoryResourceBridge, protocol.ToolV1{
			Title:       serverName + ": List Resource Templates",
			Description: fmt.Sprintf("List available resource templates for %s", serverName),
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Annotations: readOnlyAnnotations(),
		})
	}
}

// applyToolFilter drops tools whose category the configured --expose filter
// excludes. Under the default template it classifies by name
// (toolfilter.Categorize) exactly as before — preserving every existing
// deployment's behaviour, including the documented edge where a child's own tool
// named resource-read is treated as a resource bridge. Under a custom template
// the rendered name no longer reveals its category, so the category comes from
// the just-built registry instead. CallToolV1's dispatch gate (toolCategory)
// uses the identical source, so the advertised and callable surfaces never
// disagree. The default All() filter short-circuits to a no-op.
func (p *Proxy) applyToolFilter(tools []protocol.ToolV1, reg naming.Registry) []protocol.ToolV1 {
	if p.toolFilter.IsAll() {
		return tools
	}
	isDefault := p.nameTemplate.IsDefault()
	kept := tools[:0]
	for _, t := range tools {
		var cat toolfilter.Category
		if isDefault {
			cat = toolfilter.Categorize(t.Name)
		} else {
			cat = categoryFromRegistry(t.Name, reg)
		}
		if p.toolFilter.Allows(cat) {
			kept = append(kept, t)
		}
	}
	return kept
}

// applyToolExclude drops tools whose rendered name or owning server is in the
// dynamic exclude set (set via POST /clown/exclude-tools). Resolves each
// tool's owning server the same way applyToolFilter resolves category: by
// name under the default template, by registry lookup under a custom one.
// CallToolV1's dispatch gate (excludeServerFor) uses the identical source, so
// the advertised and callable surfaces never disagree. An empty exclude set
// short-circuits to a no-op.
func (p *Proxy) applyToolExclude(tools []protocol.ToolV1, reg naming.Registry) []protocol.ToolV1 {
	p.mu.RLock()
	exclude := p.toolExclude
	p.mu.RUnlock()
	if exclude.IsEmpty() {
		return tools
	}
	isDefault := p.nameTemplate.IsDefault()
	kept := tools[:0]
	for _, t := range tools {
		var srv string
		if isDefault {
			srv, _, _ = splitPrefix(t.Name, ".")
		} else {
			srv = serverFromRegistry(t.Name, reg)
		}
		if !exclude.Excludes(srv, t.Name) {
			kept = append(kept, t)
		}
	}
	return kept
}

// serverFromRegistry resolves a rendered tool name to its owning server name.
// A registered name carries its server from build time; an unregistered name
// (a builtin, which bypasses the template) has no owning server.
func serverFromRegistry(name string, reg naming.Registry) string {
	if e, ok := reg.Lookup(name); ok {
		return e.Server
	}
	return ""
}

// categoryFromRegistry resolves a rendered tool name to its --expose category.
// A registered name carries its category from build time; anything absent (the
// builtins, which bypass the template) is classified by toolfilter.Categorize,
// which maps a prefixless name to Meta.
func categoryFromRegistry(name string, reg naming.Registry) toolfilter.Category {
	if e, ok := reg.Lookup(name); ok {
		return mapCategory(e.Category)
	}
	return toolfilter.Categorize(name)
}

// mapCategory translates a naming.Category to its toolfilter.Category twin. The
// two enums are kept separate to avoid an import cycle; this is the single
// translation site.
func mapCategory(c naming.Category) toolfilter.Category {
	switch c {
	case naming.CategoryResourceBridge:
		return toolfilter.ResourceBridge
	case naming.CategoryMeta:
		return toolfilter.Meta
	default:
		return toolfilter.Child
	}
}

// resolveToolName maps an advertised (rendered) tool name back to its canonical
// (server, original). Under the default template it keeps the historical
// splitPrefix fast path (zero behaviour change for every existing deployment);
// under a custom template it consults the cached registry, refreshing it once on
// a miss in case a client dispatches before ever listing.
func (p *Proxy) resolveToolName(ctx context.Context, name string) (server, original string, ok bool) {
	if p.nameTemplate.IsDefault() {
		return splitPrefix(name, ".")
	}
	p.mu.RLock()
	e, found := p.toolRegistry.Lookup(name)
	p.mu.RUnlock()
	if !found {
		_, _ = p.ListToolsV1(ctx, "") // refresh the registry as a side effect
		p.mu.RLock()
		e, found = p.toolRegistry.Lookup(name)
		p.mu.RUnlock()
	}
	if !found {
		return "", "", false
	}
	return e.Server, e.Original, true
}

// toolCategory resolves a rendered tool name to its --expose category for the
// dispatch gate, using the same source as the list filter.
func (p *Proxy) toolCategory(name string) toolfilter.Category {
	if p.nameTemplate.IsDefault() {
		return toolfilter.Categorize(name)
	}
	p.mu.RLock()
	reg := p.toolRegistry
	p.mu.RUnlock()
	return categoryFromRegistry(name, reg)
}

// excludeServerFor resolves a rendered tool name to its owning server for the
// dynamic-exclude dispatch gate, using the same source as the list filter
// (applyToolExclude).
func (p *Proxy) excludeServerFor(name string) string {
	if p.nameTemplate.IsDefault() {
		srv, _, _ := splitPrefix(name, ".")
		return srv
	}
	p.mu.RLock()
	reg := p.toolRegistry
	p.mu.RUnlock()
	return serverFromRegistry(name, reg)
}

// CallToolV1 dispatches a tool call and emits fire-and-forget statsd
// metrics (duration + success/failure/abandoned) for every dispatch.
// All tool-call paths funnel through here: builtins, batch sub-calls,
// native moxins, persistent children, and ephemeral children — so this
// wrapper is the single instrumentation point.
func (p *Proxy) CallToolV1(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	start := time.Now()
	result, err := p.callToolV1(ctx, name, args)
	serverName, toolName := p.metricSegments(name)
	statsd.EmitToolDispatch(
		serverName, toolName,
		time.Since(start),
		dispatchOutcome(ctx, result, err),
	)
	return result, err
}

// metricSegments splits a rendered tool name into the (server, tool) statsd
// segments. Under the default template this is the historical first-dot split;
// under a custom template it reads the registry (already populated by the
// dispatch that precedes this call). A prefixless / unregistered name (a
// builtin) reports "builtin".
func (p *Proxy) metricSegments(name string) (server, tool string) {
	if p.nameTemplate.IsDefault() {
		if s, t, ok := splitPrefix(name, "."); ok {
			return s, t
		}
		return "builtin", name
	}
	p.mu.RLock()
	e, ok := p.toolRegistry.Lookup(name)
	p.mu.RUnlock()
	if ok {
		return e.Server, e.Original
	}
	return "builtin", name
}

// dispatchOutcome classifies one dispatch for metrics — see
// statsd.OutcomeFor for the shared classification rules.
func dispatchOutcome(
	ctx context.Context,
	result *protocol.ToolCallResultV1,
	err error,
) statsd.Outcome {
	return statsd.OutcomeFor(ctx.Err(), err, result != nil && result.IsError)
}

func (p *Proxy) callToolV1(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	debugLog("CallToolV1 path hit for tool %q", name)

	// Enforce the --expose filter at the single dispatch funnel: a tool whose
	// category is excluded is not callable, even by a client that already
	// knows its name. This is what makes `--expose no-meta` / `resources-only`
	// a real boundary on a public origin rather than a cosmetic list filter.
	if !p.toolFilter.Allows(p.toolCategory(name)) {
		return protocol.ErrorResultV1(
			fmt.Sprintf("tool %q is not exposed by this server", name),
		), nil
	}

	// Enforce the dynamic exclude set at the same funnel: a tool excluded via
	// POST /clown/exclude-tools is not callable, even by a client that already
	// knows its name — mirrors the --expose gate above (FDR 0010).
	p.mu.RLock()
	exclude := p.toolExclude
	p.mu.RUnlock()
	if !exclude.IsEmpty() && exclude.Excludes(p.excludeServerFor(name), name) {
		return protocol.ErrorResultV1(
			fmt.Sprintf("tool %q is excluded for this session", name),
		), nil
	}

	if p.builtinTools != nil && p.hasBuiltinTool(name) {
		return p.builtinTools.CallToolV1(ctx, name, args)
	}

	p.mu.RLock()
	children := p.children
	failed := p.failed
	p.mu.RUnlock()

	serverName, toolName, ok := p.resolveToolName(ctx, name)
	if !ok {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid tool name %q: missing server prefix", name),
		), nil
	}

	if toolName == "status" {
		for _, f := range failed {
			if f.Name == serverName {
				return protocol.ErrorResultV1(
					fmt.Sprintf(
						"server %q failed to start: %s",
						f.Name,
						f.Error,
					),
				), nil
			}
		}
	}

	child, ok := findChildIn(children, serverName)
	if !ok {
		// Check if this is an ephemeral server
		if _, isEphemeral := p.ephemeral[serverName]; isEphemeral {
			return p.callToolEphemeral(ctx, serverName, toolName, args)
		}
		return protocol.ErrorResultV1(
			fmt.Sprintf("unknown server %q", serverName),
		), nil
	}

	if toolName == "resource-read" {
		return p.callResourceRead(ctx, child, args)
	}

	if toolName == "resource-templates" {
		return p.callResourceTemplates(ctx, child)
	}

	params := protocol.ToolCallParams{
		Name:      toolName,
		Arguments: args,
	}

	raw, err := child.Client.Call(ctx, protocol.MethodToolsCall, params)
	if err != nil {
		return nil, fmt.Errorf(
			"calling tool %s on %s: %w",
			toolName,
			serverName,
			err,
		)
	}

	result, err := decodeToolCallResult(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding tool call result from %s: %w",
			serverName,
			err,
		)
	}

	return result, nil
}

// --- ResourceProvider (V0) ---

func (p *Proxy) ListResources(
	ctx context.Context,
) ([]protocol.Resource, error) {
	v1, err := p.ListResourcesV1(ctx, "")
	if err != nil {
		return nil, err
	}
	resources := make([]protocol.Resource, len(v1.Resources))
	for i, r := range v1.Resources {
		resources[i] = protocol.Resource{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MimeType,
		}
	}
	return resources, nil
}

// ReadResource reads a resource and emits fire-and-forget statsd metrics
// (moxy.<segment>.resource_read.* — see resourceMetricSegment) for every
// read, mirroring the CallToolV1 dispatch wrapper (#312).
func (p *Proxy) ReadResource(
	ctx context.Context,
	uri string,
) (*protocol.ResourceReadResult, error) {
	start := time.Now()
	result, err := p.readResource(ctx, uri)
	statsd.EmitToolDispatch(
		resourceMetricSegment(uri), "resource_read",
		time.Since(start),
		statsd.OutcomeFor(ctx.Err(), err, false),
	)
	return result, err
}

// resourceMetricSegment derives the <server> metric segment from a
// resource URI: the scheme for scheme-shaped URIs (moxy://servers,
// madder://blobs/...), else the <server>/ prefix, else "_". Sanitization
// happens at the emit layer.
func resourceMetricSegment(uri string) string {
	if i := strings.Index(uri, "://"); i > 0 {
		return uri[:i]
	}
	if server, _, ok := splitPrefix(uri, "/"); ok {
		return server
	}
	return "_"
}

func (p *Proxy) readResource(
	ctx context.Context,
	uri string,
) (*protocol.ResourceReadResult, error) {
	for _, entry := range p.resourceProviders {
		if strings.HasPrefix(uri, entry.prefix) {
			return entry.provider.ReadResource(ctx, uri)
		}
	}

	serverName, originalURI, ok := splitPrefix(uri, "/")
	if !ok {
		return nil, fmt.Errorf(
			"invalid resource URI %q: missing server prefix",
			uri,
		)
	}

	child, ok := p.findChild(serverName)
	if !ok {
		if _, isEphemeral := p.ephemeral[serverName]; isEphemeral {
			return p.readResourceEphemeral(ctx, serverName, originalURI)
		}
		return p.moxyProvider.fallbackUnknownServer(uri, serverName)
	}

	if child.Capabilities.Resources == nil {
		return p.moxyProvider.fallbackNoResources(uri, serverName)
	}

	// Parse and strip pagination params if server has paginate enabled
	var pgParams paginate.Params
	if child.Config.Paginate {
		originalURI, pgParams = paginate.ParseParams(originalURI)
	}

	params := protocol.ResourceReadParams{URI: originalURI}

	raw, err := child.Client.Call(ctx, protocol.MethodResourcesRead, params)
	if err != nil {
		return nil, fmt.Errorf(
			"reading resource %s from %s: %w",
			originalURI,
			serverName,
			err,
		)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf(
			"decoding resource read result from %s: %w",
			serverName,
			err,
		)
	}

	if pgParams.Active {
		result = paginateResourceResult(result, pgParams)
	}

	return &result, nil
}

func (p *Proxy) ListResourceTemplates(
	ctx context.Context,
) ([]protocol.ResourceTemplate, error) {
	v1, err := p.ListResourceTemplatesV1(ctx, "")
	if err != nil {
		return nil, err
	}
	templates := make([]protocol.ResourceTemplate, len(v1.ResourceTemplates))
	for i, t := range v1.ResourceTemplates {
		templates[i] = protocol.ResourceTemplate{
			URITemplate: t.URITemplate,
			Name:        t.Name,
			Description: t.Description,
			MimeType:    t.MimeType,
		}
	}
	return templates, nil
}

// --- ResourceProviderV1 ---

func (p *Proxy) ListResourcesV1(
	ctx context.Context,
	cursor string,
) (*protocol.ResourcesListResultV1, error) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	allResources := make([]protocol.ResourceV1, 0)

	for _, child := range children {
		if child.Capabilities.Resources == nil {
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodResourcesList,
			cursorParams(cursor),
		)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing resources: %w", err),
			)
			continue
		}

		resources, err := decodeResourcesList(raw)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding resources: %w", err),
			)
			continue
		}

		for _, r := range resources {
			r.URI = child.Client.Name() + "/" + r.URI
			allResources = append(allResources, r)
		}
	}

	for serverName, meta := range p.ephemeral {
		for _, r := range meta.Resources {
			r.URI = serverName + "/" + r.URI
			allResources = append(allResources, r)
		}
	}

	// Synthetic resource providers (moxy://, madder://blobs/, etc.)
	for _, entry := range p.resourceProviders {
		allResources = append(allResources, entry.provider.ListResources(ctx)...)
	}

	return &protocol.ResourcesListResultV1{Resources: allResources}, nil
}

func (p *Proxy) ListResourceTemplatesV1(
	ctx context.Context,
	cursor string,
) (*protocol.ResourceTemplatesListResultV1, error) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	allTemplates := make([]protocol.ResourceTemplateV1, 0)

	for _, child := range children {
		if child.Capabilities.Resources == nil {
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodResourcesTemplates,
			cursorParams(cursor),
		)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing resource templates: %w", err),
			)
			continue
		}

		templates, err := decodeResourceTemplatesList(raw)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding resource templates: %w", err),
			)
			continue
		}

		for _, t := range templates {
			t.URITemplate = child.Client.Name() + "/" + t.URITemplate
			allTemplates = append(allTemplates, t)
		}
	}

	for serverName, meta := range p.ephemeral {
		for _, t := range meta.Templates {
			t.URITemplate = serverName + "/" + t.URITemplate
			allTemplates = append(allTemplates, t)
		}
	}

	// Synthetic resource providers (moxy://, madder://blobs/, etc.)
	for _, entry := range p.resourceProviders {
		allTemplates = append(allTemplates, entry.provider.ListResourceTemplates(ctx)...)
	}

	return &protocol.ResourceTemplatesListResultV1{
		ResourceTemplates: allTemplates,
	}, nil
}

// --- PromptProvider (V0) ---

func (p *Proxy) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	v1, err := p.ListPromptsV1(ctx, "")
	if err != nil {
		return nil, err
	}
	prompts := make([]protocol.Prompt, len(v1.Prompts))
	for i, pr := range v1.Prompts {
		prompts[i] = protocol.Prompt{
			Name:        pr.Name,
			Description: pr.Description,
			Arguments:   pr.Arguments,
		}
	}
	return prompts, nil
}

func (p *Proxy) GetPrompt(
	ctx context.Context,
	name string,
	args map[string]string,
) (*protocol.PromptGetResult, error) {
	v1, err := p.GetPromptV1(ctx, name, args)
	if err != nil {
		return nil, err
	}
	messages := make([]protocol.PromptMessage, len(v1.Messages))
	for i, m := range v1.Messages {
		messages[i] = protocol.PromptMessage{
			Role: m.Role,
			Content: protocol.ContentBlock{
				Type:     m.Content.Type,
				Text:     m.Content.Text,
				MimeType: m.Content.MimeType,
				Data:     m.Content.Data,
			},
		}
	}
	return &protocol.PromptGetResult{
		Description: v1.Description,
		Messages:    messages,
	}, nil
}

// --- PromptProviderV1 ---

func (p *Proxy) ListPromptsV1(
	ctx context.Context,
	cursor string,
) (*protocol.PromptsListResultV1, error) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	allPrompts := make([]protocol.PromptV1, 0)

	// Prompts get an independent registry (a tool and a prompt may share a
	// rendered name — they are dispatched by different MCP methods), built the
	// same way as the tool registry: render forward, record canonical identity
	// for reverse dispatch, drop duplicate rendered names first-wins.
	pb := naming.NewBuilder(p.nameTemplate)
	seen := make(map[string]bool)
	addPrompt := func(server, original string, pr protocol.PromptV1) {
		rendered := p.nameTemplate.Render(server, original)
		pb.Add(naming.Entry{Server: server, Original: original, Kind: naming.KindPrompt, Category: naming.CategoryChild})
		if seen[rendered] {
			debugLog("ListPromptsV1: dropping duplicate rendered prompt name %q (server %q, prompt %q)", rendered, server, original)
			return
		}
		seen[rendered] = true
		pr.Name = rendered
		allPrompts = append(allPrompts, pr)
	}

	for _, child := range children {
		if child.Capabilities.Prompts == nil {
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodPromptsList,
			cursorParams(cursor),
		)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing prompts: %w", err),
			)
			continue
		}

		prompts, err := decodePromptsList(raw)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding prompts: %w", err),
			)
			continue
		}

		for _, pr := range prompts {
			addPrompt(child.Client.Name(), pr.Name, pr)
		}
	}

	for serverName, meta := range p.ephemeral {
		for _, pr := range meta.Prompts {
			addPrompt(serverName, pr.Name, pr)
		}
	}

	reg, collErr := pb.Build()
	p.mu.Lock()
	p.promptRegistry = reg
	p.promptCollision = collErr
	p.mu.Unlock()
	if collErr != nil {
		debugLog("ListPromptsV1: name-template collision: %v", collErr)
		fmt.Fprintf(os.Stderr, "moxy: name-template prompt collision (later prompt dropped): %v\n", collErr)
	}

	return &protocol.PromptsListResultV1{Prompts: allPrompts}, nil
}

// GetPromptV1 resolves a prompt and emits fire-and-forget statsd metrics
// (moxy.<server>.prompt_get.*), mirroring the CallToolV1 dispatch
// wrapper (#312).
func (p *Proxy) GetPromptV1(
	ctx context.Context,
	name string,
	args map[string]string,
) (*protocol.PromptGetResultV1, error) {
	start := time.Now()
	result, err := p.getPromptV1(ctx, name, args)
	segment, _, ok := p.resolvePromptName(ctx, name)
	if !ok {
		segment = "_"
	}
	statsd.EmitToolDispatch(
		segment, "prompt_get",
		time.Since(start),
		statsd.OutcomeFor(ctx.Err(), err, false),
	)
	return result, err
}

// resolvePromptName maps an advertised (rendered) prompt name back to its
// canonical (server, original), mirroring resolveToolName: splitPrefix under the
// default template, the prompt registry (with a one-shot refresh on a miss)
// under a custom one.
func (p *Proxy) resolvePromptName(ctx context.Context, name string) (server, original string, ok bool) {
	if p.nameTemplate.IsDefault() {
		return splitPrefix(name, ".")
	}
	p.mu.RLock()
	e, found := p.promptRegistry.Lookup(name)
	p.mu.RUnlock()
	if !found {
		_, _ = p.ListPromptsV1(ctx, "") // refresh the registry as a side effect
		p.mu.RLock()
		e, found = p.promptRegistry.Lookup(name)
		p.mu.RUnlock()
	}
	if !found {
		return "", "", false
	}
	return e.Server, e.Original, true
}

func (p *Proxy) getPromptV1(
	ctx context.Context,
	name string,
	args map[string]string,
) (*protocol.PromptGetResultV1, error) {
	serverName, promptName, ok := p.resolvePromptName(ctx, name)
	if !ok {
		return nil, fmt.Errorf(
			"invalid prompt name %q: missing server prefix",
			name,
		)
	}

	child, ok := p.findChild(serverName)
	if !ok {
		if _, isEphemeral := p.ephemeral[serverName]; isEphemeral {
			return p.getPromptEphemeral(ctx, serverName, promptName, args)
		}
		return nil, fmt.Errorf("unknown server %q", serverName)
	}

	params := protocol.PromptGetParams{
		Name:      promptName,
		Arguments: args,
	}

	raw, err := child.Client.Call(ctx, protocol.MethodPromptsGet, params)
	if err != nil {
		return nil, fmt.Errorf(
			"getting prompt %s from %s: %w",
			promptName,
			serverName,
			err,
		)
	}

	result, err := decodePromptGetResult(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding prompt get result from %s: %w",
			serverName,
			err,
		)
	}

	return result, nil
}

// --- helpers ---

// HandleRestart is the dispatch entrypoint for the `restart` builtin tool.
// Exported so cmd/moxy can register it via the builtin tool registry.
//
// With params.Server set: per-server restart (subprocess re-spawn or moxin
// re-discovery). With params.Server empty / omitted: full reload — re-read
// the moxyfile hierarchy and MOXIN_PATH, replace every running child.
func (p *Proxy) HandleRestart(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params struct {
		Server string `json:"server"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.ErrorResultV1(
				fmt.Sprintf("invalid restart args: %v", err),
			), nil
		}
	}
	if params.Server == "" {
		if err := p.Reload(ctx); err != nil {
			return protocol.ErrorResultV1(
				fmt.Sprintf("reload failed: %v", err),
			), nil
		}
		return &protocol.ToolCallResultV1{
			Content: []protocol.ContentBlockV1{
				{Type: "text", Text: "Reloaded moxyfile hierarchy and MOXIN_PATH."},
			},
		}, nil
	}
	if err := p.restartServer(ctx, params.Server); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("restart failed: %v", err),
		), nil
	}
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: fmt.Sprintf("Server %q restarted successfully", params.Server)},
		},
	}, nil
}

// Reload re-reads the moxyfile hierarchy and MOXIN_PATH via the configured
// Bootstrapper, then wholesale-replaces the running child set, configs,
// ephemeral metadata, and failed list. Closes every existing child before
// installing the new ones. Re-probes ephemeral and emits one
// notifications/tools/list_changed when finished.
//
// On bootstrap failure, the existing state is left intact and the error is
// returned to the caller.
func (p *Proxy) Reload(ctx context.Context) error {
	if p.bootstrapper == nil {
		return fmt.Errorf("bootstrapper not configured (cannot reload)")
	}

	// Reload re-spawns EVERY persistent child via exec.CommandContext. On the
	// HTTP transport ctx is the per-request context (r.Context()), so completing
	// the reload response would SIGKILL all children at once. Detach so the new
	// child set outlives the triggering request (#408).
	ctx = context.WithoutCancel(ctx)

	res, err := p.bootstrapper.Bootstrap(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	// Build new configs + ephemeral maps from the fresh result.
	newConfigs := make(map[string]config.ServerConfig, len(res.ActiveServers))
	newEphemeral := make(map[string]*EphemeralMeta)
	for _, cfg := range res.ActiveServers {
		newConfigs[cfg.Name] = cfg
		if cfg.IsEphemeral(res.Ephemeral) {
			newEphemeral[cfg.Name] = &EphemeralMeta{Config: cfg}
		}
	}

	p.mu.Lock()
	oldChildren := p.children
	p.children = res.Children
	p.failed = res.Failed
	p.configs = newConfigs
	p.ephemeral = newEphemeral
	p.globalEphemeral = res.Ephemeral
	p.mu.Unlock()

	// Close old children outside the lock so a slow Close doesn't block
	// concurrent readers. Native servers Close as a no-op; subprocess
	// children may take longer.
	for _, c := range oldChildren {
		_ = c.Client.Close()
	}

	// Wire notification forwarding on the new child set.
	for _, c := range res.Children {
		c.Client.SetOnNotification(p.ForwardNotification)
	}

	p.ProbeEphemeral(ctx)

	p.notifyToolsChanged()
	return nil
}

func (p *Proxy) getToolsForServer(ctx context.Context, serverName string) ([]protocol.ToolV1, error) {
	if meta, ok := p.ephemeral[serverName]; ok {
		return meta.Tools, nil
	}

	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	child, ok := findChildIn(children, serverName)
	if !ok {
		return nil, fmt.Errorf("unknown server %q", serverName)
	}

	if child.Capabilities.Tools == nil {
		return nil, nil
	}

	raw, err := child.Client.Call(ctx, protocol.MethodToolsList, nil)
	if err != nil {
		return nil, fmt.Errorf("listing tools from %s: %w", serverName, err)
	}

	tools, err := decodeToolsList(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding tools from %s: %w", serverName, err)
	}

	return tools, nil
}

func (p *Proxy) restartServer(ctx context.Context, serverName string) error {
	debugLog("restartServer %s", serverName)

	// A persistent child is spawned via exec.CommandContext, which SIGKILLs the
	// process when its context is cancelled. On the HTTP transport the caller's
	// ctx is the per-request context (r.Context()), cancelled by net/http when
	// the restart response completes — which would kill the freshly-respawned
	// child moments after this returns. Detach so a persistent child's lifetime
	// is tied to the moxy process, not the triggering request (#408). Retains
	// context values; ephemeral/moxin restart paths below are unaffected.
	ctx = context.WithoutCancel(ctx)

	// Moxin (native) servers: live in p.children but not in p.configs.
	// Detect by ChildEntry.Client type and re-discover via MoxinReloader.
	if p.isMoxinChild(serverName) {
		return p.restartMoxin(serverName)
	}

	cfg, ok := p.configs[serverName]
	if !ok {
		return fmt.Errorf("unknown server %q", serverName)
	}

	// Ephemeral servers: re-probe to refresh cached capabilities
	if meta, isEphemeral := p.ephemeral[serverName]; isEphemeral {
		return p.reprobeEphemeral(ctx, meta)
	}

	// Spawn the replacement FIRST, while the old child keeps serving, so the
	// server is never absent from the registry during its own restart. The
	// proxy handles messages in goroutines, so a tool call pipelined right
	// after a restart runs concurrently with it; the previous order — remove
	// old, then run the slow connectFunc, then add new — left a window where
	// that call found neither instance and got "unknown server", which flaked
	// under CPU contention once the respawn outran the client's spacing (#351).
	// Mirrors restartMoxin's make-before-break.
	client, result, err := p.connectFunc(ctx, cfg)
	if err != nil {
		// Respawn failed: leave the old child in place (still serving) and
		// mark the server failed, matching restartMoxin — a failed restart
		// must not tear down a healthy child.
		p.markFailed(serverName, err)
		return fmt.Errorf("spawning %s: %w", serverName, err)
	}

	client.SetOnNotification(func(msg *jsonrpc.Message) {
		p.ForwardNotification(msg)
	})

	// Probe tools/list before swapping the new child in, so "restarted
	// successfully" actually means the child is serving. A child can complete
	// `initialize` and then immediately drop its stdio pipe; without this probe
	// restart reported success and the closed-pipe error ("write |1: file
	// already closed") only surfaced later on the first real tools/list (#405).
	// On probe failure, tear down the dead new client and leave the old child
	// in place (still serving), mirroring the connectFunc-failure branch above.
	if result.Capabilities.Tools != nil {
		if _, probeErr := client.Call(ctx, protocol.MethodToolsList, nil); probeErr != nil {
			_ = client.Close()
			err := fmt.Errorf("listing tools from %s after restart: %w", serverName, probeErr)
			p.markFailed(serverName, err)
			return err
		}
	}

	// Atomically swap old → new under one lock: close+remove the old child,
	// drop any failed entry, add the new child. A concurrent tool call sees
	// either the old or the new srv, never neither.
	p.mu.Lock()
	for i, c := range p.children {
		if c.Client.Name() == serverName {
			debugLog("restartServer closing old %s", serverName)
			c.Client.Close()
			p.children = append(p.children[:i], p.children[i+1:]...)
			break
		}
	}
	for i, f := range p.failed {
		if f.Name == serverName {
			p.failed = append(p.failed[:i], p.failed[i+1:]...)
			break
		}
	}
	p.children = append(p.children, ChildEntry{
		Client:       client,
		Config:       cfg,
		Capabilities: result.Capabilities,
		ServerInfo:   result.ServerInfo,
		Instructions: result.Instructions,
	})
	p.mu.Unlock()

	p.notifyToolsChanged()

	return nil
}

func (p *Proxy) isMoxinChild(serverName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, c := range p.children {
		if c.Client.Name() != serverName {
			continue
		}
		if _, ok := c.Client.(*native.Server); ok {
			return true
		}
		return false
	}
	return false
}

func (p *Proxy) restartMoxin(serverName string) error {
	if p.moxinReloader == nil {
		return fmt.Errorf("moxin reloader not configured (cannot restart %q)", serverName)
	}

	nc, err := p.moxinReloader.ReloadMoxin(serverName)
	if err != nil {
		p.markFailed(serverName, err)
		return fmt.Errorf("re-discovering moxin %s: %w", serverName, err)
	}

	srv := native.NewServer(nc)
	if p.sessionID != "" {
		srv.SetSession(p.sessionID)
	}
	if p.madder != nil {
		srv.SetMadder(p.madder)
	}
	srv.SetOnNotification(p.ForwardNotification)
	initResult := srv.InitializeResult()

	p.mu.Lock()
	for i, c := range p.children {
		if c.Client.Name() == serverName {
			debugLog("restartMoxin closing old %s", serverName)
			c.Client.Close()
			p.children = append(p.children[:i], p.children[i+1:]...)
			break
		}
	}
	for i, f := range p.failed {
		if f.Name == serverName {
			p.failed = append(p.failed[:i], p.failed[i+1:]...)
			break
		}
	}
	p.children = append(p.children, ChildEntry{
		Client:       srv,
		Config:       config.ServerConfig{Name: nc.Name},
		Capabilities: initResult.Capabilities,
		ServerInfo:   initResult.ServerInfo,
		Instructions: nc.Description,
	})
	p.mu.Unlock()

	p.notifyToolsChanged()
	return nil
}

func (p *Proxy) getPromptEphemeral(
	ctx context.Context,
	serverName string,
	promptName string,
	args map[string]string,
) (*protocol.PromptGetResultV1, error) {
	client, err := p.spawnEphemeral(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("spawning ephemeral %s: %w", serverName, err)
	}
	defer client.Close()

	params := protocol.PromptGetParams{
		Name:      promptName,
		Arguments: args,
	}

	raw, err := client.Call(ctx, protocol.MethodPromptsGet, params)
	if err != nil {
		return nil, fmt.Errorf("getting prompt %s from ephemeral %s: %w", promptName, serverName, err)
	}

	result, err := decodePromptGetResult(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding prompt get result from ephemeral %s: %w", serverName, err)
	}

	return result, nil
}

func (p *Proxy) readResourceEphemeral(
	ctx context.Context,
	serverName string,
	uri string,
) (*protocol.ResourceReadResult, error) {
	client, err := p.spawnEphemeral(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("spawning ephemeral %s: %w", serverName, err)
	}
	defer client.Close()

	meta := p.ephemeral[serverName]
	if meta.Config.Paginate {
		uri, _ = paginate.ParseParams(uri)
	}

	params := protocol.ResourceReadParams{URI: uri}
	raw, err := client.Call(ctx, protocol.MethodResourcesRead, params)
	if err != nil {
		return nil, fmt.Errorf("reading resource %s from ephemeral %s: %w", uri, serverName, err)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding resource read result from ephemeral %s: %w", serverName, err)
	}

	return &result, nil
}

func (p *Proxy) callToolEphemeral(
	ctx context.Context,
	serverName string,
	toolName string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	client, err := p.spawnEphemeral(ctx, serverName)
	if err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("spawning ephemeral %s: %v", serverName, err),
		), nil
	}
	defer client.Close()

	if toolName == "resource-read" {
		return p.callResourceReadOn(ctx, client, serverName, args)
	}

	if toolName == "resource-templates" {
		return p.callResourceTemplatesOn(ctx, client, serverName)
	}

	params := protocol.ToolCallParams{
		Name:      toolName,
		Arguments: args,
	}

	raw, err := client.Call(ctx, protocol.MethodToolsCall, params)
	if err != nil {
		return nil, fmt.Errorf(
			"calling tool %s on ephemeral %s: %w",
			toolName, serverName, err,
		)
	}

	result, err := decodeToolCallResult(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding tool call result from ephemeral %s: %w",
			serverName, err,
		)
	}

	return result, nil
}

func (p *Proxy) callResourceReadOn(
	ctx context.Context,
	client ServerBackend,
	serverName string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid resource-read args: %v", err),
		), nil
	}

	raw, err := client.Call(
		ctx,
		protocol.MethodResourcesRead,
		protocol.ResourceReadParams{URI: params.URI},
	)
	if err != nil {
		return nil, fmt.Errorf("reading resource %s from %s: %w", params.URI, serverName, err)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding resource read result from %s: %w", serverName, err)
	}

	return resourceContentsToToolResult(result.Contents)
}

// resourceContentsToToolResult converts MCP resource contents into a tool call
// result. When there is exactly one text content item, the text is returned
// directly as a plain text block (no JSON wrapping). Otherwise, the contents
// array is JSON-marshaled for structured access.
func resourceContentsToToolResult(contents []protocol.ResourceContent) (*protocol.ToolCallResultV1, error) {
	if len(contents) == 1 && contents[0].Text != "" {
		return &protocol.ToolCallResultV1{
			Content: []protocol.ContentBlockV1{
				protocol.TextContentV1(contents[0].Text),
			},
		}, nil
	}

	text, err := json.Marshal(contents)
	if err != nil {
		return nil, fmt.Errorf("marshaling resource contents: %w", err)
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: string(text)},
		},
	}, nil
}

func (p *Proxy) callResourceTemplatesOn(
	ctx context.Context,
	client ServerBackend,
	serverName string,
) (*protocol.ToolCallResultV1, error) {
	raw, err := client.Call(ctx, protocol.MethodResourcesTemplates, nil)
	if err != nil {
		return nil, fmt.Errorf("listing resource templates from %s: %w", serverName, err)
	}

	templates, err := decodeResourceTemplatesList(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding resource templates from %s: %w", serverName, err)
	}

	text, err := json.Marshal(templates)
	if err != nil {
		return nil, fmt.Errorf("marshaling resource templates: %w", err)
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: string(text)},
		},
	}, nil
}

func (p *Proxy) callResourceRead(
	ctx context.Context,
	child ChildEntry,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid resource-read args: %v", err),
		), nil
	}

	raw, err := child.Client.Call(
		ctx,
		protocol.MethodResourcesRead,
		protocol.ResourceReadParams{URI: params.URI},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"reading resource %s from %s: %w",
			params.URI,
			child.Client.Name(),
			err,
		)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf(
			"decoding resource read result from %s: %w",
			child.Client.Name(),
			err,
		)
	}

	return resourceContentsToToolResult(result.Contents)
}

func (p *Proxy) callResourceTemplates(
	ctx context.Context,
	child ChildEntry,
) (*protocol.ToolCallResultV1, error) {
	raw, err := child.Client.Call(
		ctx,
		protocol.MethodResourcesTemplates,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"listing resource templates from %s: %w",
			child.Client.Name(),
			err,
		)
	}

	templates, err := decodeResourceTemplatesList(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding resource templates from %s: %w",
			child.Client.Name(),
			err,
		)
	}

	text, err := json.Marshal(templates)
	if err != nil {
		return nil, fmt.Errorf("marshaling resource templates: %w", err)
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: string(text)},
		},
	}, nil
}

func (p *Proxy) findChild(name string) (ChildEntry, bool) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()
	return findChildIn(children, name)
}

func findChildIn(children []ChildEntry, name string) (ChildEntry, bool) {
	for _, c := range children {
		if c.Client.Name() == name {
			return c, true
		}
	}
	return ChildEntry{}, false
}

func boolPtr(b bool) *bool { return &b }

func readOnlyAnnotations() *protocol.ToolAnnotations {
	return &protocol.ToolAnnotations{
		ReadOnlyHint: boolPtr(true),
	}
}

func prefixToolTitle(tool *protocol.ToolV1, serverName string) {
	if tool.Title != "" {
		tool.Title = serverName + ": " + tool.Title
	}
	if tool.Annotations != nil && tool.Annotations.Title != "" {
		tool.Annotations.Title = serverName + ": " + tool.Annotations.Title
	}
}

func splitPrefix(s, sep string) (prefix, rest string, ok bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

func matchesAnnotationFilter(
	annotations *protocol.ToolAnnotations,
	filter *config.AnnotationFilter,
) bool {
	if filter == nil {
		return true
	}
	if annotations == nil {
		return false
	}

	// OR semantics: a tool matches if ANY configured hint matches.
	if filter.ReadOnlyHint != nil &&
		annotations.ReadOnlyHint != nil &&
		*annotations.ReadOnlyHint == *filter.ReadOnlyHint {
		return true
	}
	if filter.DestructiveHint != nil &&
		annotations.DestructiveHint != nil &&
		*annotations.DestructiveHint == *filter.DestructiveHint {
		return true
	}
	if filter.IdempotentHint != nil &&
		annotations.IdempotentHint != nil &&
		*annotations.IdempotentHint == *filter.IdempotentHint {
		return true
	}
	if filter.OpenWorldHint != nil &&
		annotations.OpenWorldHint != nil &&
		*annotations.OpenWorldHint == *filter.OpenWorldHint {
		return true
	}
	return false
}

func paginateResourceResult(
	result protocol.ResourceReadResult,
	params paginate.Params,
) protocol.ResourceReadResult {
	for i, content := range result.Contents {
		if content.Text == "" {
			continue
		}
		sliced, err := paginate.SliceArray(content.Text, params)
		if err != nil {
			// Not a JSON array or pagination not active — pass through
			continue
		}
		wrapped, err := json.Marshal(sliced)
		if err != nil {
			continue
		}
		result.Contents[i].Text = string(wrapped)
	}
	return result
}

type cursorParam struct {
	Cursor string `json:"cursor,omitempty"`
}

func cursorParams(cursor string) *cursorParam {
	if cursor == "" {
		return nil
	}
	return &cursorParam{Cursor: cursor}
}

// decodeToolsList tries V1 first, falls back to V0 and upgrades.
func decodeToolsList(raw json.RawMessage) ([]protocol.ToolV1, error) {
	var v1 protocol.ToolsListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.Tools) > 0 {
		return v1.Tools, nil
	}

	var v0 protocol.ToolsListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		tools := make([]protocol.ToolV1, len(v0.Tools))
		for i, t := range v0.Tools {
			tools[i] = protocol.ToolV1{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			}
		}
		return tools, nil
	}

	return nil, fmt.Errorf("unable to decode tools list response")
}

// decodeToolCallResult tries V1 first, falls back to V0 and upgrades.
func decodeToolCallResult(
	raw json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var v1 protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &v1); err == nil {
		return &v1, nil
	}

	var v0 protocol.ToolCallResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		return &protocol.ToolCallResultV1{
			Content: upgradeContentBlocks(v0.Content),
			IsError: v0.IsError,
		}, nil
	}

	return nil, fmt.Errorf("unable to decode tool call result")
}

// decodeResourcesList tries V1 first, falls back to V0 and upgrades.
func decodeResourcesList(raw json.RawMessage) ([]protocol.ResourceV1, error) {
	var v1 protocol.ResourcesListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.Resources) > 0 {
		return v1.Resources, nil
	}

	var v0 protocol.ResourcesListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		resources := make([]protocol.ResourceV1, len(v0.Resources))
		for i, r := range v0.Resources {
			resources[i] = protocol.ResourceV1{
				URI:         r.URI,
				Name:        r.Name,
				Description: r.Description,
				MimeType:    r.MimeType,
			}
		}
		return resources, nil
	}

	return nil, fmt.Errorf("unable to decode resources list response")
}

// decodeResourceTemplatesList tries V1 first, falls back to V0 and upgrades.
func decodeResourceTemplatesList(
	raw json.RawMessage,
) ([]protocol.ResourceTemplateV1, error) {
	var v1 protocol.ResourceTemplatesListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil &&
		len(v1.ResourceTemplates) > 0 {
		return v1.ResourceTemplates, nil
	}

	var v0 protocol.ResourceTemplatesListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		templates := make(
			[]protocol.ResourceTemplateV1,
			len(v0.ResourceTemplates),
		)
		for i, t := range v0.ResourceTemplates {
			templates[i] = protocol.ResourceTemplateV1{
				URITemplate: t.URITemplate,
				Name:        t.Name,
				Description: t.Description,
				MimeType:    t.MimeType,
			}
		}
		return templates, nil
	}

	return nil, fmt.Errorf("unable to decode resource templates list response")
}

func downgradeContentBlocks(
	blocks []protocol.ContentBlockV1,
) []protocol.ContentBlock {
	out := make([]protocol.ContentBlock, len(blocks))
	for i, b := range blocks {
		// V1 resource blocks have no V0 equivalent — flatten to text.
		if b.Type == "resource" && b.Resource != nil {
			text := ""
			if b.Resource.Text != nil {
				text = *b.Resource.Text
			}
			out[i] = protocol.ContentBlock{
				Type: "text",
				Text: text,
			}
			continue
		}
		out[i] = protocol.ContentBlock{
			Type:     b.Type,
			Text:     b.Text,
			MimeType: b.MimeType,
			Data:     b.Data,
		}
	}
	return out
}

func upgradeContentBlocks(
	blocks []protocol.ContentBlock,
) []protocol.ContentBlockV1 {
	out := make([]protocol.ContentBlockV1, len(blocks))
	for i, b := range blocks {
		out[i] = protocol.ContentBlockV1{
			Type:     b.Type,
			Text:     b.Text,
			MimeType: b.MimeType,
			Data:     b.Data,
		}
	}
	return out
}

// decodePromptsList tries V1 first, falls back to V0 and upgrades.
func decodePromptsList(raw json.RawMessage) ([]protocol.PromptV1, error) {
	var v1 protocol.PromptsListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.Prompts) > 0 {
		return v1.Prompts, nil
	}

	var v0 protocol.PromptsListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		prompts := make([]protocol.PromptV1, len(v0.Prompts))
		for i, p := range v0.Prompts {
			prompts[i] = protocol.PromptV1{
				Name:        p.Name,
				Description: p.Description,
				Arguments:   p.Arguments,
			}
		}
		return prompts, nil
	}

	return nil, fmt.Errorf("unable to decode prompts list response")
}

// decodePromptGetResult tries V1 first, falls back to V0 and upgrades.
func decodePromptGetResult(
	raw json.RawMessage,
) (*protocol.PromptGetResultV1, error) {
	var v1 protocol.PromptGetResultV1
	if err := json.Unmarshal(raw, &v1); err == nil {
		return &v1, nil
	}

	var v0 protocol.PromptGetResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		messages := make([]protocol.PromptMessageV1, len(v0.Messages))
		for i, m := range v0.Messages {
			messages[i] = protocol.PromptMessageV1{
				Role: m.Role,
				Content: protocol.ContentBlockV1{
					Type:     m.Content.Type,
					Text:     m.Content.Text,
					MimeType: m.Content.MimeType,
					Data:     m.Content.Data,
				},
			}
		}
		return &protocol.PromptGetResultV1{
			Description: v0.Description,
			Messages:    messages,
		}, nil
	}

	return nil, fmt.Errorf("unable to decode prompt get result")
}

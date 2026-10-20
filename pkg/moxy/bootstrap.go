package moxy

import (
	"context"
	"fmt"
	"io"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/proxy"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// bootstrapResult is the full picture one bootstrap produces: the judged,
// expanded config, the spawned child servers (subprocess + moxin), the
// failed-startup list, and the moxin configs actually served (the
// permission resolver is built over exactly these).
type bootstrapResult struct {
	cfg           moxyfile.Config
	activeServers []moxyfile.ServerConfig
	children      []proxy.ChildEntry
	failed        []proxy.FailedServer
	moxins        []*native.NativeConfig
	systemDir     string
}

// bootstrapInputs are the dependencies every bootstrap — startup, a full
// reload — shares, resolved once in Open so a reload runs with the same
// connect, blob store and session the startup used.
type bootstrapInputs struct {
	serving   *serving
	moxinPath string
	systemDir string // "" → resolve from the moxyfile's builtin-native and the build default
	sessionID string
	connect   ConnectFunc
	blobs     BlobStore
	stderr    io.Writer
}

// bootstrap loads the moxyfile, discovers moxins, judges the pair against
// the parent reference, and only then spawns subprocess children for every
// active [[servers]] entry and builds an in-process *native.Server per
// moxin. It re-reads the moxyfile source on every call, so configuration
// edits made between startup and reload are picked up.
func bootstrap(ctx context.Context, in bootstrapInputs) (*bootstrapResult, error) {
	literal, err := in.serving.src.load()
	if err != nil {
		return nil, err
	}
	stderr := in.stderr

	systemDir := in.systemDir
	if builtinNative := literal.BuiltinNative; builtinNative == nil || *builtinNative {
		if systemDir == "" {
			systemDir = native.SystemMoxinDir()
		}
		fmt.Fprintf(stderr, "moxy: bootstrap: builtin-native enabled, systemDir=%q\n", systemDir)
	} else {
		fmt.Fprintf(stderr, "moxy: bootstrap: builtin-native DISABLED (cfg.BuiltinNative=%v)\n", *builtinNative)
		systemDir = ""
	}

	fmt.Fprintf(stderr, "moxy: bootstrap: MOXIN_PATH=%q\n", in.moxinPath)
	discovered, err := native.DiscoverServing(in.moxinPath, systemDir)
	if err != nil {
		fmt.Fprintf(stderr, "moxy: warning: moxin discovery: %v\n", err)
	}
	for _, loadErr := range discovered.Errors {
		fmt.Fprintf(stderr, "moxy: skipping moxin %s: %v\n", loadErr.Dir, loadErr.Err)
	}
	nativeConfigs := discovered.Configs
	fmt.Fprintf(stderr, "moxy: bootstrap: discovered %d moxin configs\n", len(nativeConfigs))

	// Nothing is spawned until the config has been judged, against the very
	// moxins about to be served: a moxyfile or MOXIN_PATH that widens the
	// parent reference fails here, before any child exists. Discovery only
	// reads moxin TOML, so running it first starts nothing. This is the one
	// call that decides what is actually served (startup's first run, and
	// every reload after): eligible to pin the moxy#461 startup allow-tools
	// baseline if nothing has pinned one yet.
	if err := in.serving.refuseUnlessNarrows(literal, native.MoxinSources(nativeConfigs), true); err != nil {
		return nil, err
	}
	cfg := moxyfile.ExpandHeaders(literal)

	for _, srv := range cfg.Servers {
		if srv.Name == "" {
			return nil, fmt.Errorf("server has no name")
		}
		if srv.Command.IsEmpty() && !srv.IsHTTP() {
			return nil, fmt.Errorf("server %q has no command or url", srv.Name)
		}
	}

	disableServers := cfg.BuildDisableServerSet()
	var activeServers []moxyfile.ServerConfig
	for _, srvCfg := range cfg.Servers {
		if disableServers.ServerDisabled(srvCfg.Name) {
			fmt.Fprintf(stderr, "moxy: skipping server %q (disabled by moxyfile)\n", srvCfg.Name)
			continue
		}
		activeServers = append(activeServers, srvCfg)
	}

	var children []proxy.ChildEntry
	var failed []proxy.FailedServer
	for _, srvCfg := range activeServers {
		if srvCfg.IsEphemeral(cfg.Ephemeral) {
			fmt.Fprintf(stderr, "moxy: %s configured as ephemeral (on-demand)\n", srvCfg.Name)
			continue
		}

		client, result, err := in.connect(ctx, srvCfg)
		if err != nil {
			fmt.Fprintf(stderr, "moxy: failed to start %s: %v\n", srvCfg.Name, err)
			failed = append(failed, proxy.FailedServer{
				Name:  srvCfg.Name,
				Error: err.Error(),
			})
			continue
		}

		children = append(children, proxy.ChildEntry{
			Client:       client,
			Config:       srvCfg,
			Capabilities: result.Capabilities,
			ServerInfo:   result.ServerInfo,
			Instructions: result.Instructions,
		})

		fmt.Fprintf(stderr, "moxy: connected to %s (%s %s)\n",
			srvCfg.Name, result.ServerInfo.Name, result.ServerInfo.Version)
	}

	existingNames := make(map[string]bool)
	for _, c := range children {
		existingNames[c.Config.Name] = true
	}
	for _, f := range failed {
		existingNames[f.Name] = true
	}
	for _, s := range cfg.Servers {
		existingNames[s.Name] = true
	}

	disableSet := cfg.BuildDisableMoxinSet()

	// A moxin that was asked for and refused is a failed server, so the
	// client sees `<name>.status` saying why instead of a missing namespace.
	// One the moxyfile shadows or disables was never going to be served.
	for _, refused := range discovered.Refused {
		if existingNames[refused.Name] || disableSet.ServerDisabled(refused.Name) {
			continue
		}
		failed = append(failed, proxy.FailedServer{
			Name:  refused.Name,
			Error: fmt.Sprintf("moxin %s failed to load: %v", refused.Dir, refused.Err),
		})
	}

	var served []*native.NativeConfig
	for _, nc := range nativeConfigs {
		if existingNames[nc.Name] {
			fmt.Fprintf(stderr, "moxy: skipping moxin %q (name collision with moxyfile server)\n", nc.Name)
			continue
		}
		if disableSet.ServerDisabled(nc.Name) {
			fmt.Fprintf(stderr, "moxy: skipping moxin %q (disabled by moxyfile)\n", nc.Name)
			continue
		}

		filtered := nc.Tools[:0]
		for _, t := range nc.Tools {
			if disableSet.ToolDisabled(nc.Name, t.Name) {
				fmt.Fprintf(stderr, "moxy: disabling tool %s.%s (disabled by moxyfile)\n", nc.Name, t.Name)
				continue
			}
			filtered = append(filtered, t)
		}
		nc.Tools = filtered

		srv := native.NewServer(nc)
		if in.sessionID != "" {
			srv.SetSession(in.sessionID)
		}
		if in.blobs != nil {
			srv.SetMadder(in.blobs)
		}
		initResult := srv.InitializeResult()
		children = append(children, proxy.ChildEntry{
			Client:       srv,
			Config:       moxyfile.ServerConfig{Name: nc.Name},
			Capabilities: initResult.Capabilities,
			ServerInfo:   initResult.ServerInfo,
			Instructions: nc.Description,
		})
		served = append(served, nc)
		fmt.Fprintf(stderr, "moxy: registered moxin %s (%d tools)\n", nc.Name, len(nc.Tools))
	}

	fmt.Fprintf(stderr, "moxy: bootstrap: total children: %d\n", len(children))

	return &bootstrapResult{
		cfg:           cfg,
		activeServers: activeServers,
		children:      children,
		failed:        failed,
		moxins:        served,
		systemDir:     systemDir,
	}, nil
}

// bootstrapper is the proxy.Bootstrapper behind a full reload: the same
// inputs startup used, so Reload re-spawns subprocess children and rebuilds
// moxin children against a re-read moxyfile, judged against the pinned
// parent reference.
type bootstrapper struct {
	in       bootstrapInputs
	onReload func(*bootstrapResult)
}

func (b *bootstrapper) Bootstrap(ctx context.Context) (*proxy.BootstrapResult, error) {
	res, err := bootstrap(ctx, b.in)
	if err != nil {
		fmt.Fprintf(b.in.stderr, "moxy: reload refused, keeping the running configuration: %v\n", err)
		return nil, err
	}
	if b.onReload != nil {
		b.onReload(res)
	}
	return &proxy.BootstrapResult{
		Children:      res.children,
		Failed:        res.failed,
		ActiveServers: res.activeServers,
		Ephemeral:     res.cfg.Ephemeral,
		AllowTools:    res.cfg.BuildAllowToolSet(),
	}, nil
}

// moxinReloader is the proxy.MoxinReloader behind `restart <moxin>`: it
// re-reads the moxyfile so manual disable-moxins edits and [[servers]]
// collisions made since boot are honoured, re-discovers the moxin, and
// judges the pair against the pinned parent reference like a full reload.
type moxinReloader struct {
	in bootstrapInputs
}

func (r *moxinReloader) ReloadMoxin(name string) (*native.NativeConfig, error) {
	literal, err := r.in.serving.src.load()
	if err != nil {
		return nil, fmt.Errorf("re-loading moxyfile hierarchy: %w", err)
	}

	// The moxyfile may have turned the system moxins off since boot; a
	// reload must not bring one back by name.
	systemDir := r.in.systemDir
	if literal.BuiltinNative != nil && !*literal.BuiltinNative {
		systemDir = ""
	}
	configs, err := native.DiscoverConfigs(r.in.moxinPath, systemDir)
	if err != nil {
		return nil, fmt.Errorf("discovering moxins: %w", err)
	}

	// Same gate as a full reload: the re-read moxyfile and the moxins found
	// just now must still narrow the parent reference.
	if err := r.in.serving.refuseUnlessNarrows(literal, native.MoxinSources(configs), true); err != nil {
		fmt.Fprintf(r.in.stderr, "moxy: reload refused, keeping the running configuration: %v\n", err)
		return nil, err
	}

	existingNames := make(map[string]bool)
	for _, s := range literal.Servers {
		existingNames[s.Name] = true
	}
	if existingNames[name] {
		return nil, fmt.Errorf("moxin %q collides with a moxyfile server of the same name", name)
	}
	disableSet := literal.BuildDisableMoxinSet()
	if disableSet.ServerDisabled(name) {
		return nil, fmt.Errorf("moxin %q is disabled by the moxyfile", name)
	}

	for _, nc := range configs {
		if nc.Name != name {
			continue
		}
		filtered := nc.Tools[:0]
		for _, t := range nc.Tools {
			if disableSet.ToolDisabled(nc.Name, t.Name) {
				continue
			}
			filtered = append(filtered, t)
		}
		nc.Tools = filtered
		return nc, nil
	}
	return nil, fmt.Errorf("moxin %q not found on MOXIN_PATH or in the system moxin dir", name)
}

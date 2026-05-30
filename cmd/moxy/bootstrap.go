package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/native"
	"github.com/amarbel-llc/moxy/internal/proxy"
)

// bootstrapInputs are the static inputs threaded through bootstrap. Closures
// are bundled here so reload can call bootstrap with the same configuration
// that startup used.
type bootstrapInputs struct {
	moxinPath string
	systemDir string
	sessionID string
	connect   proxy.ConnectFunc
	madder    native.MadderBackend
}

// bootstrapResult is the full picture bootstrap produces: the merged config,
// the spawned child servers (subprocess + moxin), the ephemeral metadata
// (empty until ProbeEphemeral runs), and the failed-startup list.
type bootstrapResult struct {
	cfg           config.Config
	activeServers []config.ServerConfig
	children      []proxy.ChildEntry
	failed        []proxy.FailedServer
	moxinPath     string
	systemDir     string
}

// bootstrap loads the moxyfile hierarchy, spawns subprocess children for
// every active [[servers]] entry, discovers moxins from MOXIN_PATH, and
// builds in-process *native.Server children for each. Returns the full
// picture for the proxy constructor (or for Reload) to consume.
//
// bootstrap re-reads the moxyfile hierarchy on every call, so configuration
// edits made between startup and reload are picked up.
func bootstrap(ctx context.Context, inputs bootstrapInputs) (*bootstrapResult, error) {
	hierarchy, err := config.LoadDefaultHierarchy()
	if err != nil {
		return nil, err
	}
	cfg := hierarchy.Merged

	for _, srv := range cfg.Servers {
		if srv.Name == "" {
			return nil, fmt.Errorf("server has no name")
		}
		if srv.Command.IsEmpty() && !srv.IsHTTP() {
			return nil, fmt.Errorf("server %q has no command or url", srv.Name)
		}
	}

	disableServers := cfg.BuildDisableServerSet()
	var activeServers []config.ServerConfig
	for _, srvCfg := range cfg.Servers {
		if disableServers.ServerDisabled(srvCfg.Name) {
			fmt.Fprintf(os.Stderr, "moxy: skipping server %q (disabled by moxyfile)\n", srvCfg.Name)
			continue
		}
		activeServers = append(activeServers, srvCfg)
	}

	var children []proxy.ChildEntry
	var failed []proxy.FailedServer
	for _, srvCfg := range activeServers {
		if srvCfg.IsEphemeral(cfg.Ephemeral) {
			fmt.Fprintf(os.Stderr, "moxy: %s configured as ephemeral (on-demand)\n", srvCfg.Name)
			continue
		}

		client, result, err := inputs.connect(ctx, srvCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "moxy: failed to start %s: %v\n", srvCfg.Name, err)
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

		fmt.Fprintf(os.Stderr, "moxy: connected to %s (%s %s)\n",
			srvCfg.Name, result.ServerInfo.Name, result.ServerInfo.Version)
	}

	systemDir := inputs.systemDir
	if cfg.BuiltinNative == nil || *cfg.BuiltinNative {
		if systemDir == "" {
			systemDir = native.SystemMoxinDir()
		}
		fmt.Fprintf(os.Stderr, "moxy: bootstrap: builtin-native enabled, systemDir=%q\n", systemDir)
	} else {
		fmt.Fprintf(os.Stderr, "moxy: bootstrap: builtin-native DISABLED (cfg.BuiltinNative=%v)\n", *cfg.BuiltinNative)
		systemDir = ""
	}

	fmt.Fprintf(os.Stderr, "moxy: bootstrap: MOXIN_PATH=%q\n", inputs.moxinPath)
	nativeConfigs, err := native.DiscoverConfigs(inputs.moxinPath, systemDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moxy: warning: moxin discovery: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "moxy: bootstrap: discovered %d moxin configs\n", len(nativeConfigs))

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

	for _, nc := range nativeConfigs {
		if existingNames[nc.Name] {
			fmt.Fprintf(os.Stderr, "moxy: skipping moxin %q (name collision with moxyfile server)\n", nc.Name)
			continue
		}
		if disableSet.ServerDisabled(nc.Name) {
			fmt.Fprintf(os.Stderr, "moxy: skipping moxin %q (disabled by moxyfile)\n", nc.Name)
			continue
		}

		filtered := nc.Tools[:0]
		for _, t := range nc.Tools {
			if disableSet.ToolDisabled(nc.Name, t.Name) {
				fmt.Fprintf(os.Stderr, "moxy: disabling tool %s.%s (disabled by moxyfile)\n", nc.Name, t.Name)
				continue
			}
			filtered = append(filtered, t)
		}
		nc.Tools = filtered

		srv := native.NewServer(nc)
		if inputs.sessionID != "" {
			srv.SetSession(inputs.sessionID)
		}
		if inputs.madder != nil {
			srv.SetMadder(inputs.madder)
		}
		initResult := srv.InitializeResult()
		children = append(children, proxy.ChildEntry{
			Client:       srv,
			Config:       config.ServerConfig{Name: nc.Name},
			Capabilities: initResult.Capabilities,
			ServerInfo:   initResult.ServerInfo,
			Instructions: nc.Description,
		})
		fmt.Fprintf(os.Stderr, "moxy: registered moxin %s (%d tools)\n", nc.Name, len(nc.Tools))
	}

	fmt.Fprintf(os.Stderr, "moxy: bootstrap: total children: %d\n", len(children))

	return &bootstrapResult{
		cfg:           cfg,
		activeServers: activeServers,
		children:      children,
		failed:        failed,
		moxinPath:     inputs.moxinPath,
		systemDir:     systemDir,
	}, nil
}

// bootstrapperImpl is the proxy.Bootstrapper implementation injected into
// the proxy. Capturing inputs (including the connect closure that depends
// on the credential store) makes the function re-runnable on Reload.
type bootstrapperImpl struct {
	inputs bootstrapInputs
}

func (b *bootstrapperImpl) Bootstrap(ctx context.Context) (*proxy.BootstrapResult, error) {
	res, err := bootstrap(ctx, b.inputs)
	if err != nil {
		return nil, err
	}
	return &proxy.BootstrapResult{
		Children:      res.children,
		Failed:        res.failed,
		ActiveServers: res.activeServers,
		Ephemeral:     res.cfg.Ephemeral,
	}, nil
}

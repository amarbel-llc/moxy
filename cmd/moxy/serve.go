package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/command"
	"code.linenisgreat.com/purse-first/libs/go-mcp/server"
	"code.linenisgreat.com/purse-first/libs/go-mcp/transport"

	"code.linenisgreat.com/moxy/internal/stderrlog"
	"code.linenisgreat.com/moxy/internal/streamhttp"
	"code.linenisgreat.com/moxy/pkg/moxy"
	"code.linenisgreat.com/moxy/pkg/perms"
)

type transportMode int

const (
	transportStdio transportMode = iota
	transportHTTP
)

// serveOptions turns the serve entry points' environment and flags into the
// library's Options (FDR 0014): MOXIN_PATH, MOXY_PARENT_MOXYFILE (loaded and
// validated the way the serving binary always read it), the session id
// chain, the meta tools plus go-mcp's own `status` tool, and the binary's
// stderr for startup chatter. The library itself reads none of the
// environment; this is the one place the binary's conventions become
// options.
func serveOptions(app *command.App, expose, nameTemplate, permissions string) (moxy.Options, string, error) {
	sessionID, sessionSource := resolveSessionID()
	mode, ok := perms.ParseMode(permissions)
	if !ok {
		return moxy.Options{}, "", fmt.Errorf("--permissions: unknown mode %q (want delegate or enforce)", permissions)
	}
	o := moxy.Options{
		MoxinPath:     os.Getenv("MOXIN_PATH"),
		SessionID:     sessionID,
		Permissions:   perms.Policy{Mode: mode},
		NameTemplate:  nameTemplate,
		Expose:        expose,
		MetaTools:     true,
		RegisterTools: app.RegisterMCPToolsV1,
		Stderr:        os.Stderr,
	}
	if path := os.Getenv(parentMoxyfileEnv); path != "" {
		parent, err := moxy.LoadParentReference(path)
		if err != nil {
			return moxy.Options{}, "", fmt.Errorf("%s: %w", parentMoxyfileEnv, err)
		}
		o.Parent = parent
		o.ParentLabel = fmt.Sprintf("%s (%s)", parentMoxyfileEnv, path)
	}
	return o, sessionSource, nil
}

func runServer(app *command.App, mode transportMode, listenAddr, expose, nameTemplate, permissions string) error {
	opts, sessionSource, err := serveOptions(app, expose, nameTemplate, permissions)
	if err != nil {
		return err
	}

	// Load and judge the moxyfile (MOXY_PARENT_MOXYFILE) before stderr is
	// redirected below: a refusal to serve has to reach the launcher's stderr
	// as written, and it must come before anything is started. A bad --expose
	// or --name-template fails fast here too, before any child is spawned.
	prepared, err := moxy.Prepare(opts)
	if err != nil {
		return err
	}

	// Redirect os.Stderr (including Go panic traces) to a per-session log
	// file so crashes that bypass the normal logging path can be recovered
	// after the fact. Rotate on clean return so a leftover entry in
	// active/ indicates the process was killed before it could shut down.
	if err := stderrlog.Init(app.Version); err != nil {
		log.Printf("stderrlog init: %v", err)
	}
	defer stderrlog.Rotate()

	fmt.Fprintf(os.Stderr, "moxy: session %s (from %s)\n", opts.SessionID, sessionSource)
	if policy := prepared.Policy(); policy.Enforcing() {
		fmt.Fprintf(os.Stderr, "moxy: permission-mode enforce: the server resolves every call's tier itself; %d enforce-allow grant(s)\n", len(policy.Allow))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// A fixed-address HTTP origin sits behind a tunnel, so bind it before the
	// slow part (spawning every child) rather than after: a restart is then a
	// held request, not a refused connection / 502. See streamhttp.StartupGate.
	var early *earlyHTTP
	if addr, source := fixedHTTPAddr(mode, listenAddr); addr != "" {
		early, err = startEarlyHTTP(ctx, addr, source)
		if err != nil {
			return err
		}
		defer early.close()
	}

	inst, err := prepared.Open(ctx)
	if err != nil {
		return err
	}

	switch mode {
	case transportHTTP:
		// A caller-supplied --listen address binds a fixed port and is
		// meant to sit behind a reverse proxy or tunnel — there is no
		// clown-plugin-host on the other end of stdout, so suppress the
		// handshake line and report the bound address on stderr only.
		if early != nil {
			return early.serve(inst)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			_ = inst.Close(context.Background())
			return fmt.Errorf("binding ephemeral port: %w", err)
		}
		fmt.Fprintf(os.Stdout, "1|1|tcp|%s|streamable-http\n", ln.Addr())
		fmt.Fprintf(os.Stderr, "moxy: serving streamable-http on %s (clown-plugin-protocol)\n", ln.Addr())
		return runHTTPServerOnListener(ctx, ln, inst, "")

	default:
		if early != nil {
			return early.serve(inst)
		}

		fmt.Fprintf(os.Stderr, "moxy: serving stdio\n")
		t := transport.NewStdio(os.Stdin, os.Stdout)
		inst.SetNotifier(t.Write)

		srv, err := server.New(t, server.Options{
			ServerName:    "moxy",
			ServerVersion: version,
			Instructions:  inst.Instructions(),
			Tools:         inst,
			Resources:     inst,
			Prompts:       inst,
		})
		if err != nil {
			_ = inst.Close(context.Background())
			return err
		}

		err = srv.Run(ctx)

		// Close interrupts in-flight async jobs BEFORE tearing down children
		// so each emits a terminal `done` (interrupted) while its dispatch
		// path is still alive — no job left open in the clown journal.
		_ = inst.Close(context.Background())

		return err
	}
}

// startupHoldMax bounds how long a request is held while children start; past
// it the caller gets a well-formed 503 + Retry-After instead of hanging into
// the tunnel's own (~100s) timeout.
const startupHoldMax = 90 * time.Second

// fixedHTTPAddr reports the caller-chosen address (and what selected it, for
// the stderr line) a fixed-bind HTTP origin listens on: --listen under
// serve-http, or MOXY_HTTP_ADDR under the default stdio command. Empty means
// no fixed bind (the ephemeral clown-plugin path, or plain stdio).
func fixedHTTPAddr(mode transportMode, listenAddr string) (addr, source string) {
	switch {
	case mode == transportHTTP && listenAddr != "":
		return listenAddr, "--listen"
	case mode == transportStdio:
		if a := os.Getenv("MOXY_HTTP_ADDR"); a != "" {
			return a, "MOXY_HTTP_ADDR"
		}
	}
	return "", ""
}

// earlyHTTP is a fixed-address origin already accepting connections while
// children are still starting. Its gate holds requests until serve installs
// the real handler.
type earlyHTTP struct {
	srv  *http.Server
	gate *streamhttp.StartupGate
	done chan error
}

// startEarlyHTTP binds addr and starts serving the startup gate. source labels
// the stderr line with what selected the fixed bind and never reaches the wire
// — unlike the ephemeral path, no clown-plugin handshake is printed to stdout.
func startEarlyHTTP(ctx context.Context, addr, source string) (*earlyHTTP, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	fmt.Fprintf(os.Stderr, "moxy: serving streamable-http on %s (%s)\n", ln.Addr(), source)
	fmt.Fprintf(os.Stderr, "moxy: accepting connections before children are ready; requests are held until startup completes\n")

	gate := streamhttp.NewStartupGate(startupHoldMax)
	e := &earlyHTTP{srv: &http.Server{Handler: gate}, gate: gate, done: make(chan error, 1)}
	shutdownOnDone(ctx, e.srv)
	go func() { e.done <- e.srv.Serve(ln) }()
	return e, nil
}

func (e *earlyHTTP) close() { e.srv.Close() }

// serve releases the held requests onto the real handler, then blocks until
// the server stops and tears down. A fixed address is a long-lived origin
// behind a tunnel: the session id is persisted so a restart is invisible to
// clients (the ephemeral clown-plugin path is one session per process and
// stays in memory).
func (e *earlyHTTP) serve(inst *moxy.Instance) error {
	e.gate.Ready(newStreamHTTP(inst, streamhttp.DefaultSessionIDPath()))
	return finishHTTPServe(inst, <-e.done)
}

func newStreamHTTP(inst *moxy.Instance, sessionIDPath string) *streamhttp.Server {
	// Dynamic system-prompt fragment for clown's RFC-0002 §5 contribution,
	// served over HTTP at /clown/system-prompt. Empty => the handler returns
	// 204 and clown keeps the static prompt.
	systemPrompt, _ := inst.SystemPromptFragment()
	httpSrv := streamhttp.New(streamhttp.Options{
		Tools:                inst,
		Resources:            inst,
		Prompts:              inst,
		ServerName:           "moxy",
		ServerVersion:        version,
		Instructions:         inst.Instructions(),
		SystemPromptFragment: systemPrompt,
		SessionIDPath:        sessionIDPath,
	})
	inst.SetNotifier(httpSrv.Notify)
	return httpSrv
}

// shutdownOnDone stops httpServer when ctx is cancelled. Clown grants up to 5s
// between SIGTERM and SIGKILL. Budget: 3s for HTTP graceful shutdown, 1s for
// child teardown, ~1s slack for stderrlog rotation and runtime exit.
func shutdownOnDone(ctx context.Context, httpServer *http.Server) {
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			httpServer.Close()
		}
	}()
}

func runHTTPServerOnListener(ctx context.Context, ln net.Listener, inst *moxy.Instance, sessionIDPath string) error {
	httpServer := &http.Server{Handler: newStreamHTTP(inst, sessionIDPath)}
	shutdownOnDone(ctx, httpServer)
	return finishHTTPServe(inst, httpServer.Serve(ln))
}

func finishHTTPServe(inst *moxy.Instance, err error) error {
	// Same ordering as the stdio path: interrupt async jobs while their
	// dispatch path is still alive, then tear down children.
	_ = inst.Close(context.Background())

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

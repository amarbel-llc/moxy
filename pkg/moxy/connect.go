package moxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/credentials"
	"code.linenisgreat.com/moxy/internal/mcpclient"
	"code.linenisgreat.com/moxy/internal/oauth"
	"code.linenisgreat.com/moxy/internal/proxy"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// defaultConnect is the binary's connect: a command is spawned over stdio;
// a url is a streamable-HTTP child with static headers, a headers-helper
// and the cached OAuth token from the credential store.
func defaultConnect(store credentials.Store, stderr io.Writer) ConnectFunc {
	return func(ctx context.Context, srvCfg moxyfile.ServerConfig) (proxy.ServerBackend, *protocol.InitializeResultV1, error) {
		if srvCfg.IsHTTP() {
			return connectHTTPServer(ctx, srvCfg, store, stderr)
		}
		exe, args := srvCfg.EffectiveCommand()
		return mcpclient.SpawnAndInitialize(ctx, srvCfg.Name, exe, args)
	}
}

func connectHTTPServer(ctx context.Context, cfg moxyfile.ServerConfig, store credentials.Store, stderr io.Writer) (*mcpclient.Client, *protocol.InitializeResultV1, error) {
	var opts []mcpclient.HTTPTransportOption

	if len(cfg.Headers) > 0 {
		opts = append(opts, mcpclient.WithHeaders(cfg.Headers))
	}

	if cfg.HeadersHelper != nil {
		dynamicHeaders, err := runHeadersHelper(*cfg.HeadersHelper, stderr)
		if err != nil {
			return nil, nil, fmt.Errorf("headers-helper %q: %w", *cfg.HeadersHelper, err)
		}
		opts = append(opts, mcpclient.WithHeaders(dynamicHeaders))
	}

	if cfg.OAuth != nil {
		tok, err := store.Read(cfg.Name)
		if err != nil {
			fmt.Fprintf(stderr, "moxy: no cached OAuth token for %s: %v\n", cfg.Name, err)
			fmt.Fprintf(stderr, "moxy: run 'moxy add' to authenticate %s\n", cfg.Name)
		} else if tok.Valid() {
			opts = append(opts, mcpclient.WithBearerToken(tok.AccessToken))
		} else if tok.RefreshToken != "" {
			newTok, err := oauth.RefreshToken(ctx, cfg.URL, cfg.OAuth.ClientID, tok.RefreshToken)
			if err != nil {
				fmt.Fprintf(stderr, "moxy: token refresh failed for %s: %v\n", cfg.Name, err)
				fmt.Fprintf(stderr, "moxy: run 'moxy add' to re-authenticate %s\n", cfg.Name)
			} else {
				if writeErr := store.Write(cfg.Name, newTok); writeErr != nil {
					fmt.Fprintf(stderr, "moxy: warning: could not cache refreshed token for %s: %v\n", cfg.Name, writeErr)
				}
				opts = append(opts, mcpclient.WithBearerToken(newTok.AccessToken))
			}
		}
	}

	t := mcpclient.NewHTTPTransport(cfg.URL, opts...)
	return mcpclient.ConnectAndInitialize(ctx, cfg.Name, t)
}

func runHeadersHelper(command string, stderr io.Writer) (map[string]string, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty headers-helper command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var headers map[string]string
	if err := json.Unmarshal(out, &headers); err != nil {
		return nil, fmt.Errorf("parsing headers-helper output as JSON: %w", err)
	}
	return headers, nil
}

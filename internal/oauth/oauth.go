// Package oauth implements OAuth 2.1 + PKCE for MCP HTTP servers.
//
// The flow follows the MCP spec: on 401 from an MCP server, discover
// the authorization server via .well-known metadata, optionally register
// as a dynamic client, then open the browser for user consent, receive
// the callback, and exchange the code for tokens.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/amarbel-llc/moxy/internal/credentials"
)

// ProtectedResourceMetadata is from .well-known/oauth-protected-resource.
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// AuthorizationServerMetadata is from .well-known/oauth-authorization-server.
type AuthorizationServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint,omitempty"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported,omitempty"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`
	GrantTypesSupported           []string `json:"grant_types_supported,omitempty"`
}

// ClientRegistrationResponse is returned from dynamic client registration.
type ClientRegistrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// TokenResponse is the OAuth token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// Config holds OAuth settings for a server.
type Config struct {
	ClientID     string
	CallbackPort int
}

// DiscoverAndAuthorize performs the full OAuth 2.1 + PKCE flow:
// 1. Discover protected resource metadata
// 2. Discover authorization server metadata
// 3. Register as dynamic client (if no client ID configured)
// 4. Open browser for authorization
// 5. Receive callback and exchange code for tokens
func DiscoverAndAuthorize(ctx context.Context, serverURL string, cfg Config) (credentials.Token, error) {
	// Step 1: Discover protected resource metadata
	prm, err := discoverProtectedResource(ctx, serverURL)
	if err != nil {
		return credentials.Token{}, fmt.Errorf("discovering protected resource: %w", err)
	}

	if len(prm.AuthorizationServers) == 0 {
		return credentials.Token{}, fmt.Errorf("no authorization servers in protected resource metadata")
	}

	authServerURL := prm.AuthorizationServers[0]

	// Step 2: Discover authorization server metadata
	asm, err := discoverAuthorizationServer(ctx, authServerURL)
	if err != nil {
		return credentials.Token{}, fmt.Errorf("discovering authorization server: %w", err)
	}

	// Step 3: Get or register client ID
	clientID := cfg.ClientID
	if clientID == "" {
		if asm.RegistrationEndpoint == "" {
			return credentials.Token{}, fmt.Errorf(
				"no client-id configured and server does not support dynamic client registration; " +
					"set [servers.oauth] client-id in your moxyfile",
			)
		}
		reg, err := registerClient(ctx, asm.RegistrationEndpoint, serverURL)
		if err != nil {
			return credentials.Token{}, fmt.Errorf("registering client: %w", err)
		}
		clientID = reg.ClientID
	}

	// Step 4: Generate PKCE verifier and challenge
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return credentials.Token{}, fmt.Errorf("generating PKCE: %w", err)
	}

	// Step 5: Start callback server
	callbackPort := cfg.CallbackPort
	if callbackPort == 0 {
		callbackPort = 0 // random port
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	callbackServer, redirectURI, err := startCallbackServer(callbackPort, codeCh, errCh)
	if err != nil {
		return credentials.Token{}, fmt.Errorf("starting callback server: %w", err)
	}
	defer callbackServer.Close()

	// Step 6: Build authorization URL and open browser
	state, err := randomString(32)
	if err != nil {
		return credentials.Token{}, fmt.Errorf("generating state: %w", err)
	}

	authURL := buildAuthorizationURL(asm.AuthorizationEndpoint, clientID, redirectURI, state, challenge)

	fmt.Printf("Opening browser for authorization...\n")
	fmt.Printf("If the browser doesn't open, visit: %s\n", authURL)

	if err := openBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser: %v\n", err)
	}

	// Step 7: Wait for callback
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return credentials.Token{}, fmt.Errorf("authorization callback: %w", err)
	case <-ctx.Done():
		return credentials.Token{}, ctx.Err()
	case <-time.After(5 * time.Minute):
		return credentials.Token{}, fmt.Errorf("authorization timed out (5 minutes)")
	}

	// Step 8: Exchange code for tokens
	tokenResp, err := exchangeCode(ctx, asm.TokenEndpoint, clientID, code, redirectURI, verifier)
	if err != nil {
		return credentials.Token{}, fmt.Errorf("exchanging code: %w", err)
	}

	tok := credentials.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
	}
	if tokenResp.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return tok, nil
}

// RefreshToken attempts to refresh an expired token.
func RefreshToken(ctx context.Context, serverURL, clientID, refreshToken string) (credentials.Token, error) {
	prm, err := discoverProtectedResource(ctx, serverURL)
	if err != nil {
		return credentials.Token{}, err
	}
	if len(prm.AuthorizationServers) == 0 {
		return credentials.Token{}, fmt.Errorf("no authorization servers")
	}

	asm, err := discoverAuthorizationServer(ctx, prm.AuthorizationServers[0])
	if err != nil {
		return credentials.Token{}, err
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}

	resp, err := http.PostForm(asm.TokenEndpoint, data)
	if err != nil {
		return credentials.Token{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return credentials.Token{}, fmt.Errorf("token refresh failed: HTTP %d: %s", resp.StatusCode, body)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return credentials.Token{}, err
	}

	tok := credentials.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken // keep old refresh token if not returned
	}
	if tokenResp.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return tok, nil
}

// ProbeRequiresAuth sends a GET to the server URL and checks for 401.
func ProbeRequiresAuth(ctx context.Context, serverURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusUnauthorized
}

func discoverProtectedResource(ctx context.Context, serverURL string) (*ProtectedResourceMetadata, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	wellKnownURL := fmt.Sprintf("%s://%s/.well-known/oauth-protected-resource", u.Scheme, u.Host)
	return fetchJSON[ProtectedResourceMetadata](ctx, wellKnownURL)
}

func discoverAuthorizationServer(ctx context.Context, authServerURL string) (*AuthorizationServerMetadata, error) {
	u, err := url.Parse(authServerURL)
	if err != nil {
		return nil, err
	}

	wellKnownURL := fmt.Sprintf("%s://%s/.well-known/oauth-authorization-server", u.Scheme, u.Host)
	return fetchJSON[AuthorizationServerMetadata](ctx, wellKnownURL)
}

func registerClient(ctx context.Context, registrationEndpoint, serverURL string) (*ClientRegistrationResponse, error) {
	body := map[string]any{
		"client_name":                "moxy",
		"redirect_uris":              []string{"http://127.0.0.1/callback"},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed: HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result ClientRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	verifier, err = randomString(43)
	if err != nil {
		return "", "", err
	}

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

func startCallbackServer(port int, codeCh chan<- string, errCh chan<- error) (*http.Server, string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", actualPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			desc := r.URL.Query().Get("error_description")
			errCh <- fmt.Errorf("%s: %s", errParam, desc)
			fmt.Fprintf(w, "<html><body><h1>Authorization Failed</h1><p>%s: %s</p><p>You can close this tab.</p></body></html>", errParam, desc)
			return
		}

		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			fmt.Fprintf(w, "<html><body><h1>Error</h1><p>No authorization code received.</p></body></html>")
			return
		}

		codeCh <- code
		fmt.Fprintf(w, "<html><body><h1>Authorization Successful</h1><p>You can close this tab and return to the terminal.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(ln)

	return server, redirectURI, nil
}

func buildAuthorizationURL(endpoint, clientID, redirectURI, state, challenge string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return endpoint + "?" + params.Encode()
}

func exchangeCode(ctx context.Context, tokenEndpoint, clientID, code, redirectURI, verifier string) (*TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed: HTTP %d: %s", resp.StatusCode, body)
	}

	var result TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func fetchJSON[T any](ctx context.Context, url string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func randomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b)[:length], nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

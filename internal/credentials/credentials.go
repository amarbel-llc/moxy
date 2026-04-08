package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const keychainService = "moxy"

// Token holds OAuth tokens for a server.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
}

// Valid reports whether the token has an access token that hasn't expired.
func (t Token) Valid() bool {
	if t.AccessToken == "" {
		return false
	}
	if t.Expiry.IsZero() {
		return true
	}
	return time.Now().Before(t.Expiry)
}

// Store reads, writes, and deletes tokens by server name.
type Store interface {
	Read(name string) (Token, error)
	Write(name string, tok Token) error
	Delete(name string) error
}

// CommandConfig configures a command-based credential store.
type CommandConfig struct {
	Read   string `toml:"read"`
	Write  string `toml:"write"`
	Delete string `toml:"delete"`
}

// NewStore returns a credential store based on the config.
// If cfg is nil, returns a keychain-backed store.
func NewStore(cfg *CommandConfig) Store {
	if cfg != nil {
		return &commandStore{cfg: *cfg}
	}
	return &keychainStore{}
}

// commandStore runs user-configured commands for credential operations.
// {name} in the command string is replaced with the server name.
type commandStore struct {
	cfg CommandConfig
}

func (s *commandStore) Read(name string) (Token, error) {
	if s.cfg.Read == "" {
		return Token{}, fmt.Errorf("no read command configured")
	}
	cmd := expandName(s.cfg.Read, name)
	out, err := runCommand(cmd)
	if err != nil {
		return Token{}, fmt.Errorf("reading credentials for %s: %w", name, err)
	}
	var tok Token
	if err := json.Unmarshal(out, &tok); err != nil {
		return Token{}, fmt.Errorf("parsing credentials for %s: %w", name, err)
	}
	return tok, nil
}

func (s *commandStore) Write(name string, tok Token) error {
	if s.cfg.Write == "" {
		return fmt.Errorf("no write command configured")
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	cmd := expandName(s.cfg.Write, name)
	return runCommandWithStdin(cmd, data)
}

func (s *commandStore) Delete(name string) error {
	if s.cfg.Delete == "" {
		return fmt.Errorf("no delete command configured")
	}
	cmd := expandName(s.cfg.Delete, name)
	_, err := runCommand(cmd)
	return err
}

func expandName(cmdTemplate, name string) string {
	return strings.ReplaceAll(cmdTemplate, "{name}", name)
}

func runCommand(command string) ([]byte, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func runCommandWithStdin(command string, stdin []byte) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(string(stdin))
	return cmd.Run()
}

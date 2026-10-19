package streamhttp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// DefaultSessionIDPath is where a long-lived origin persists its server-wide
// Mcp-Session-Id: $XDG_STATE_HOME/moxy/streamhttp-session-id, falling back to
// ~/.local/state/moxy/streamhttp-session-id.
func DefaultSessionIDPath() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "moxy", "streamhttp-session-id")
}

// loadOrCreateSessionID returns the session id stored at path, minting and
// storing a fresh one when the file is missing or does not hold a UUID. The
// id is the only session state serve-http has (one shared session for every
// client), so persisting it makes an origin restart invisible to clients that
// still carry the previous id. The id is a random UUID, not "accept anything":
// a client must have been issued it.
func loadOrCreateSessionID(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); uuid.Validate(id) == nil {
			return id, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("reading session id %s: %w", path, err)
	}

	id := uuid.New().String()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating session id dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-id-*")
	if err != nil {
		return "", fmt.Errorf("writing session id: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(id + "\n"); err != nil {
		tmp.Close()
		return "", fmt.Errorf("writing session id: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("writing session id: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("storing session id: %w", err)
	}
	return id, nil
}

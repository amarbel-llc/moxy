package mcpclient

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// childStderrTailBytes is how much of a child's stderr a spawn failure
// quotes: enough for a stack trace's head or a config error, not a log.
const childStderrTailBytes = 4096

// childStderrDrainDelay bounds the wait for a dead child's stderr pipe to
// close, which a grandchild that inherited it can hold open.
const childStderrDrainDelay = 2 * time.Second

// stderrTail keeps the last bytes written to it (#37).
type stderrTail struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newStderrTail(maxBytes int) *stderrTail {
	return &stderrTail{max: maxBytes}
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *stderrTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// errorSuffix is what a spawn error appends: the tail, or nothing when the
// child wrote nothing.
func (t *stderrTail) errorSuffix() string {
	text := strings.TrimSpace(t.String())
	if text == "" {
		return ""
	}
	return fmt.Sprintf("; child stderr (last %d bytes at most): %s", t.max, text)
}

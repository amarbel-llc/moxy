package streamhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
)

// heartbeatEnvVar selects the cadence at which handlePost emits keep-alive
// activity on streaming POST responses. Unset uses heartbeatDefault.
// "0", "off", or "" disables heartbeats AND falls back to plain
// application/json responses (legacy behavior). Any other value is
// parsed by time.ParseDuration; an unparseable value falls back to the
// default.
const heartbeatEnvVar = "MOXY_HEARTBEAT_INTERVAL"

const heartbeatDefault = 30 * time.Second

func heartbeatInterval() time.Duration {
	v, set := os.LookupEnv(heartbeatEnvVar)
	if !set {
		return heartbeatDefault
	}
	switch v {
	case "0", "off", "":
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return heartbeatDefault
	}
	return d
}

var (
	streamhttpLoggerOnce sync.Once
	streamhttpLogger     *log.Logger
)

func streamhttpLog(format string, args ...any) {
	streamhttpLoggerOnce.Do(func() {
		logHome := os.Getenv("XDG_LOG_HOME")
		if logHome == "" {
			home, _ := os.UserHomeDir()
			logHome = filepath.Join(home, ".local", "log")
		}
		logDir := filepath.Join(logHome, "moxy")
		_ = os.MkdirAll(logDir, 0o755)
		f, err := os.OpenFile(
			filepath.Join(logDir, "streamhttp.log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY,
			0o644,
		)
		if err == nil {
			streamhttpLogger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
		}
	})
	if streamhttpLogger != nil {
		streamhttpLogger.Printf(format, args...)
	}
}

// extractProgressToken returns the JSON-encoded progressToken from a
// JSON-RPC request's params._meta, or nil if not present. Returns the
// raw token bytes so they can be inlined verbatim — preserving whether
// the client sent a string ("abc123") or an integer (42).
func extractProgressToken(params json.RawMessage) json.RawMessage {
	if len(params) == 0 {
		return nil
	}
	var probe struct {
		Meta struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(params, &probe); err != nil {
		return nil
	}
	tok := probe.Meta.ProgressToken
	if len(tok) == 0 || string(tok) == "null" {
		return nil
	}
	return tok
}

// progressNotificationParams mirrors the MCP spec's
// notifications/progress params shape. ProgressToken is a RawMessage so
// the original JSON encoding (string vs integer) round-trips intact.
type progressNotificationParams struct {
	ProgressToken json.RawMessage `json:"progressToken"`
	Progress      int64           `json:"progress"`
	Message       string          `json:"message,omitempty"`
}

// handlePostStreaming serves a request as text/event-stream and emits
// periodic heartbeats while waiting for the dispatcher to return.
// When params._meta.progressToken is present, each heartbeat is a
// JSON-RPC notifications/progress referencing that token (the MCP
// spec's resetTimeoutOnProgress hook). When absent, heartbeats are
// SSE comment lines that only keep the TCP connection warm.
func (s *Server) handlePostStreaming(
	w http.ResponseWriter,
	r *http.Request,
	msg *jsonrpc.Message,
	interval time.Duration,
	started time.Time,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		streamhttpLog("post end id=%s outcome=error elapsed_ms=%d err=%q",
			idKey(msg), time.Since(started).Milliseconds(), "streaming unsupported")
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	progressToken := extractProgressToken(msg.Params)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	type dispatchResult struct {
		resp *jsonrpc.Message
		err  error
	}
	results := make(chan dispatchResult, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				streamhttpLog("post end id=%s outcome=panic elapsed_ms=%d panic=%v",
					idKey(msg), time.Since(started).Milliseconds(), rec)
				results <- dispatchResult{err: fmt.Errorf("dispatch panicked: %v", rec)}
			}
		}()
		resp, err := s.dispatcher.dispatch(r.Context(), msg)
		results <- dispatchResult{resp: resp, err: err}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var seq int64

	for {
		select {
		case <-r.Context().Done():
			streamhttpLog("post end id=%s outcome=ctx_canceled elapsed_ms=%d transport=sse heartbeats=%d",
				idKey(msg), time.Since(started).Milliseconds(), seq)
			return
		case res := <-results:
			if res.err != nil {
				if errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded) {
					streamhttpLog("post end id=%s outcome=ctx_canceled elapsed_ms=%d transport=sse heartbeats=%d",
						idKey(msg), time.Since(started).Milliseconds(), seq)
					return
				}
				streamhttpLog("post end id=%s outcome=error elapsed_ms=%d transport=sse heartbeats=%d err=%q",
					idKey(msg), time.Since(started).Milliseconds(), seq, res.err.Error())
				errResp := jsonrpc.Message{
					JSONRPC: jsonrpc.Version,
					ID:      msg.ID,
					Error: &jsonrpc.Error{
						Code:    jsonrpc.InternalError,
						Message: res.err.Error(),
					},
				}
				writeSSEData(w, flusher, &errResp)
				return
			}
			streamhttpLog("post end id=%s outcome=response_sent elapsed_ms=%d transport=sse heartbeats=%d",
				idKey(msg), time.Since(started).Milliseconds(), seq)
			if res.resp != nil {
				writeSSEData(w, flusher, res.resp)
			}
			return
		case <-ticker.C:
			seq++
			heartbeatKind := "comment"
			if len(progressToken) > 0 {
				heartbeatKind = "progress"
				notif, nerr := jsonrpc.NewNotification("notifications/progress", progressNotificationParams{
					ProgressToken: progressToken,
					Progress:      seq,
					Message:       "moxy: still waiting",
				})
				if nerr == nil {
					writeSSEData(w, flusher, notif)
				}
			} else {
				fmt.Fprintf(w, ": heartbeat %d\n\n", seq)
				flusher.Flush()
			}
			streamhttpLog("heartbeat id=%s seq=%d kind=%s elapsed_ms=%d",
				idKey(msg), seq, heartbeatKind, time.Since(started).Milliseconds())
		}
	}
}

// writeSSEData encodes a JSON-RPC message as a single SSE data event.
func writeSSEData(w http.ResponseWriter, flusher http.Flusher, msg *jsonrpc.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// idKey returns a printable form of a JSON-RPC message id for logging.
func idKey(msg *jsonrpc.Message) string {
	if msg == nil || msg.ID == nil {
		return "<nil>"
	}
	b, err := json.Marshal(msg.ID)
	if err != nil {
		return "<err>"
	}
	return string(b)
}

package streamhttp

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"

	"github.com/google/uuid"
)

const headerMCPSessionID = "Mcp-Session-Id"

type Options struct {
	Tools         server.ToolProviderV1
	Resources     server.ResourceProviderV1
	Prompts       server.PromptProviderV1
	ServerName    string
	ServerVersion string
	Instructions  string
}

type Server struct {
	dispatcher *dispatcher
	streams    *streamRegistry
	sessionID  string
	mu         sync.RWMutex
}

func New(opts Options) *Server {
	return &Server{
		dispatcher: &dispatcher{
			tools:         opts.Tools,
			resources:     opts.Resources,
			prompts:       opts.Prompts,
			serverName:    opts.ServerName,
			serverVersion: opts.ServerVersion,
			instructions:  opts.Instructions,
		},
		streams: newStreamRegistry(),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodGet:
		s.handleGet(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var msg jsonrpc.Message
	if err := json.Unmarshal(body, &msg); err != nil {
		writeJSONRPCError(w, nil, jsonrpc.ParseError, "parse error")
		return
	}

	if msg.Method == "initialize" {
		s.handleInitializePost(w, r, &msg)
		return
	}

	if !s.validateSession(w, r) {
		return
	}

	if msg.IsNotification() {
		w.WriteHeader(http.StatusAccepted)
		_, _ = s.dispatcher.dispatch(r.Context(), &msg)
		return
	}

	resp, err := s.dispatcher.dispatch(r.Context(), &msg)
	if err != nil {
		writeJSONRPCError(w, msg.ID, jsonrpc.InternalError, err.Error())
		return
	}
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleInitializePost(w http.ResponseWriter, r *http.Request, msg *jsonrpc.Message) {
	s.mu.Lock()
	if s.sessionID == "" {
		s.sessionID = uuid.New().String()
	}
	sid := s.sessionID
	s.mu.Unlock()

	resp, err := s.dispatcher.dispatch(r.Context(), msg)
	if err != nil {
		writeJSONRPCError(w, msg.ID, jsonrpc.InternalError, err.Error())
		return
	}

	w.Header().Set(headerMCPSessionID, sid)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if !s.validateSession(w, r) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	stream := &sseStream{
		id:    uuid.New().String(),
		w:     w,
		flush: flusher,
		done:  make(chan struct{}),
	}
	s.streams.add(stream)
	defer s.streams.remove(stream.id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	select {
	case <-r.Context().Done():
	case <-stream.done:
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.validateSession(w, r) {
		return
	}

	s.streams.closeAll()

	s.mu.Lock()
	s.sessionID = ""
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (s *Server) validateSession(w http.ResponseWriter, r *http.Request) bool {
	s.mu.RLock()
	sid := s.sessionID
	s.mu.RUnlock()

	if sid == "" {
		http.Error(w, "no active session", http.StatusNotFound)
		return false
	}

	clientSID := r.Header.Get(headerMCPSessionID)
	if clientSID == "" {
		http.Error(w, "missing Mcp-Session-Id header", http.StatusBadRequest)
		return false
	}
	if clientSID != sid {
		http.Error(w, "invalid session", http.StatusNotFound)
		return false
	}
	return true
}

// Notify pushes a JSON-RPC message to all open SSE streams.
// This satisfies the notifier signature expected by proxy.SetNotifier.
func (s *Server) Notify(msg *jsonrpc.Message) error {
	s.streams.broadcast(msg)
	return nil
}

func writeJSONRPCError(w http.ResponseWriter, id *jsonrpc.ID, code int, message string) {
	resp := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      id,
		Error: &jsonrpc.Error{
			Code:    code,
			Message: message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

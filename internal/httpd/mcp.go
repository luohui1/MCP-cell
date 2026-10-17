package httpd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/jsonrpc"
)

// The endpoint speaks the session-based Streamable HTTP transport (2025-11-25
// and earlier). A 2026-07-28 client probes first with a request that has no
// session; it gets a plain 400 without a modern error code and, as the spec
// requires, falls back to initialize.
var supportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const (
	maxBody     = 8 << 20
	maxSessions = 512
	sessionIdle = 2 * time.Hour
	// A session whose GET stream dropped and stayed quiet is almost always a
	// client that exited without DELETE (claude -p does this).
	orphanIdle = 10 * time.Minute
	keepalive  = 20 * time.Second
)

func pid() int { return os.Getpid() }

type session struct {
	id, service     string
	client, version string
	created         time.Time

	mu        sync.Mutex
	lastSeen  time.Time
	inflight  int
	stream    chan jsonrpc.Message // the open GET stream, if any
	streamGen int
	hadStream bool
	queue     []jsonrpc.Message
	closed    bool
}

func (ss *session) ID() string { return ss.id }

// Notify queues a message for the GET stream. Never blocks: when the client is
// not reading, the oldest queued messages are dropped.
func (ss *session) Notify(m jsonrpc.Message) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return
	}
	if ss.stream != nil {
		select {
		case ss.stream <- m:
			return
		default:
		}
	}
	if len(ss.queue) >= 64 {
		ss.queue = ss.queue[1:]
	}
	ss.queue = append(ss.queue, m)
}

func (ss *session) touch(delta int) {
	ss.mu.Lock()
	ss.lastSeen = time.Now()
	ss.inflight += delta
	ss.mu.Unlock()
}

// openStream makes ch the session's GET stream, closing any previous one, and
// returns messages queued while no stream was open.
func (ss *session) openStream(ch chan jsonrpc.Message) (queued []jsonrpc.Message, gen int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.stream != nil {
		close(ss.stream)
	}
	ss.stream, ss.streamGen, ss.hadStream = ch, ss.streamGen+1, true
	queued, ss.queue = ss.queue, nil
	return queued, ss.streamGen
}

func (ss *session) closeStream(gen int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.streamGen == gen && ss.stream != nil {
		close(ss.stream)
		ss.stream = nil
	}
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !s.bearerOK(r) {
		writeMessage(w, http.StatusUnauthorized, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "缺少或错误的 MCPcell token"))
		return
	}
	name := r.PathValue("name")
	if !s.pool.Has(name) {
		writeMessage(w, http.StatusNotFound, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "MCPcell 中没有服务 "+name))
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.mcpPost(w, r, name)
	case http.MethodGet:
		s.mcpGet(w, r, name)
	case http.MethodDelete:
		if ss := s.lookup(r, name); ss != nil {
			s.endSession(ss)
			w.WriteHeader(http.StatusOK)
			return
		}
		writeMessage(w, http.StatusNotFound, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "会话不存在"))
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) lookup(r *http.Request, name string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[r.Header.Get("Mcp-Session-Id")]
	if ss == nil || ss.service != name {
		return nil
	}
	return ss
}

func (s *Server) mcpPost(w http.ResponseWriter, r *http.Request, name string) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeMessage(w, http.StatusUnsupportedMediaType, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "Content-Type 必须是 application/json"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeMessage(w, http.StatusRequestEntityTooLarge, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "请求过大"))
		return
	}
	msgs, batch, err := jsonrpc.Parse(body)
	if err != nil {
		writeMessage(w, http.StatusBadRequest, jsonrpc.NewError(nil, jsonrpc.CodeParseError, "无法解析 JSON-RPC"))
		return
	}
	if r.Header.Get("Mcp-Session-Id") == "" {
		if batch || msgs[0].Kind() != jsonrpc.Request || msgs[0].Method != "initialize" {
			id := json.RawMessage(nil)
			if !batch && msgs[0].Kind() == jsonrpc.Request {
				id = msgs[0].ID
			}
			writeMessage(w, http.StatusBadRequest, jsonrpc.NewError(id, jsonrpc.CodeInvalidRequest,
				"缺少 Mcp-Session-Id：此端点使用带会话的 MCP 协议（2025-11-25 及更早），请先 initialize"))
			return
		}
		s.initialize(w, r, name, msgs[0])
		return
	}
	ss := s.lookup(r, name)
	if ss == nil {
		writeMessage(w, http.StatusNotFound, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "会话不存在或已过期，请重新初始化"))
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(supportedVersions, v) {
		writeMessage(w, http.StatusBadRequest, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "不支持的 MCP-Protocol-Version："+v))
		return
	}
	ss.touch(0)
	var reqs, early []jsonrpc.Message
	for _, m := range msgs {
		switch m.Kind() {
		case jsonrpc.Request:
			reqs = append(reqs, m)
		case jsonrpc.Notification:
			s.clientNotification(ss, m)
		case jsonrpc.Response:
			// Answers to server requests; shared instances send none.
		default:
			early = append(early, jsonrpc.NewError(m.ID, jsonrpc.CodeInvalidRequest, "无效的 JSON-RPC 消息"))
		}
	}
	switch {
	case len(reqs) == 0 && len(early) == 0:
		w.WriteHeader(http.StatusAccepted)
	case strings.Contains(r.Header.Get("Accept"), "text/event-stream"):
		s.respondSSE(w, r, ss, reqs, early)
	default:
		s.respondJSON(w, r, ss, reqs, early, batch)
	}
}

func (s *Server) initialize(w http.ResponseWriter, r *http.Request, name string, m jsonrpc.Message) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	inst, err := s.pool.Acquire(ctx, name)
	if err != nil {
		writeMessage(w, http.StatusOK, jsonrpc.NewError(m.ID, jsonrpc.CodeUpstreamUnavailable, "MCP 服务无法启动："+firstLine(err.Error())))
		return
	}
	var p struct {
		ClientInfo struct{ Name, Version string } `json:"clientInfo"`
	}
	_ = json.Unmarshal(m.Params, &p)
	result, version := inst.InitResult()
	ss := &session{id: randomHex(16), service: name, version: version, created: time.Now(), lastSeen: time.Now(),
		client: strings.TrimSpace(p.ClientInfo.Name + " " + p.ClientInfo.Version)}
	s.mu.Lock()
	if len(s.sessions) >= maxSessions {
		s.mu.Unlock()
		writeMessage(w, http.StatusServiceUnavailable, jsonrpc.NewError(m.ID, jsonrpc.CodeInternalError, "会话数已达上限"))
		return
	}
	s.sessions[ss.id] = ss
	s.mu.Unlock()
	inst.Attach(ss)
	s.opts.Log.Info("session opened", "service", name, "client", ss.client, "protocol", version)
	// The upstream negotiated its protocol version once; every session on the
	// shared instance speaks that version.
	w.Header().Set("Mcp-Session-Id", ss.id)
	writeMessage(w, http.StatusOK, jsonrpc.NewResult(m.ID, result))
}

func (s *Server) clientNotification(ss *session, m jsonrpc.Message) {
	if m.Method == "notifications/cancelled" {
		if inst := s.pool.Current(ss.service); inst != nil {
			inst.Forward(ss, m)
		}
	}
}

// handle answers one request. emit carries progress for this request.
func (s *Server) handle(ctx context.Context, ss *session, m jsonrpc.Message, emit func(jsonrpc.Message)) jsonrpc.Message {
	switch m.Method {
	case "ping":
		return jsonrpc.NewResult(m.ID, nil)
	case "initialize":
		return jsonrpc.NewError(m.ID, jsonrpc.CodeInvalidRequest, "会话已初始化")
	case "logging/setLevel":
		// The log level is per-process state; one client must not change it
		// for every other client of a shared instance.
		return jsonrpc.NewResult(m.ID, nil)
	}
	ss.touch(1)
	defer ss.touch(-1)
	inst, err := s.pool.Acquire(ctx, ss.service)
	if err != nil {
		return jsonrpc.NewError(m.ID, jsonrpc.CodeUpstreamUnavailable, "MCP 服务无法启动："+firstLine(err.Error()))
	}
	return inst.Call(ctx, ss, m, emit)
}

func (s *Server) respondSSE(w http.ResponseWriter, r *http.Request, ss *session, reqs, early []jsonrpc.Message) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	_ = rc.Flush()
	var mu sync.Mutex
	write := func(chunk string) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = io.WriteString(w, chunk)
		_ = rc.Flush()
	}
	send := func(m jsonrpc.Message) {
		if b, err := jsonrpc.Encode(m); err == nil {
			write("event: message\ndata: " + string(b) + "\n\n")
		}
	}
	for _, m := range early {
		send(m)
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(keepalive)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				write(": keepalive\n\n")
			}
		}
	}()
	var wg sync.WaitGroup
	for _, m := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			send(s.handle(r.Context(), ss, m, send))
		}()
	}
	wg.Wait()
	close(done)
}

func (s *Server) respondJSON(w http.ResponseWriter, r *http.Request, ss *session, reqs, early []jsonrpc.Message, batch bool) {
	out := make([]jsonrpc.Message, len(reqs))
	var wg sync.WaitGroup
	for n, m := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[n] = s.handle(r.Context(), ss, m, func(jsonrpc.Message) {}) // JSON mode has no channel for progress
		}()
	}
	wg.Wait()
	out = append(early, out...)
	if !batch && len(out) == 1 {
		writeMessage(w, http.StatusOK, out[0])
		return
	}
	parts := make([]string, 0, len(out))
	for _, m := range out {
		if b, err := jsonrpc.Encode(m); err == nil {
			parts = append(parts, string(b))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "["+strings.Join(parts, ",")+"]")
}

func (s *Server) mcpGet(w http.ResponseWriter, r *http.Request, name string) {
	ss := s.lookup(r, name)
	if ss == nil {
		writeMessage(w, http.StatusNotFound, jsonrpc.NewError(nil, jsonrpc.CodeInvalidRequest, "会话不存在或已过期，请重新初始化"))
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	ch := make(chan jsonrpc.Message, 64)
	queued, gen := ss.openStream(ch)
	defer ss.closeStream(gen)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	send := func(m jsonrpc.Message) bool {
		b, err := jsonrpc.Encode(m)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	for _, m := range queued {
		send(m)
	}
	_ = rc.Flush()
	t := time.NewTicker(keepalive)
	defer t.Stop()
	for {
		select {
		case m, ok := <-ch:
			if !ok || !send(m) {
				return // replaced by a newer stream, session closed, or client gone
			}
		case <-t.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil || rc.Flush() != nil {
				return
			}
			ss.touch(0)
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) endSession(ss *session) {
	s.mu.Lock()
	delete(s.sessions, ss.id)
	s.mu.Unlock()
	ss.mu.Lock()
	ss.closed = true
	if ss.stream != nil {
		close(ss.stream)
		ss.stream = nil
	}
	ss.mu.Unlock()
	if inst := s.pool.Current(ss.service); inst != nil {
		inst.Detach(ss)
	}
	s.opts.Log.Info("session closed", "service", ss.service, "client", ss.client)
}

func (s *Server) expireSessions(now time.Time) {
	s.mu.Lock()
	var stale []*session
	for _, ss := range s.sessions {
		ss.mu.Lock()
		idle := now.Sub(ss.lastSeen)
		if ss.inflight == 0 && ss.stream == nil && (idle > sessionIdle || ss.hadStream && idle > orphanIdle) {
			stale = append(stale, ss)
		}
		ss.mu.Unlock()
	}
	s.mu.Unlock()
	for _, ss := range stale {
		s.endSession(ss)
	}
}

func (s *Server) closeAllSessions() {
	s.mu.Lock()
	all := make([]*session, 0, len(s.sessions))
	for _, ss := range s.sessions {
		all = append(all, ss)
	}
	s.mu.Unlock()
	for _, ss := range all {
		s.endSession(ss)
	}
}

type sessionInfo struct {
	ID              string    `json:"id"`
	Service         string    `json:"service"`
	Client          string    `json:"client"`
	ProtocolVersion string    `json:"protocolVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	LastSeenAt      time.Time `json:"lastSeenAt"`
	Inflight        int       `json:"inflight"`
	Streaming       bool      `json:"streaming"`
}

func (s *Server) sessionInfos() []sessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sessionInfo, 0, len(s.sessions))
	for _, ss := range s.sessions {
		ss.mu.Lock()
		out = append(out, sessionInfo{ID: ss.id[:8], Service: ss.service, Client: ss.client,
			ProtocolVersion: ss.version, CreatedAt: ss.created, LastSeenAt: ss.lastSeen,
			Inflight: ss.inflight, Streaming: ss.stream != nil})
		ss.mu.Unlock()
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.Before(out[b].CreatedAt) })
	return out
}

func writeMessage(w http.ResponseWriter, status int, m jsonrpc.Message) {
	b, err := jsonrpc.Encode(m)
	if err != nil {
		b = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"encode"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

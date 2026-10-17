// Package instance runs one upstream MCP server (stdio, legacy handshake) and
// multiplexes many downstream sessions onto it.
//
// Only ownership fields are rewritten: request ids, _meta.progressToken and the
// requestId inside notifications/cancelled. Everything else passes through
// verbatim. A call whose upstream dies mid-flight is reported as result-unknown
// and never replayed.
package instance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/jsonrpc"
	"github.com/luohui1/MCP-cell/internal/proc"
)

// ProtocolVersion is what MCPcell asks legacy upstreams for; they answer with
// the version they actually speak.
const ProtocolVersion = "2025-11-25"

const maxFrame = 16 << 20

// Downstream is one client session as seen by an instance.
type Downstream interface {
	ID() string
	// Notify delivers a server message that belongs to no particular request
	// (list_changed, log messages, resource updates). It must not block.
	Notify(jsonrpc.Message)
}

type Options struct {
	Name        string
	Spec        proc.Spec
	InitTimeout time.Duration
	Version     string // MCPcell version, sent as clientInfo
	Log         *slog.Logger
	// OnListChanged runs synchronously when the upstream announces a
	// */list_changed, before sessions are told, so a cache can be marked stale
	// before clients re-list.
	OnListChanged func(notification string)
}

type Instance struct {
	opts    Options
	p       *proc.Process
	stderr  *ring
	started time.Time
	log     *slog.Logger

	writeMu sync.Mutex

	mu         sync.Mutex
	closed     bool
	nextID     int64
	pending    map[int64]*call
	byDown     map[string]int64 // session + downstream id -> upstream id
	tokens     map[string]*call // rewritten progress token -> call
	attached   map[string]Downstream
	subs       map[string]map[string]Downstream // resource uri -> sessions
	lastActive time.Time
	calls      uint64
	failures   uint64
	badLines   uint64
	stopReason string

	initResult json.RawMessage
	version    string

	done chan struct{}
}

type call struct {
	ds        Downstream // nil for MCPcell's own requests
	origID    json.RawMessage
	origToken json.RawMessage
	upToken   string
	downKey   string
	subURI    string
	events    chan jsonrpc.Message
	resp      chan jsonrpc.Message
}

var (
	ErrClosed      = errors.New("instance closed")
	errDuplicateID = errors.New("duplicate in-flight request id")
)

// Start launches the server and completes the initialize handshake.
func Start(opts Options) (*Instance, error) {
	if opts.InitTimeout == 0 {
		opts.InitTimeout = 90 * time.Second
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	p, err := proc.Start(opts.Spec)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	i := &Instance{
		opts: opts, p: p, stderr: newRing(16 << 10), started: now, lastActive: now,
		log:     opts.Log.With("service", opts.Name, "pid", p.Pid),
		pending: map[int64]*call{}, byDown: map[string]int64{}, tokens: map[string]*call{},
		attached: map[string]Downstream{}, subs: map[string]map[string]Downstream{},
		done: make(chan struct{}),
	}
	go i.drainStderr()
	go i.readLoop()
	if err := i.handshake(); err != nil {
		i.Stop("初始化失败")
		if tail := i.stderr.Tail(4); tail != "" {
			err = fmt.Errorf("%w\nstderr：%s", err, tail)
		}
		return nil, err
	}
	i.log.Info("upstream ready", "protocol", i.version, "argv0", p.Argv[0])
	return i, nil
}

func (i *Instance) handshake() error {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "mcpcell", "version": i.opts.Version},
	}
	req, _ := jsonrpc.NewRequest(json.RawMessage(`0`), "initialize", params)
	c, out, err := i.register(nil, req)
	if err != nil {
		return err
	}
	if err := i.write(out); err != nil {
		return fmt.Errorf("无法写入上游：%w", err)
	}
	var resp jsonrpc.Message
	select {
	case resp = <-c.resp:
	case <-time.After(i.opts.InitTimeout):
		return fmt.Errorf("上游在 %s 内没有完成初始化", i.opts.InitTimeout)
	}
	if len(resp.Error) > 0 {
		return fmt.Errorf("上游拒绝初始化：%s", resp.Error)
	}
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil || res.ProtocolVersion == "" {
		return fmt.Errorf("上游的初始化结果无效")
	}
	i.mu.Lock()
	i.initResult, i.version = resp.Result, res.ProtocolVersion
	i.mu.Unlock()
	note, _ := jsonrpc.NewNotification("notifications/initialized", nil)
	return i.write(note)
}

// register allocates an upstream id and returns the rewritten request.
func (i *Instance) register(ds Downstream, req jsonrpc.Message) (*call, jsonrpc.Message, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil, req, ErrClosed
	}
	up := i.nextID
	i.nextID++
	c := &call{ds: ds, origID: req.ID, events: make(chan jsonrpc.Message, 64), resp: make(chan jsonrpc.Message, 1)}
	upID := json.RawMessage(strconv.FormatInt(up, 10))
	if ds != nil {
		c.downKey = ds.ID() + "\x00" + req.IDKey()
		if _, dup := i.byDown[c.downKey]; dup {
			return nil, req, errDuplicateID
		}
		i.byDown[c.downKey] = up
		i.attached[ds.ID()] = ds
	}
	params := req.Params
	if tok := jsonrpc.ProgressToken(params); tok != nil {
		c.origToken = tok
		c.upToken = `"mcpcell-` + strconv.FormatInt(up, 10) + `"`
		if p, err := jsonrpc.SetProgressToken(params, json.RawMessage(c.upToken)); err == nil {
			params = p
			i.tokens[c.upToken] = c
		}
	}
	if req.Method == "resources/subscribe" {
		c.subURI = jsonrpc.IDKey(jsonrpc.Field(params, "uri"))
	}
	i.pending[up] = c
	i.lastActive = time.Now()
	if ds != nil {
		i.calls++
	}
	return c, jsonrpc.Message{JSONRPC: jsonrpc.Version, ID: upID, Method: req.Method, Params: params}, nil
}

func (i *Instance) unregister(c *call, up int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.pending, up)
	delete(i.byDown, c.downKey)
	delete(i.tokens, c.upToken)
}

// Call forwards a request from ds and waits for its response. onEvent receives
// messages tied to this request (progress) on the caller's goroutine, so a slow
// client never stalls the shared upstream.
func (i *Instance) Call(ctx context.Context, ds Downstream, req jsonrpc.Message, onEvent func(jsonrpc.Message)) jsonrpc.Message {
	if req.Method == "resources/unsubscribe" && i.unsubscribeLocally(ds, req) {
		return jsonrpc.NewResult(req.ID, nil)
	}
	c, out, err := i.register(ds, req)
	switch {
	case errors.Is(err, errDuplicateID):
		return jsonrpc.NewError(req.ID, jsonrpc.CodeInvalidRequest, "同一会话中这个请求 id 仍在处理中")
	case err != nil:
		return jsonrpc.NewError(req.ID, jsonrpc.CodeUpstreamUnavailable, "MCP 服务已停止，请重试")
	}
	if err := i.write(out); err != nil {
		up, _ := strconv.ParseInt(string(out.ID), 10, 64)
		i.unregister(c, up)
		return jsonrpc.NewError(req.ID, jsonrpc.CodeUpstreamUnavailable, "无法写入 MCP 服务")
	}
	for {
		select {
		case ev := <-c.events:
			onEvent(ev)
		case resp := <-c.resp:
			for {
				select {
				case ev := <-c.events:
					onEvent(ev)
					continue
				default:
				}
				return resp
			}
		case <-ctx.Done():
			// The client stopped waiting. A dropped connection is not a
			// cancellation in MCP, so the upstream call runs to completion and
			// its response is discarded when it arrives.
			return jsonrpc.NewError(req.ID, jsonrpc.CodeInternalError, "请求已中断")
		}
	}
}

// Request sends a request on MCPcell's own behalf (catalog refresh) and waits
// for the response. Errors are transport-level; a JSON-RPC error comes back as
// the message.
func (i *Instance) Request(ctx context.Context, method string, params any) (jsonrpc.Message, error) {
	req, err := jsonrpc.NewRequest(json.RawMessage(`0`), method, params)
	if err != nil {
		return req, err
	}
	c, out, err := i.register(nil, req)
	if err != nil {
		return req, err
	}
	if err := i.write(out); err != nil {
		up, _ := strconv.ParseInt(string(out.ID), 10, 64)
		i.unregister(c, up)
		return req, err
	}
	select {
	case resp := <-c.resp:
		return resp, nil
	case <-ctx.Done():
		return req, ctx.Err()
	}
}

// unsubscribeLocally keeps the upstream subscription while other sessions
// still use it; it reports whether the request was fully handled here.
func (i *Instance) unsubscribeLocally(ds Downstream, req jsonrpc.Message) bool {
	uri := jsonrpc.IDKey(jsonrpc.Field(req.Params, "uri"))
	i.mu.Lock()
	defer i.mu.Unlock()
	set := i.subs[uri]
	if set == nil {
		return false
	}
	delete(set, ds.ID())
	if len(set) > 0 {
		return true
	}
	delete(i.subs, uri)
	return false
}

// Forward relays a client notification. Only cancellation is meaningful for a
// shared upstream; initialized, roots/list_changed and the like describe one
// client's state and must not leak to the others.
func (i *Instance) Forward(ds Downstream, m jsonrpc.Message) {
	if m.Method != "notifications/cancelled" {
		return
	}
	key := ds.ID() + "\x00" + jsonrpc.IDKey(jsonrpc.Field(m.Params, "requestId"))
	i.mu.Lock()
	up, ok := i.byDown[key]
	i.mu.Unlock()
	if !ok {
		return
	}
	if params, err := jsonrpc.SetField(m.Params, "requestId", json.RawMessage(strconv.FormatInt(up, 10))); err == nil {
		m.Params = params
		_ = i.write(m)
	}
}

// Attach registers ds for broadcast notifications before its first call.
func (i *Instance) Attach(ds Downstream) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.closed {
		i.attached[ds.ID()] = ds
	}
}

// Detach removes a closed session: its in-flight requests are cancelled
// upstream and subscriptions it alone held are dropped.
func (i *Instance) Detach(ds Downstream) {
	id := ds.ID()
	var cancels []int64
	var unsub []string
	i.mu.Lock()
	delete(i.attached, id)
	for up, c := range i.pending {
		if c.ds != nil && c.ds.ID() == id {
			cancels = append(cancels, up)
			delete(i.byDown, c.downKey)
			c.ds = nil
		}
	}
	for uri, set := range i.subs {
		if _, ok := set[id]; ok {
			delete(set, id)
			if len(set) == 0 {
				delete(i.subs, uri)
				unsub = append(unsub, uri)
			}
		}
	}
	i.mu.Unlock()
	for _, up := range cancels {
		n, _ := jsonrpc.NewNotification("notifications/cancelled", map[string]any{"requestId": up, "reason": "client session closed"})
		_ = i.write(n)
	}
	for _, uri := range unsub {
		req, _ := jsonrpc.NewRequest(nil, "resources/unsubscribe", map[string]json.RawMessage{"uri": json.RawMessage(uri)})
		if _, out, err := i.register(nil, req); err == nil {
			_ = i.write(out)
		}
	}
}

func (i *Instance) write(m jsonrpc.Message) error {
	b, err := jsonrpc.Encode(m)
	if err != nil {
		return err
	}
	i.writeMu.Lock()
	defer i.writeMu.Unlock()
	_, err = i.p.Stdin.Write(append(b, '\n'))
	return err
}

func (i *Instance) readLoop() {
	sc := bufio.NewScanner(i.p.Stdout)
	sc.Buffer(make([]byte, 64<<10), maxFrame)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		msgs, _, err := jsonrpc.Parse(line)
		if err != nil {
			// Some servers print logs to stdout; skip rather than die.
			i.mu.Lock()
			i.badLines++
			i.mu.Unlock()
			continue
		}
		for _, m := range msgs {
			i.dispatch(m)
		}
	}
	if err := sc.Err(); err != nil {
		i.log.Warn("upstream output unreadable", "err", err)
	}
	i.p.Kill()
	i.finish()
}

func (i *Instance) finish() {
	i.mu.Lock()
	i.closed = true
	pending := i.pending
	i.pending, i.byDown, i.tokens = map[int64]*call{}, map[string]int64{}, map[string]*call{}
	reason := i.stopReason
	i.mu.Unlock()
	msg := "MCP 服务进程已退出，调用结果未知，未自动重试"
	if i.p.MemoryLimitHit() {
		msg = "MCP 服务超出内存上限，已被终止；调用结果未知，未自动重试"
	} else if reason != "" {
		msg = "MCP 服务已被停止（" + reason + "），调用结果未知，未自动重试"
	}
	for _, c := range pending {
		c.resp <- jsonrpc.NewError(c.origID, jsonrpc.CodeResultUnknown, msg)
	}
	i.log.Info("upstream exited", "reason", reason, "err", i.p.ExitErr())
	close(i.done)
}

func (i *Instance) dispatch(m jsonrpc.Message) {
	switch m.Kind() {
	case jsonrpc.Response:
		i.onResponse(m)
	case jsonrpc.Notification:
		i.onNotification(m)
	case jsonrpc.Request:
		// Answer off the read loop: writing stdin while the server is blocked
		// writing stdout would deadlock.
		var reply jsonrpc.Message
		if m.Method == "ping" {
			reply = jsonrpc.NewResult(m.ID, nil)
		} else {
			reply = jsonrpc.NewError(m.ID, jsonrpc.CodeMethodNotFound, "MCPcell 共享实例不转发服务端请求 "+m.Method)
		}
		go func() { _ = i.write(reply) }()
	}
}

func (i *Instance) onResponse(m jsonrpc.Message) {
	up, err := strconv.ParseInt(string(m.ID), 10, 64)
	if err != nil {
		return
	}
	i.mu.Lock()
	c := i.pending[up]
	if c == nil {
		i.mu.Unlock()
		return
	}
	delete(i.pending, up)
	delete(i.byDown, c.downKey)
	delete(i.tokens, c.upToken)
	if c.subURI != "" && c.ds != nil && len(m.Error) == 0 {
		if i.subs[c.subURI] == nil {
			i.subs[c.subURI] = map[string]Downstream{}
		}
		i.subs[c.subURI][c.ds.ID()] = c.ds
	}
	i.lastActive = time.Now()
	if len(m.Error) > 0 && c.ds != nil {
		i.failures++
	}
	i.mu.Unlock()
	c.resp <- m.WithID(c.origID)
}

func (i *Instance) onNotification(m jsonrpc.Message) {
	switch m.Method {
	case "notifications/progress":
		key := jsonrpc.IDKey(jsonrpc.Field(m.Params, "progressToken"))
		i.mu.Lock()
		c := i.tokens[key]
		i.mu.Unlock()
		if c == nil {
			return
		}
		params, err := jsonrpc.SetField(m.Params, "progressToken", c.origToken)
		if err != nil {
			return
		}
		m.Params = params
		select {
		case c.events <- m:
		default: // the client is not keeping up; progress is advisory
		}
	case "notifications/resources/updated":
		uri := jsonrpc.IDKey(jsonrpc.Field(m.Params, "uri"))
		for _, ds := range i.targets(i.subs[uri]) {
			ds.Notify(m)
		}
	case "notifications/cancelled":
		// Cancels a server-to-client request; shared instances forward none.
	default:
		if strings.HasSuffix(m.Method, "/list_changed") && i.opts.OnListChanged != nil {
			i.opts.OnListChanged(m.Method)
		}
		// list_changed, log messages and unknown notifications go to every
		// session using this instance.
		for _, ds := range i.targets(i.attached) {
			ds.Notify(m)
		}
	}
}

func (i *Instance) targets(set map[string]Downstream) []Downstream {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]Downstream, 0, len(set))
	for _, ds := range set {
		out = append(out, ds)
	}
	return out
}

func (i *Instance) drainStderr() {
	buf := make([]byte, 4096)
	for {
		n, err := i.p.Stderr.Read(buf)
		if n > 0 {
			i.stderr.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// Stop kills the upstream tree. In-flight calls get result-unknown errors.
func (i *Instance) Stop(reason string) {
	i.mu.Lock()
	if i.stopReason == "" {
		i.stopReason = reason
	}
	i.mu.Unlock()
	i.p.Kill()
	<-i.done
}

func (i *Instance) Done() <-chan struct{} { return i.done }

// MemoryLimitHit reports whether the upstream was killed for exceeding its
// memory limit.
func (i *Instance) MemoryLimitHit() bool { return i.p.MemoryLimitHit() }

func (i *Instance) Alive() bool {
	select {
	case <-i.done:
		return false
	default:
		return true
	}
}

// InitResult is the upstream's initialize result, replayed to each session.
func (i *Instance) InitResult() (json.RawMessage, string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.initResult, i.version
}

// Idle reports whether nothing is in flight, and since when.
func (i *Instance) Idle() (bool, time.Time) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.pending) == 0, i.lastActive
}

func (i *Instance) StopReason() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.stopReason
}

func (i *Instance) StderrTail(lines int) string { return i.stderr.Tail(lines) }

type Snapshot struct {
	PID             int        `json:"pid"`
	ProtocolVersion string     `json:"protocolVersion"`
	ServerName      string     `json:"serverName,omitempty"`
	ServerVersion   string     `json:"serverVersion,omitempty"`
	StartedAt       time.Time  `json:"startedAt"`
	LastActiveAt    time.Time  `json:"lastActiveAt"`
	Inflight        int        `json:"inflight"`
	Sessions        int        `json:"sessions"`
	Calls           uint64     `json:"calls"`
	Failures        uint64     `json:"failures"`
	Usage           proc.Usage `json:"usage"`
}

func (i *Instance) Snapshot() Snapshot {
	usage, _ := i.p.Usage()
	i.mu.Lock()
	defer i.mu.Unlock()
	s := Snapshot{
		PID: i.p.Pid, ProtocolVersion: i.version, StartedAt: i.started, LastActiveAt: i.lastActive,
		Sessions: len(i.attached), Calls: i.calls, Failures: i.failures, Usage: usage,
	}
	for _, c := range i.pending {
		if c.ds != nil {
			s.Inflight++
		}
	}
	var info struct {
		ServerInfo struct{ Name, Version string } `json:"serverInfo"`
	}
	if json.Unmarshal(i.initResult, &info) == nil {
		s.ServerName, s.ServerVersion = info.ServerInfo.Name, info.ServerInfo.Version
	}
	return s
}

// ring keeps the tail of a stream in memory only; stderr can contain secrets
// and is never written to disk.
type ring struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newRing(max int) *ring { return &ring{max: max} }

func (r *ring) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if over := len(r.buf) - r.max; over > 0 {
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
}

func (r *ring) Tail(lines int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := strings.TrimRight(strings.ToValidUTF8(string(r.buf), "?"), "\r\n")
	parts := strings.Split(s, "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

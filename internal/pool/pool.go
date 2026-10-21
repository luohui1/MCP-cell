// Package pool owns the upstream instances for every configured service:
// single-flight start, npx flattening, catalog refresh, idle reaping, the
// instance budget, crash backoff and status. A shared service runs one
// instance for every session; a per-session service runs one per session.
package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/instance"
	"github.com/luohui1/MCP-cell/internal/jsonrpc"
	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
)

const (
	crashWindow     = time.Minute
	crashLimit      = 3
	quarantineDelay = time.Minute
)

var (
	ErrUnknownService = errors.New("unknown service")
	// ErrNeedsSession: a per-session service only runs on a session's behalf.
	ErrNeedsSession = errors.New("按会话运行的服务只在会话调用时启动")
)

// Session is the client session a per-session instance belongs to.
type Session interface {
	instance.Requester
	// InitParams is the client's initialize params, replayed to the
	// handshake of the session's own instance.
	InitParams() json.RawMessage
}

// Spawner is a session that knows how its client would have started the
// server: the client's environment and working directory. A per-session
// instance for such a session starts exactly so, as it would natively.
type Spawner interface {
	SpawnEnv() []string // nil: the service definition's environment
	SpawnDir() string   // "": the service definition's directory
}

type Options struct {
	Version      string
	ParentEnv    []string // the daemon's environment; only the allowlist reaches upstreams
	DefaultDir   string
	InitTimeout  time.Duration
	MaxInstances int // 0 means unlimited
	Log          *slog.Logger
	// Packages enables running `npx pkg` as `node <bin>`; nil keeps npx.
	Packages *npmcache.Cache
	// Catalog enables answering initialize and list requests from cache.
	Catalog *catalog.Store
	// SpecFor overrides how a definition becomes a process (tests).
	SpecFor func(name string, def *registry.Service) proc.Spec
}

type Pool struct {
	opts    Options
	mu      sync.Mutex
	entries map[string]*entry
	closed  bool
	// onListChange is told when a cached list changed, so sessions that never
	// reached the upstream still hear about it.
	onListChange func(service, list string)
}

type entry struct {
	name string
	def  *registry.Service
	// slots holds the running instances: for a shared service one per
	// protocol version its clients ask for (sharedKey + version), for a
	// per-session service one per session id.
	slots map[string]*slot
	// needs is why a shareability check found the service cannot be shared
	// (loaded from the catalog once: checked).
	needs      string
	checked    bool
	checking   map[string]bool // client capabilities being checked
	starts     int
	failures   int       // consecutive failed starts
	failedAt   time.Time // the last failed start
	crashes    []time.Time
	nextTry    time.Time
	lastErr    string
	lastErrAt  time.Time
	lastStderr string
	note       string // non-fatal: why the service runs through npx after all
}

// slot is one instance of a service, or one being started.
type slot struct {
	key    string
	shared bool
	// intro is how the instance introduces itself (see introduction); its
	// text is the catalog profile the instance's lists are cached under.
	intro  json.RawMessage
	inst   *instance.Instance
	op     *startOp
	direct bool // the instance was started as node <bin>
}

func (sl *slot) alive() bool { return sl.inst != nil && sl.inst.Alive() }

// sharedKey starts a shared slot's key; the protocol version follows.
const sharedKey = "\x00"

// requestedVersion is the protocol version a client's initialize asks for.
func requestedVersion(client json.RawMessage) string {
	var v string
	if json.Unmarshal(jsonrpc.Field(client, "protocolVersion"), &v) != nil || v == "" {
		return instance.ProtocolVersion
	}
	return v
}

// capabilityKey is a client's capabilities in canonical form.
func capabilityKey(client json.RawMessage) string {
	var v any
	_ = json.Unmarshal(jsonrpc.Field(client, "capabilities"), &v)
	if v == nil {
		v = map[string]any{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// introduction is what an instance's handshake sends on a client's behalf,
// beyond MCPcell's own clientInfo. A shared instance is introduced with the
// client's protocol version and no capabilities: the shared list only holds
// servers that answer every client alike, and a shareability check confirms
// it. A session's own instance also gets the client's capabilities. Servers
// answer by what they are introduced with, so the text is also the catalog
// profile: clients that would get different answers never share an entry.
func introduction(perSession bool, client json.RawMessage) json.RawMessage {
	v, _ := json.Marshal(requestedVersion(client))
	intro := map[string]json.RawMessage{"protocolVersion": v}
	if perSession {
		intro["capabilities"] = json.RawMessage(capabilityKey(client))
	}
	b, _ := json.Marshal(intro)
	return b
}

func clientOf(ss Session) json.RawMessage {
	if ss == nil {
		return nil
	}
	return ss.InitParams()
}

type startOp struct {
	done chan struct{}
	inst *instance.Instance
	err  error
}

func newEntry(name string, def *registry.Service) *entry {
	return &entry{name: name, def: def, slots: map[string]*slot{}, checking: map[string]bool{}}
}

// perSessionLocked reports whether each session gets an instance of its own:
// configured so, or a shareability check found the server needs it.
func (p *Pool) perSessionLocked(e *entry) bool {
	if e.def.PerSession() {
		return true
	}
	if !e.checked && p.opts.Catalog != nil && e.def.CatalogEnabled() {
		e.checked = true
		e.needs = p.opts.Catalog.NeedsSession(e.name, p.fingerprint(e.def))
	}
	return e.needs != ""
}

// keyLocked picks the slot that serves ss; ss may be nil only for a shared
// service.
func (p *Pool) keyLocked(e *entry, ss Session) string {
	if p.perSessionLocked(e) {
		return ss.ID()
	}
	return sharedKey + requestedVersion(clientOf(ss))
}

// takeAll empties the entry and returns its instances for the caller to stop.
// A start in progress finds its slot gone and stops what it started.
func (e *entry) takeAll() []*instance.Instance {
	var out []*instance.Instance
	for k, sl := range e.slots {
		if sl.inst != nil {
			out = append(out, sl.inst)
		}
		delete(e.slots, k)
	}
	return out
}

func New(cfg *registry.Config, opts Options) *Pool {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	p := &Pool{opts: opts, entries: map[string]*entry{}}
	for _, name := range cfg.Names() {
		e := newEntry(name, cfg.Services[name])
		p.restoreFailureLocked(e)
		p.entries[name] = e
	}
	return p
}

// restoreFailureLocked brings back a failed start recorded before the daemon
// restarted, if the service is still configured the same way.
func (p *Pool) restoreFailureLocked(e *entry) {
	if p.opts.Catalog == nil {
		return
	}
	if f := p.opts.Catalog.Failure(e.name, p.fingerprint(e.def)); f != nil {
		e.failures, e.lastErr, e.lastErrAt, e.failedAt = f.Failures, f.Error, f.At, f.At
	}
}

// saveFailureLocked keeps e's failed-start state across daemon restarts:
// recorded while its starts fail, cleared once one succeeds.
func (p *Pool) saveFailureLocked(e *entry) {
	if p.opts.Catalog == nil {
		return
	}
	var f *catalog.Failure
	if e.failures > 0 {
		f = &catalog.Failure{Error: e.lastErr, At: e.failedAt, Failures: e.failures}
	}
	if err := p.opts.Catalog.SetFailure(e.name, p.fingerprint(e.def), f); err != nil {
		p.opts.Log.Warn("saving start failure", "service", e.name, "err", err)
	}
}

// OnListChange registers the callback for cached lists that changed.
func (p *Pool) OnListChange(fn func(service, list string)) {
	p.mu.Lock()
	p.onListChange = fn
	p.mu.Unlock()
}

func (p *Pool) Has(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.entries[name] != nil
}

// PerSession reports whether the service gives each session its own instance.
func (p *Pool) PerSession(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.entries[name]
	return e != nil && p.perSessionLocked(e)
}

// Current returns the instance serving ss (the shared one for its protocol
// version on a shared service) without starting one.
func (p *Pool) Current(name string, ss Session) *instance.Instance {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.entries[name]
	if e == nil || ss == nil && p.perSessionLocked(e) {
		return nil
	}
	if sl := e.slots[p.keyLocked(e, ss)]; sl != nil && sl.alive() {
		return sl.inst
	}
	return nil
}

// Acquire returns the instance serving ss, starting it if needed: the shared
// instance, or ss's own for a per-session service (ss may be nil only for a
// shared one). Concurrent callers share a single start; a caller giving up
// does not abort the start for the others.
func (p *Pool) Acquire(ctx context.Context, name string, ss Session) (*instance.Instance, error) {
	p.mu.Lock()
	e := p.entries[name]
	switch {
	case e == nil:
		p.mu.Unlock()
		return nil, ErrUnknownService
	case p.closed:
		p.mu.Unlock()
		return nil, errors.New("MCPcell 正在退出")
	case ss == nil && p.perSessionLocked(e):
		p.mu.Unlock()
		return nil, ErrNeedsSession
	}
	key := p.keyLocked(e, ss)
	sl := e.slots[key]
	if sl != nil && sl.alive() {
		inst := sl.inst
		p.mu.Unlock()
		return inst, nil
	}
	if sl == nil {
		shared := strings.HasPrefix(key, sharedKey)
		sl = &slot{key: key, shared: shared, intro: introduction(!shared, clientOf(ss))}
		e.slots[key] = sl
	}
	op := sl.op
	if op == nil {
		fail := func(err error) (*instance.Instance, error) {
			if e.slots[key] == sl && !sl.alive() {
				delete(e.slots, key)
			}
			p.mu.Unlock()
			return nil, err
		}
		if wait := time.Until(e.nextTry); wait > 0 {
			return fail(fmt.Errorf("%s 最近启动失败，%d 秒后再试：%s", name, int(wait.Seconds())+1, e.lastErr))
		}
		victim, err := p.makeRoomLocked()
		if err != nil {
			return fail(err)
		}
		if victim != nil {
			go victim.Stop("为其他实例腾出名额")
		}
		op = &startOp{done: make(chan struct{})}
		sl.op = op
		e.starts++
		go p.start(e, sl, op, ss)
	}
	p.mu.Unlock()
	select {
	case <-op.done:
		return op.inst, op.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// makeRoomLocked enforces MaxInstances: when full it detaches the least
// recently used idle instance (the caller stops it) or refuses. Shared
// instances go first: stopping one costs a cold start, while stopping a
// session's own instance loses that session's state.
func (p *Pool) makeRoomLocked() (*instance.Instance, error) {
	if p.opts.MaxInstances <= 0 {
		return nil, nil
	}
	running := 0
	var lru *slot
	var lruEntry *entry
	var lruAt time.Time
	for _, e := range p.entries {
		for _, sl := range e.slots {
			if sl.op != nil {
				running++
				continue
			}
			if !sl.alive() {
				continue
			}
			running++
			idle, since := sl.inst.Idle()
			if !idle {
				continue
			}
			shared, lruShared := sl.shared, lru != nil && lru.shared
			if lru == nil || shared && !lruShared || shared == lruShared && since.Before(lruAt) {
				lru, lruEntry, lruAt = sl, e, since
			}
		}
	}
	if running < p.opts.MaxInstances {
		return nil, nil
	}
	if lru == nil {
		return nil, fmt.Errorf("已有 %d 个实例在运行且都在处理请求，达到上限（limits.maxInstances）", running)
	}
	delete(lruEntry.slots, lru.key)
	p.opts.Log.Info("evicting idle instance for budget", "service", lruEntry.name, "perSession", !lru.shared)
	return lru.inst, nil
}

func (p *Pool) start(e *entry, sl *slot, op *startOp, ss Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	p.mu.Lock()
	def := e.def
	p.mu.Unlock()
	var spawn Spawner
	if sp, ok := ss.(Spawner); ok && !sl.shared {
		spawn = sp
	}
	spec, direct, note := p.buildSpec(ctx, e.name, def, spawn)
	opts := instance.Options{
		Name: e.name, Spec: spec, Version: p.opts.Version,
		InitTimeout: p.opts.InitTimeout, Log: p.opts.Log,
		OnListChanged: func(n string) { p.listChanged(e, sl, n) },
		Client:        sl.intro,
	}
	if sl.shared {
		opts.OnServerRequest = func(method string) { p.serverAsked(e, def, method) }
	} else {
		// A session's own instance is introduced exactly as its client.
		opts.Owner, opts.Client = ss, ss.InitParams()
	}
	inst, err := instance.Start(opts)
	p.mu.Lock()
	sl.op = nil
	e.note = note
	switch {
	case err == nil && (e.def != def || p.entries[e.name] != e):
		err = errors.New("服务配置刚被修改或删除，请重试")
	case err != nil:
		e.failedLocked(err)
		p.saveFailureLocked(e)
		p.opts.Log.Warn("start failed", "service", e.name, "err", err)
	case p.closed:
		err = errors.New("MCPcell 正在退出")
	case e.slots[sl.key] != sl:
		err = errors.New("实例在启动过程中被停止（会话结束或手动回收）")
	default:
		if e.failures > 0 {
			e.failures = 0
			p.saveFailureLocked(e)
		}
		sl.inst, sl.direct = inst, direct
		go p.watch(e, sl, inst)
		go func() {
			if err := p.refreshCatalog(e.name, def, inst, sl.intro); err != nil {
				p.opts.Log.Warn("catalog fetch failed", "service", e.name, "err", err)
			}
		}()
	}
	if err != nil && e.slots[sl.key] == sl && !sl.alive() {
		delete(e.slots, sl.key)
	}
	p.mu.Unlock()
	if err != nil && inst != nil {
		inst.Stop("MCPcell 退出")
		inst = nil
	}
	op.inst, op.err = inst, err
	close(op.done)
}

// failedLocked records a failed start.
func (e *entry) failedLocked(err error) {
	now := time.Now()
	e.failures++
	e.lastErr, e.lastErrAt, e.failedAt = err.Error(), now, now
	e.nextTry = now.Add(backoff(e.failures))
}

func backoff(failures int) time.Duration {
	steps := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}
	return steps[min(failures, len(steps))-1]
}

// handBack is how long after its last failed start a service is left to the
// wrapper, which runs the command itself. Unlike backoff it costs a wrapped
// session nothing (the agent gets the server as it would natively), so it
// can be long; a server that fails to start every time is then not started
// again for every session.
func handBack(failures int) time.Duration {
	steps := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	return steps[min(failures, len(steps))-1]
}

// Failing reports whether the service's last start failed recently enough
// that a wrapped session should not wait for another try, and why. A
// successful start, a changed definition or a manual stop clears it.
func (p *Pool) Failing(name string) (why string, failing bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.entries[name]
	if e == nil || e.failures == 0 || time.Since(e.failedAt) >= handBack(e.failures) {
		return "", false
	}
	return e.lastErr, true
}

func (p *Pool) watch(e *entry, sl *slot, inst *instance.Instance) {
	<-inst.Done()
	p.mu.Lock()
	defer p.mu.Unlock()
	e.lastStderr = inst.StderrTail(8)
	if sl.inst == inst {
		sl.inst = nil
		if sl.op == nil && e.slots[sl.key] == sl {
			delete(e.slots, sl.key)
		}
	}
	if inst.StopReason() != "" {
		return // stopped on purpose (idle, manual, budget, session end, shutdown)
	}
	now := time.Now()
	e.lastErr, e.lastErrAt = "进程意外退出", now
	if inst.MemoryLimitHit() {
		e.lastErr = fmt.Sprintf("超出内存上限 %d MB，已终止整棵进程树", uint64(e.def.MemoryLimit)>>20)
	}
	recent := e.crashes[:0]
	for _, t := range e.crashes {
		if now.Sub(t) < crashWindow {
			recent = append(recent, t)
		}
	}
	e.crashes = append(recent, now)
	if len(e.crashes) >= crashLimit {
		e.nextTry = now.Add(quarantineDelay)
		e.lastErr = fmt.Sprintf("%s 内崩溃 %d 次，暂停启动", crashWindow, len(e.crashes))
		p.opts.Log.Warn("quarantined", "service", e.name)
	}
}

func (p *Pool) baseSpec(name string, def *registry.Service) proc.Spec {
	if p.opts.SpecFor != nil {
		return p.opts.SpecFor(name, def)
	}
	dir := def.Cwd
	if dir == "" {
		dir = p.opts.DefaultDir
	}
	return proc.Spec{
		Argv: append([]string{def.Command}, def.Args...),
		Env:  proc.BuildEnv(p.opts.ParentEnv, def.Env),
		Dir:  dir,
	}
}

// npxPackage returns the npm spec and server args when def launches a
// registry package through npx and flattening applies.
func (p *Pool) npxPackage(def *registry.Service) (spec string, args []string, ok bool) {
	if p.opts.Packages == nil || p.opts.SpecFor != nil || !def.FlattenEnabled() {
		return "", nil, false
	}
	return npmcache.Detect(proc.UnwrapCmd(append([]string{def.Command}, def.Args...)))
}

// buildSpec turns a definition into a process spec, replacing npx with
// node <bin> from the package cache when possible. If that fails the service
// still runs through npx and note says why.
func (p *Pool) buildSpec(ctx context.Context, name string, def *registry.Service, spawn Spawner) (proc.Spec, bool, string) {
	spec, direct, note := p.launchSpec(ctx, name, def, spawn)
	if def.LeanEnabled() {
		spec = leanSpec(spec)
	}
	return spec, direct, note
}

func (p *Pool) launchSpec(ctx context.Context, name string, def *registry.Service, spawn Spawner) (spec proc.Spec, direct bool, note string) {
	spec = p.baseSpec(name, def)
	if spawn != nil {
		if env := spawn.SpawnEnv(); env != nil {
			spec.Env = env
		}
		if dir := spawn.SpawnDir(); dir != "" {
			spec.Dir = dir
		}
	}
	spec.MemoryLimit = uint64(def.MemoryLimit)
	pkg, args, ok := p.npxPackage(def)
	if !ok {
		return spec, false, ""
	}
	pin, err := p.opts.Packages.Ensure(ctx, pkg, spec.Env)
	if err != nil {
		return spec, false, "npm 包安装失败，暂按原 npx 命令运行：" + firstLine(err.Error())
	}
	node, err := proc.ResolveArgv([]string{"node"}, spec.Env, spec.Dir)
	if err != nil {
		return spec, false, "找不到 node，暂按原 npx 命令运行"
	}
	spec.Argv = append([]string{node[0], pin.Script()}, args...)
	return spec, true, ""
}

// leanNodeFlags shrink a Node.js server's memory without changing what it
// can do: a 1 MB young generation and size-favoring compilation. Measured
// 2026-10-17 on three real servers: 27-38% less private commit, all tools
// still working. --lite-mode and --jitless save more but disable
// WebAssembly, which breaks Node's built-in fetch ("fetch failed").
var leanNodeFlags = []string{"--max-semi-space-size=1", "--optimize-for-size"}

// leanSpec adds leanNodeFlags when the command is node itself; for other
// Node launchers (npx fallback) it uses NODE_OPTIONS, which only accepts the
// semi-space flag.
func leanSpec(spec proc.Spec) proc.Spec {
	argv := proc.UnwrapCmd(spec.Argv)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(argv[0]), filepath.Ext(argv[0])))
	switch {
	case base == "node" && len(argv) > 1 && !strings.HasPrefix(argv[1], "-"):
		spec.Argv = append(append([]string{argv[0]}, leanNodeFlags...), argv[1:]...)
	case base == "npx" || base == "npm" || base == "pnpm" || base == "node":
		spec.Env = append(spec.Env, "NODE_OPTIONS="+leanNodeFlags[0])
	}
	return spec
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// fingerprint identifies what the service would run, without starting or
// installing anything: the definition plus the pinned package version.
func (p *Pool) fingerprint(def *registry.Service) string {
	version := ""
	if pkg, _, ok := p.npxPackage(def); ok {
		if pin := p.opts.Packages.Pinned(pkg); pin != nil {
			version = pin.Version
		}
	}
	return catalog.Fingerprint(def.Command, def.Args, def.Env, def.Cwd, version)
}

// Definition returns the service's definition, or nil.
func (p *Pool) Definition(name string) *registry.Service { return p.definition(name) }

// Definitions returns every service's definition, by name.
func (p *Pool) Definitions() map[string]*registry.Service {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]*registry.Service, len(p.entries))
	for name, e := range p.entries {
		out[name] = e.def
	}
	return out
}

func (p *Pool) definition(name string) *registry.Service {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.entries[name]; e != nil {
		return e.def
	}
	return nil
}

// catalogKey returns the definition and the catalog profile for a client of
// the service, or nil when the service has no catalog.
func (p *Pool) catalogKey(name string, client json.RawMessage) (*registry.Service, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.entries[name]
	if e == nil || p.opts.Catalog == nil || !e.def.CatalogEnabled() {
		return nil, ""
	}
	return e.def, string(introduction(p.perSessionLocked(e), client))
}

// CachedInit returns the cached initialize result for a client whose
// initialize params are client.
func (p *Pool) CachedInit(name string, client json.RawMessage) (result json.RawMessage, version string, ok bool) {
	def, profile := p.catalogKey(name, client)
	if def == nil {
		return nil, "", false
	}
	return p.opts.Catalog.Init(name, p.fingerprint(def), profile)
}

// CachedList returns a cached list result (all pages merged) for a client.
func (p *Pool) CachedList(name, method string, client json.RawMessage) (json.RawMessage, bool) {
	def, profile := p.catalogKey(name, client)
	if def == nil {
		return nil, false
	}
	return p.opts.Catalog.List(name, p.fingerprint(def), profile, method)
}

// Cataloged reports whether the service's catalog holds anything for its
// current configuration.
func (p *Pool) Cataloged(name string) bool {
	def := p.definition(name)
	return def != nil && p.opts.Catalog != nil && def.CatalogEnabled() && p.opts.Catalog.Get(name, p.fingerprint(def)) != nil
}

// refreshCatalog caches what inst reports for the service, for clients that
// inst was introduced as (intro).
func (p *Pool) refreshCatalog(name string, def *registry.Service, inst *instance.Instance, intro json.RawMessage) error {
	if p.opts.Catalog == nil || !def.CatalogEnabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	entry, err := catalog.Fetch(ctx, inst, p.fingerprint(def))
	if err != nil {
		return err
	}
	if jsonrpc.Field(intro, "capabilities") != nil {
		entry.Client = intro
	}
	changed, err := p.opts.Catalog.Put(name, string(intro), entry)
	if err != nil {
		p.opts.Log.Warn("catalog save failed", "service", name, "err", err)
	}
	p.mu.Lock()
	notify := p.onListChange
	p.mu.Unlock()
	if notify != nil {
		for _, list := range changed {
			notify(name, list)
		}
	}
	return nil
}

// listChanged runs before the upstream's notification reaches sessions: stop
// serving the cached list, then refetch it from the instance that announced it.
func (p *Pool) listChanged(e *entry, sl *slot, notification string) {
	if p.opts.Catalog == nil {
		return
	}
	p.opts.Catalog.MarkStale(e.name, catalog.ListFor(notification)...)
	go func() {
		p.mu.Lock()
		inst, def := sl.inst, e.def
		p.mu.Unlock()
		if inst != nil && inst.Alive() {
			_ = p.refreshCatalog(e.name, def, inst, sl.intro)
		}
	}()
}

// Warm starts the service on MCPcell's own behalf to cache its catalog, then
// stops it. A per-session service is started once as each kind of client
// seen recently would start it (at most three), so their sessions connect
// from the catalog; with none seen yet, and for a shared service, once as
// MCPcell.
func (p *Pool) Warm(ctx context.Context, name string) error {
	p.mu.Lock()
	e := p.entries[name]
	if e == nil {
		p.mu.Unlock()
		return ErrUnknownService
	}
	def, perSession := e.def, p.perSessionLocked(e)
	p.mu.Unlock()
	return p.warmAll(ctx, name, def, perSession)
}

// Try starts a service before it is added, the way Warm would once added: it
// tells whether MCPcell can run the server at all before an agent's entry is
// pointed at it. The catalog it fetches is kept for the service's name.
func (p *Pool) Try(ctx context.Context, name string, def *registry.Service) error {
	return p.warmAll(ctx, name, def, def.PerSession())
}

func (p *Pool) warmAll(ctx context.Context, name string, def *registry.Service, perSession bool) error {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return errors.New("MCPcell 正在退出")
	}
	clients := []json.RawMessage{nil}
	if seen := p.opts.Catalog; perSession && seen != nil {
		if recent := seen.Clients(3); len(recent) > 0 {
			clients = recent
		}
	}
	for _, client := range clients {
		if err := p.warm(ctx, name, def, introduction(perSession, client)); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pool) warm(ctx context.Context, name string, def *registry.Service, intro json.RawMessage) error {
	spec, _, note := p.buildSpec(ctx, name, def, nil)
	inst, err := instance.Start(instance.Options{Name: name, Spec: spec, Version: p.opts.Version,
		InitTimeout: p.opts.InitTimeout, Log: p.opts.Log, Client: intro})
	p.mu.Lock()
	if e := p.entries[name]; e != nil && e.def == def {
		e.note = note
		if err != nil {
			e.failedLocked(err)
			p.saveFailureLocked(e)
		} else if e.failures > 0 {
			e.failures, e.nextTry = 0, time.Time{}
			p.saveFailureLocked(e)
		}
	}
	p.mu.Unlock()
	if err != nil {
		return err
	}
	defer inst.Stop("预热完成")
	return p.refreshCatalog(name, def, inst, intro)
}

// checkWait is how long a shareability check waits after the handshake and
// lists for the server to ask the client something (servers ask for roots
// right after initialized).
const checkWait = 2 * time.Second

// CheckShared makes sure a shared service serves ss's kind of client as that
// client's own instance would. The first time a client whose capabilities the
// service has not been checked for connects, an instance is started in the
// background, introduced exactly as that client. If the server asks the
// client anything (roots, sampling, elicitation), or its initialize or lists
// differ from what the shared instance reported, the service needs sessions:
// every later session gets an instance of its own. Results are kept with the
// catalog, so each kind of client is checked once per configuration.
func (p *Pool) CheckShared(name string, ss Session) {
	client := clientOf(ss)
	caps := capabilityKey(client)
	p.mu.Lock()
	e := p.entries[name]
	if e == nil || p.closed || p.opts.Catalog == nil || !e.def.CatalogEnabled() || p.perSessionLocked(e) ||
		caps == "{}" || e.checking[caps] {
		p.mu.Unlock()
		return // no capabilities: introduced exactly as the shared instance is
	}
	def := e.def
	fp := p.fingerprint(def)
	if _, done := p.opts.Catalog.Check(name, fp, caps); done {
		p.mu.Unlock()
		return
	}
	e.checking[caps] = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(e.checking, caps)
		p.mu.Unlock()
	}()
	reason, err := p.check(name, def, fp, client)
	if err != nil {
		p.opts.Log.Warn("shareability check failed", "service", name, "err", firstLine(err.Error()))
		return // not recorded: the next such session tries again
	}
	if err := p.opts.Catalog.SetCheck(name, fp, caps, reason); err != nil {
		p.opts.Log.Warn("catalog save failed", "service", name, "err", err)
	}
	if reason == "" {
		p.opts.Log.Info("shareability checked", "service", name, "capabilities", caps)
		return
	}
	p.mu.Lock()
	if p.entries[name] == e && e.def == def {
		e.checked, e.needs = true, reason
	}
	p.mu.Unlock()
	p.opts.Log.Warn("shared service needs an instance per session", "service", name, "reason", reason)
}

// serverAsked records that a shared instance's server sent its client a
// request during a call, which the shareability check cannot see coming:
// later sessions get instances of their own.
func (p *Pool) serverAsked(e *entry, def *registry.Service, method string) {
	reason := "调用中向客户端发了 " + method + " 请求"
	p.mu.Lock()
	if p.entries[e.name] != e || e.def != def || e.needs != "" {
		p.mu.Unlock()
		return
	}
	e.checked, e.needs = true, reason
	fp := p.fingerprint(def)
	p.mu.Unlock()
	if p.opts.Catalog != nil && def.CatalogEnabled() {
		if err := p.opts.Catalog.SetCheck(e.name, fp, "server-request", reason); err != nil {
			p.opts.Log.Warn("catalog save failed", "service", e.name, "err", err)
		}
	}
	p.opts.Log.Warn("shared service needs an instance per session", "service", e.name, "reason", reason)
}

// asker stands in for a client during a shareability check: it notes what
// the server asks and declines.
type asker struct {
	mu      sync.Mutex
	methods []string
}

func (a *asker) ID() string             { return "shareability-check" }
func (a *asker) Notify(jsonrpc.Message) {}
func (a *asker) Request(_ context.Context, m jsonrpc.Message) (jsonrpc.Message, error) {
	a.mu.Lock()
	a.methods = append(a.methods, m.Method)
	a.mu.Unlock()
	return jsonrpc.NewError(m.ID, jsonrpc.CodeMethodNotFound, "MCPcell 共享检测不应答"), nil
}

// check runs one shareability check; reason is "" when the server answered
// the client as it answers the shared instance.
func (p *Pool) check(name string, def *registry.Service, fp string, client json.RawMessage) (reason string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	spec, _, _ := p.buildSpec(ctx, name, def, nil)
	a := &asker{}
	inst, err := instance.Start(instance.Options{Name: name, Spec: spec, Version: p.opts.Version,
		InitTimeout: p.opts.InitTimeout, Log: p.opts.Log, Client: client, Owner: a})
	if err != nil {
		return "", err
	}
	defer inst.Stop("共享检测完成")
	got, err := catalog.Fetch(ctx, inst, fp)
	if err != nil {
		return "", err
	}
	select {
	case <-time.After(checkWait):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	a.mu.Lock()
	asked := distinct(a.methods)
	a.mu.Unlock()
	if len(asked) > 0 {
		return "会向客户端发 " + strings.Join(asked, "、") + " 请求", nil
	}
	profile := string(introduction(false, client))
	var sharedInit json.RawMessage
	for wait := 0; ; wait++ { // the shared instance may still be caching its catalog
		var ok bool
		if sharedInit, _, ok = p.opts.Catalog.Init(name, fp, profile); ok || wait == 30 {
			if !ok {
				return "", errors.New("共享实例的能力目录还没有缓存")
			}
			break
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	for _, k := range []string{"capabilities", "instructions"} {
		if !sameJSON(jsonrpc.Field(got.InitResult, k), jsonrpc.Field(sharedInit, k)) {
			return "初始化结果随客户端能力变化（" + k + "）", nil
		}
	}
	for method := range catalog.Lists {
		mine, ok := got.Lists[method]
		if !ok {
			continue // not offered to this client: the capabilities differ already
		}
		theirs, ok := p.opts.Catalog.List(name, fp, profile, method)
		if !ok {
			return "", errors.New("共享实例的 " + method + " 还没有缓存")
		}
		if !sameJSON(mine, theirs) {
			return method + " 随客户端能力变化", nil
		}
	}
	return "", nil
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	return reflect.DeepEqual(x, y)
}

// distinct returns the distinct values of list in first-seen order.
func distinct(list []string) []string {
	var out []string
	for _, s := range list {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Put adds a service or replaces its definition. A replaced service's running
// instances are stopped, so the next call starts with the new definition.
func (p *Pool) Put(name string, def *registry.Service) {
	p.mu.Lock()
	e := p.entries[name]
	var old []*instance.Instance
	if e == nil {
		e = newEntry(name, def)
		p.restoreFailureLocked(e)
		p.entries[name] = e
	} else {
		old = e.takeAll()
		e.failures = 0
		p.saveFailureLocked(e) // changed settings deserve a fresh try
		e.def = def
		e.nextTry, e.crashes, e.lastErr, e.lastErrAt = time.Time{}, nil, "", time.Time{}
		e.note, e.checked, e.needs = "", false, ""
	}
	p.mu.Unlock()
	stopAll(old, "配置已修改")
}

// Remove deletes a service, stopping its instances.
func (p *Pool) Remove(name string) bool {
	p.mu.Lock()
	e := p.entries[name]
	delete(p.entries, name)
	var old []*instance.Instance
	if e != nil {
		old = e.takeAll()
		e.failures = 0
		p.saveFailureLocked(e)
	}
	p.mu.Unlock()
	stopAll(old, "服务已删除")
	return e != nil
}

// Stop ends the service's instances. The next request starts a fresh one, so
// this also clears a crash quarantine.
func (p *Pool) Stop(name, reason string) bool {
	p.mu.Lock()
	e := p.entries[name]
	var old []*instance.Instance
	if e != nil {
		old = e.takeAll()
		failed := e.failures > 0
		e.nextTry, e.crashes, e.failures = time.Time{}, nil, 0
		e.checked = false // an update changes the fingerprint the checks belong to
		if failed {
			p.saveFailureLocked(e)
		}
	}
	p.mu.Unlock()
	stopAll(old, reason)
	return len(old) > 0
}

// Release ends ss's use of the service when the session ends: the instance
// it had of its own stops, and the shared one it used forgets it. A session
// can have both when a shareability check moved the service to per-session
// instances while it was open.
func (p *Pool) Release(name string, ss Session) {
	p.mu.Lock()
	e := p.entries[name]
	if e == nil {
		p.mu.Unlock()
		return
	}
	own := e.slots[ss.ID()]
	delete(e.slots, ss.ID())
	shared := e.slots[sharedKey+requestedVersion(ss.InitParams())]
	p.mu.Unlock()
	if own != nil && own.inst != nil {
		own.inst.Stop("会话结束")
	}
	if shared != nil && shared.inst != nil {
		shared.inst.Detach(ss)
	}
}

func stopAll(insts []*instance.Instance, reason string) {
	var wg sync.WaitGroup
	for _, inst := range insts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst.Stop(reason)
		}()
	}
	wg.Wait()
}

// Update reinstalls an npx service's package at the newest version its spec
// allows, then starts it once so the catalog reflects the new version.
func (p *Pool) Update(ctx context.Context, name string) (now, before *npmcache.Pin, err error) {
	def := p.definition(name)
	if def == nil {
		return nil, nil, ErrUnknownService
	}
	pkg, _, ok := p.npxPackage(def)
	if !ok {
		return nil, nil, errors.New("这个服务不是通过 npx 启动的 npm 包，或已关闭 flatten")
	}
	p.Stop(name, "更新版本")
	spec := p.baseSpec(name, def)
	if now, before, err = p.opts.Packages.Update(ctx, pkg, spec.Env); err != nil {
		return nil, nil, err
	}
	if def.PerSession() {
		err = p.Warm(ctx, name)
	} else {
		_, err = p.Acquire(ctx, name, nil)
	}
	if err != nil {
		return now, before, fmt.Errorf("已更新到 %s，但启动失败：%w", now.Version, err)
	}
	return now, before, nil
}

// Reap stops instances idle for longer than their service's idleTimeout.
func (p *Pool) Reap(now time.Time) {
	p.mu.Lock()
	var victims []*instance.Instance
	for _, e := range p.entries {
		limit := e.def.Idle()
		if limit <= 0 {
			continue
		}
		for k, sl := range e.slots {
			if sl.op != nil || !sl.alive() {
				continue
			}
			if idle, since := sl.inst.Idle(); idle && now.Sub(since) >= limit {
				victims = append(victims, sl.inst)
				delete(e.slots, k)
			}
		}
	}
	p.mu.Unlock()
	stopAll(victims, "空闲回收")
}

// Run reaps idle instances until ctx ends.
func (p *Pool) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			p.Reap(now)
		}
	}
}

// Close stops every instance and refuses new starts.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	var all []*instance.Instance
	for _, e := range p.entries {
		all = append(all, e.takeAll()...)
	}
	p.mu.Unlock()
	stopAll(all, "MCPcell 退出")
}

type PackageInfo struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
}

type CatalogInfo struct {
	Tools     int       `json:"tools"`
	Prompts   int       `json:"prompts"`
	Resources int       `json:"resources"`
	FetchedAt time.Time `json:"fetchedAt"`
	// Description is what the server says it is for (catalog.Entry.Description).
	Description string   `json:"description,omitempty"`
	ToolNames   []string `json:"toolNames,omitempty"`
}

// SessionInstance is a per-session service's instance for one session.
type SessionInstance struct {
	Session string `json:"session"` // the first 8 characters of the session id
	instance.Snapshot
}

type ServiceStatus struct {
	Name           string             `json:"name"`
	Mode           string             `json:"mode"`
	State          string             `json:"state"` // cold | starting | ready | failed
	Command        string             `json:"command"`
	EnvNames       []string           `json:"envNames,omitempty"`
	IdleTimeoutSec int                `json:"idleTimeoutSec"`
	MemoryLimit    uint64             `json:"memoryLimit,omitempty"`
	Starts         int                `json:"starts"`
	Failures       int                `json:"failures,omitempty"` // consecutive failed starts; 0 once one succeeds
	Direct         bool               `json:"direct"`             // running as node <bin>, without npx
	Lean           bool               `json:"lean"`               // memory-saving node flags enabled
	Package        *PackageInfo       `json:"package,omitempty"`
	Catalog        *CatalogInfo       `json:"catalog,omitempty"`
	Note           string             `json:"note,omitempty"`
	LastError      string             `json:"lastError,omitempty"`
	LastErrorAt    *time.Time         `json:"lastErrorAt,omitempty"`
	RetryAt        *time.Time         `json:"retryAt,omitempty"`
	StderrTail     string             `json:"stderrTail,omitempty"`
	Instance       *instance.Snapshot `json:"instance,omitempty"`  // a shared service's instance
	Instances      []SessionInstance  `json:"instances,omitempty"` // a per-session service's instances
}

func (p *Pool) Status() []ServiceStatus {
	p.mu.Lock()
	type running struct {
		sl   *slot
		inst *instance.Instance
	}
	type item struct {
		st   ServiceStatus
		def  *registry.Service
		runs []running
	}
	items := make([]item, 0, len(p.entries))
	now := time.Now()
	for _, e := range p.entries {
		st := ServiceStatus{
			Name: e.name, Mode: e.def.Mode, State: "cold",
			Command:        DisplayCommand(e.def),
			IdleTimeoutSec: int(e.def.Idle().Seconds()),
			MemoryLimit:    uint64(e.def.MemoryLimit),
			Lean:           e.def.LeanEnabled(),
			Starts:         e.starts, Failures: e.failures, Note: e.note, LastError: e.lastErr, StderrTail: e.lastStderr,
		}
		if !e.def.PerSession() && p.perSessionLocked(e) {
			st.Mode = registry.ModePerSession
			st.Note = strings.TrimSpace("共享检测发现它" + e.needs + "，已自动改为按会话运行。" + st.Note)
		}
		for k := range e.def.Env {
			st.EnvNames = append(st.EnvNames, k)
		}
		sort.Strings(st.EnvNames)
		if !e.lastErrAt.IsZero() {
			t := e.lastErrAt
			st.LastErrorAt = &t
		}
		var runs []running
		starting := false
		for _, sl := range e.slots {
			starting = starting || sl.op != nil
			if sl.alive() {
				runs = append(runs, running{sl, sl.inst})
				st.Direct = st.Direct || sl.direct
			}
		}
		switch {
		case len(runs) > 0:
			st.State = "ready"
		case starting:
			st.State = "starting"
		case e.nextTry.After(now):
			st.State = "failed"
			t := e.nextTry
			st.RetryAt = &t
		}
		items = append(items, item{st, e.def, runs})
	}
	p.mu.Unlock()
	out := make([]ServiceStatus, 0, len(items))
	for _, it := range items {
		for _, r := range it.runs {
			snap := r.inst.Snapshot() // syscalls; outside the pool lock
			switch {
			case r.sl.shared && it.st.Instance == nil:
				it.st.Instance = &snap
			case r.sl.shared:
				// Another protocol version's shared instance.
				it.st.Instances = append(it.st.Instances, SessionInstance{Session: strings.TrimPrefix(r.sl.key, sharedKey), Snapshot: snap})
			default:
				it.st.Instances = append(it.st.Instances, SessionInstance{Session: r.sl.key[:min(8, len(r.sl.key))], Snapshot: snap})
			}
		}
		sort.Slice(it.st.Instances, func(a, b int) bool { return it.st.Instances[a].StartedAt.Before(it.st.Instances[b].StartedAt) })
		if pkg, _, ok := p.npxPackage(it.def); ok {
			if pin := p.opts.Packages.Pinned(pkg); pin != nil {
				it.st.Package = &PackageInfo{Name: pin.Name, Version: pin.Version, InstalledAt: pin.InstalledAt}
			}
		}
		if p.opts.Catalog != nil && it.def.CatalogEnabled() {
			if e := p.opts.Catalog.Get(it.st.Name, p.fingerprint(it.def)); e != nil {
				it.st.Catalog = &CatalogInfo{Tools: max(e.Count("tools/list"), 0), Prompts: max(e.Count("prompts/list"), 0),
					Resources: max(e.Count("resources/list"), 0), FetchedAt: e.FetchedAt,
					Description: e.Description(), ToolNames: e.Names("tools/list")}
			}
		}
		out = append(out, it.st)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

var secretFlag = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[-_]?key|apikey|auth|credential|key)`)

// DisplayCommand shows a definition's command line with likely secrets masked.
func DisplayCommand(def *registry.Service) string {
	parts := []string{def.Command}
	maskNext := false
	for _, a := range def.Args {
		switch {
		case maskNext:
			a, maskNext = "***", false
		case strings.HasPrefix(a, "-") && strings.Contains(a, "="):
			if k, _, _ := strings.Cut(a, "="); secretFlag.MatchString(k) {
				a = k + "=***"
			}
		case strings.HasPrefix(a, "-") && secretFlag.MatchString(a):
			maskNext = true
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

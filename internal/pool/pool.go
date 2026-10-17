// Package pool owns the upstream instances for every configured service:
// single-flight start, npx flattening, catalog refresh, idle reaping, the
// instance budget, crash backoff and status.
package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/instance"
	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
)

const (
	crashWindow     = time.Minute
	crashLimit      = 3
	quarantineDelay = time.Minute
)

var ErrUnknownService = errors.New("unknown service")

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
	name       string
	def        *registry.Service
	inst       *instance.Instance
	op         *startOp
	starts     int
	failures   int // consecutive failed starts
	crashes    []time.Time
	nextTry    time.Time
	lastErr    string
	lastErrAt  time.Time
	lastStderr string
	note       string // non-fatal: why the service runs through npx after all
	direct     bool   // the running instance was started as node <bin>
}

type startOp struct {
	done chan struct{}
	inst *instance.Instance
	err  error
}

func New(cfg *registry.Config, opts Options) *Pool {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	p := &Pool{opts: opts, entries: map[string]*entry{}}
	for _, name := range cfg.Names() {
		p.entries[name] = &entry{name: name, def: cfg.Services[name]}
	}
	return p
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

// Current returns the running instance without starting one.
func (p *Pool) Current(name string) *instance.Instance {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.entries[name]; e != nil && e.inst != nil && e.inst.Alive() {
		return e.inst
	}
	return nil
}

// Acquire returns the service's running instance, starting it if needed.
// Concurrent callers share a single start; a caller giving up does not abort
// the start for the others.
func (p *Pool) Acquire(ctx context.Context, name string) (*instance.Instance, error) {
	p.mu.Lock()
	e := p.entries[name]
	switch {
	case e == nil:
		p.mu.Unlock()
		return nil, ErrUnknownService
	case p.closed:
		p.mu.Unlock()
		return nil, errors.New("MCPcell 正在退出")
	case e.inst != nil && e.inst.Alive():
		inst := e.inst
		p.mu.Unlock()
		return inst, nil
	}
	op := e.op
	if op == nil {
		if wait := time.Until(e.nextTry); wait > 0 {
			err := fmt.Errorf("%s 最近启动失败，%d 秒后再试：%s", name, int(wait.Seconds())+1, e.lastErr)
			p.mu.Unlock()
			return nil, err
		}
		victim, err := p.makeRoomLocked()
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if victim != nil {
			go victim.Stop("为其他服务腾出实例名额")
		}
		op = &startOp{done: make(chan struct{})}
		e.op = op
		e.starts++
		go p.start(e, op)
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
// recently used idle instance (the caller stops it) or refuses.
func (p *Pool) makeRoomLocked() (*instance.Instance, error) {
	if p.opts.MaxInstances <= 0 {
		return nil, nil
	}
	running := 0
	var lru *entry
	var lruAt time.Time
	for _, e := range p.entries {
		if e.op != nil {
			running++
			continue
		}
		if e.inst == nil || !e.inst.Alive() {
			continue
		}
		running++
		if idle, since := e.inst.Idle(); idle && (lru == nil || since.Before(lruAt)) {
			lru, lruAt = e, since
		}
	}
	if running < p.opts.MaxInstances {
		return nil, nil
	}
	if lru == nil {
		return nil, fmt.Errorf("已有 %d 个实例在运行且都在处理请求，达到上限（limits.maxInstances）", running)
	}
	victim := lru.inst
	lru.inst = nil
	p.opts.Log.Info("evicting idle instance for budget", "service", lru.name)
	return victim, nil
}

func (p *Pool) start(e *entry, op *startOp) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	p.mu.Lock()
	def := e.def
	p.mu.Unlock()
	spec, direct, note := p.buildSpec(ctx, e.name, def)
	inst, err := instance.Start(instance.Options{
		Name: e.name, Spec: spec, Version: p.opts.Version,
		InitTimeout: p.opts.InitTimeout, Log: p.opts.Log,
		OnListChanged: func(n string) { p.listChanged(e, n) },
	})
	p.mu.Lock()
	e.op = nil
	e.note = note
	switch {
	case err == nil && (e.def != def || p.entries[e.name] != e):
		err = errors.New("服务配置刚被修改或删除，请重试")
	case err != nil:
		e.failures++
		e.lastErr, e.lastErrAt = err.Error(), time.Now()
		e.nextTry = time.Now().Add(backoff(e.failures))
		p.opts.Log.Warn("start failed", "service", e.name, "err", err)
	case p.closed:
		err = errors.New("MCPcell 正在退出")
	default:
		e.failures = 0
		e.inst, e.direct = inst, direct
		go p.watch(e, inst)
		go p.refreshCatalog(e, inst)
	}
	p.mu.Unlock()
	if err != nil && inst != nil {
		inst.Stop("MCPcell 退出")
		inst = nil
	}
	op.inst, op.err = inst, err
	close(op.done)
}

func backoff(failures int) time.Duration {
	steps := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}
	return steps[min(failures, len(steps))-1]
}

func (p *Pool) watch(e *entry, inst *instance.Instance) {
	<-inst.Done()
	p.mu.Lock()
	defer p.mu.Unlock()
	e.lastStderr = inst.StderrTail(8)
	if e.inst == inst {
		e.inst = nil
	}
	if inst.StopReason() != "" {
		return // stopped on purpose (idle, manual, budget, shutdown)
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
func (p *Pool) buildSpec(ctx context.Context, name string, def *registry.Service) (spec proc.Spec, direct bool, note string) {
	spec = p.baseSpec(name, def)
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

func (p *Pool) definition(name string) *registry.Service {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.entries[name]; e != nil {
		return e.def
	}
	return nil
}

// CachedInit returns the cached initialize result for a service.
func (p *Pool) CachedInit(name string) (result json.RawMessage, version string, ok bool) {
	def := p.definition(name)
	if def == nil || p.opts.Catalog == nil || !def.CatalogEnabled() {
		return nil, "", false
	}
	return p.opts.Catalog.Init(name, p.fingerprint(def))
}

// CachedList returns a cached list result (all pages merged).
func (p *Pool) CachedList(name, method string) (json.RawMessage, bool) {
	def := p.definition(name)
	if def == nil || p.opts.Catalog == nil || !def.CatalogEnabled() {
		return nil, false
	}
	return p.opts.Catalog.List(name, p.fingerprint(def), method)
}

func (p *Pool) refreshCatalog(e *entry, inst *instance.Instance) {
	if p.opts.Catalog == nil || !e.def.CatalogEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	entry, err := catalog.Fetch(ctx, inst, p.fingerprint(e.def))
	if err != nil {
		p.opts.Log.Warn("catalog fetch failed", "service", e.name, "err", err)
		return
	}
	changed, err := p.opts.Catalog.Put(e.name, entry)
	if err != nil {
		p.opts.Log.Warn("catalog save failed", "service", e.name, "err", err)
	}
	p.mu.Lock()
	notify := p.onListChange
	p.mu.Unlock()
	if notify != nil {
		for _, list := range changed {
			notify(e.name, list)
		}
	}
}

// listChanged runs before the upstream's notification reaches sessions:
// stop serving the cached list, then refetch it.
func (p *Pool) listChanged(e *entry, notification string) {
	if p.opts.Catalog == nil {
		return
	}
	p.opts.Catalog.MarkStale(e.name, catalog.ListFor(notification)...)
	go func() {
		if inst := p.Current(e.name); inst != nil {
			p.refreshCatalog(e, inst)
		}
	}()
}

// Put adds a service or replaces its definition. A replaced service's running
// instance is stopped, so the next call starts with the new definition.
func (p *Pool) Put(name string, def *registry.Service) {
	p.mu.Lock()
	e := p.entries[name]
	var old *instance.Instance
	if e == nil {
		p.entries[name] = &entry{name: name, def: def}
	} else {
		old, e.inst = e.inst, nil
		e.def = def
		e.nextTry, e.crashes, e.failures, e.note, e.lastErr, e.lastErrAt = time.Time{}, nil, 0, "", "", time.Time{}
	}
	p.mu.Unlock()
	if old != nil {
		old.Stop("配置已修改")
	}
}

// Remove deletes a service, stopping its instance.
func (p *Pool) Remove(name string) bool {
	p.mu.Lock()
	e := p.entries[name]
	delete(p.entries, name)
	var inst *instance.Instance
	if e != nil {
		inst = e.inst
	}
	p.mu.Unlock()
	if inst != nil {
		inst.Stop("服务已删除")
	}
	return e != nil
}

// Stop ends the service's instance. The next request starts a fresh one, so
// this also clears a crash quarantine.
func (p *Pool) Stop(name, reason string) bool {
	p.mu.Lock()
	e := p.entries[name]
	var inst *instance.Instance
	if e != nil {
		inst = e.inst
		e.nextTry, e.crashes, e.failures = time.Time{}, nil, 0
	}
	p.mu.Unlock()
	if inst == nil {
		return false
	}
	inst.Stop(reason)
	return true
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
	if _, err := p.Acquire(ctx, name); err != nil {
		return now, before, fmt.Errorf("已更新到 %s，但启动失败：%w", now.Version, err)
	}
	return now, before, nil
}

// Reap stops instances idle for longer than their service's idleTimeout.
func (p *Pool) Reap(now time.Time) {
	p.mu.Lock()
	var victims []*instance.Instance
	for _, e := range p.entries {
		if e.inst == nil || e.def.Idle() <= 0 {
			continue
		}
		if idle, since := e.inst.Idle(); idle && now.Sub(since) >= e.def.Idle() {
			victims = append(victims, e.inst)
		}
	}
	p.mu.Unlock()
	for _, inst := range victims {
		inst.Stop("空闲回收")
	}
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
		if e.inst != nil {
			all = append(all, e.inst)
		}
	}
	p.mu.Unlock()
	var wg sync.WaitGroup
	for _, inst := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst.Stop("MCPcell 退出")
		}()
	}
	wg.Wait()
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
	Direct         bool               `json:"direct"` // running as node <bin>, without npx
	Package        *PackageInfo       `json:"package,omitempty"`
	Catalog        *CatalogInfo       `json:"catalog,omitempty"`
	Note           string             `json:"note,omitempty"`
	LastError      string             `json:"lastError,omitempty"`
	LastErrorAt    *time.Time         `json:"lastErrorAt,omitempty"`
	RetryAt        *time.Time         `json:"retryAt,omitempty"`
	StderrTail     string             `json:"stderrTail,omitempty"`
	Instance       *instance.Snapshot `json:"instance,omitempty"`
}

func (p *Pool) Status() []ServiceStatus {
	p.mu.Lock()
	type item struct {
		st   ServiceStatus
		def  *registry.Service
		inst *instance.Instance
	}
	items := make([]item, 0, len(p.entries))
	now := time.Now()
	for _, e := range p.entries {
		st := ServiceStatus{
			Name: e.name, Mode: e.def.Mode, State: "cold",
			Command:        DisplayCommand(e.def),
			IdleTimeoutSec: int(e.def.Idle().Seconds()),
			MemoryLimit:    uint64(e.def.MemoryLimit),
			Starts:         e.starts, Note: e.note, LastError: e.lastErr, StderrTail: e.lastStderr,
		}
		for k := range e.def.Env {
			st.EnvNames = append(st.EnvNames, k)
		}
		sort.Strings(st.EnvNames)
		if !e.lastErrAt.IsZero() {
			t := e.lastErrAt
			st.LastErrorAt = &t
		}
		var inst *instance.Instance
		switch {
		case e.op != nil:
			st.State = "starting"
		case e.inst != nil && e.inst.Alive():
			st.State, inst, st.Direct = "ready", e.inst, e.direct
		case e.nextTry.After(now):
			st.State = "failed"
			t := e.nextTry
			st.RetryAt = &t
		}
		items = append(items, item{st, e.def, inst})
	}
	p.mu.Unlock()
	out := make([]ServiceStatus, 0, len(items))
	for _, it := range items {
		if it.inst != nil {
			snap := it.inst.Snapshot() // syscalls; outside the pool lock
			it.st.Instance = &snap
		}
		if pkg, _, ok := p.npxPackage(it.def); ok {
			if pin := p.opts.Packages.Pinned(pkg); pin != nil {
				it.st.Package = &PackageInfo{Name: pin.Name, Version: pin.Version, InstalledAt: pin.InstalledAt}
			}
		}
		if p.opts.Catalog != nil && it.def.CatalogEnabled() {
			if e := p.opts.Catalog.Get(it.st.Name, p.fingerprint(it.def)); e != nil {
				it.st.Catalog = &CatalogInfo{Tools: max(e.Count("tools/list"), 0), Prompts: max(e.Count("prompts/list"), 0),
					Resources: max(e.Count("resources/list"), 0), FetchedAt: e.FetchedAt}
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

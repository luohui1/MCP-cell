// Package pool owns the upstream instances for every configured service:
// single-flight start, idle reaping, crash backoff and status.
package pool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/instance"
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
	Version     string
	ParentEnv   []string // the daemon's environment; only the allowlist reaches upstreams
	DefaultDir  string
	InitTimeout time.Duration
	Log         *slog.Logger
	// SpecFor overrides how a definition becomes a process (tests).
	SpecFor func(name string, def *registry.Service) proc.Spec
}

type Pool struct {
	opts    Options
	mu      sync.Mutex
	entries map[string]*entry
	closed  bool
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

func (p *Pool) start(e *entry, op *startOp) {
	spec := p.spec(e.name, e.def)
	inst, err := instance.Start(instance.Options{
		Name: e.name, Spec: spec, Version: p.opts.Version,
		InitTimeout: p.opts.InitTimeout, Log: p.opts.Log,
	})
	p.mu.Lock()
	e.op = nil
	switch {
	case err != nil:
		e.failures++
		e.lastErr, e.lastErrAt = err.Error(), time.Now()
		e.nextTry = time.Now().Add(backoff(e.failures))
		p.opts.Log.Warn("start failed", "service", e.name, "err", err)
	case p.closed:
		err = errors.New("MCPcell 正在退出")
	default:
		e.failures = 0
		e.inst = inst
		go p.watch(e, inst)
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
		return // stopped on purpose (idle, manual, shutdown)
	}
	now := time.Now()
	e.lastErr, e.lastErrAt = "进程意外退出", now
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

func (p *Pool) spec(name string, def *registry.Service) proc.Spec {
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

// Reap stops instances idle for longer than their service's idleTimeout.
func (p *Pool) Reap(now time.Time) {
	p.mu.Lock()
	var victims []*instance.Instance
	for _, e := range p.entries {
		if e.inst == nil || e.def.IdleTimeout.Duration <= 0 {
			continue
		}
		if idle, since := e.inst.Idle(); idle && now.Sub(since) >= e.def.IdleTimeout.Duration {
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

type ServiceStatus struct {
	Name           string             `json:"name"`
	Mode           string             `json:"mode"`
	State          string             `json:"state"` // cold | starting | ready | failed
	Command        string             `json:"command"`
	EnvNames       []string           `json:"envNames,omitempty"`
	IdleTimeoutSec int                `json:"idleTimeoutSec"`
	Starts         int                `json:"starts"`
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
		inst *instance.Instance
	}
	items := make([]item, 0, len(p.entries))
	now := time.Now()
	for _, e := range p.entries {
		st := ServiceStatus{
			Name: e.name, Mode: e.def.Mode, State: "cold",
			Command:        DisplayCommand(e.def),
			IdleTimeoutSec: int(e.def.IdleTimeout.Seconds()),
			Starts:         e.starts, LastError: e.lastErr, StderrTail: e.lastStderr,
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
			st.State, inst = "ready", e.inst
		case e.nextTry.After(now):
			st.State = "failed"
			t := e.nextTry
			st.RetryAt = &t
		}
		items = append(items, item{st, inst})
	}
	p.mu.Unlock()
	out := make([]ServiceStatus, 0, len(items))
	for _, it := range items {
		if it.inst != nil {
			snap := it.inst.Snapshot() // syscalls; outside the pool lock
			it.st.Instance = &snap
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

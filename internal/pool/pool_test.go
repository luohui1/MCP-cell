package pool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/instance"
	"github.com/luohui1/MCP-cell/internal/jsonrpc"
	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
	"github.com/luohui1/MCP-cell/internal/testutil/fakemcp"
)

func TestMain(m *testing.M) {
	fakemcp.MaybeRun()
	os.Exit(m.Run())
}

func newPool(t *testing.T, idle string) *Pool {
	t.Helper()
	cfg, err := registry.Parse([]byte(`{"services":{"fake":{"command":"fake","idleTimeout":"` + idle + `"},
		"broken":{"command":"definitely-not-a-real-binary-xyz"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	p := New(cfg, Options{Version: "test", ParentEnv: os.Environ(), DefaultDir: os.TempDir(),
		SpecFor: func(name string, def *registry.Service) proc.Spec {
			if name == "fake" {
				return fakemcp.Spec()
			}
			return proc.Spec{Argv: []string{def.Command}, Env: proc.BuildEnv(os.Environ(), nil), Dir: os.TempDir()}
		}})
	t.Cleanup(p.Close)
	return p
}

func TestConcurrentAcquireStartsOnce(t *testing.T) {
	p := newPool(t, "10m")
	var wg sync.WaitGroup
	got := make([]*instance.Instance, 10)
	for n := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst, err := p.Acquire(context.Background(), "fake")
			if err != nil {
				t.Error(err)
			}
			got[n] = inst
		}()
	}
	wg.Wait()
	for _, inst := range got[1:] {
		if inst != got[0] {
			t.Fatal("concurrent acquires started more than one instance")
		}
	}
	if st := p.Status()[1]; st.Name != "fake" || st.State != "ready" || st.Starts != 1 || st.Instance == nil {
		t.Fatalf("status %+v", st)
	}
}

func TestReapIdle(t *testing.T) {
	p := newPool(t, "50ms")
	inst, err := p.Acquire(context.Background(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	p.Reap(time.Now()) // not idle long enough yet
	if !inst.Alive() {
		t.Fatal("reaped too early")
	}
	p.Reap(time.Now().Add(time.Second))
	if inst.Alive() || p.Current("fake") != nil {
		t.Fatal("idle instance survived reaping")
	}
	again, err := p.Acquire(context.Background(), "fake")
	if err != nil || again == inst {
		t.Fatalf("expected a fresh instance after reaping: %v", err)
	}
}

func TestCrashRestartsWithNewProcess(t *testing.T) {
	p := newPool(t, "10m")
	inst, _ := p.Acquire(context.Background(), "fake")
	pid := inst.Snapshot().PID
	req, _ := jsonrpc.NewRequest(json.RawMessage(`1`), "tools/call", map[string]any{"name": "crash", "arguments": map[string]any{}})
	go inst.Call(context.Background(), dummy{}, req, func(jsonrpc.Message) {})
	<-inst.Done()
	time.Sleep(50 * time.Millisecond) // let the watcher record the crash
	st := p.Status()[1]
	if st.State != "cold" || st.LastError == "" {
		t.Fatalf("after crash: %+v", st)
	}
	next, err := p.Acquire(context.Background(), "fake")
	if err != nil || next.Snapshot().PID == pid {
		t.Fatalf("restart: %v", err)
	}
}

func TestFailedStartBacksOff(t *testing.T) {
	p := newPool(t, "10m")
	if _, err := p.Acquire(context.Background(), "broken"); err == nil {
		t.Fatal("expected start failure")
	}
	st := p.Status()[0]
	if st.Name != "broken" || st.State != "failed" || st.RetryAt == nil {
		t.Fatalf("status %+v", st)
	}
	if _, err := p.Acquire(context.Background(), "broken"); err == nil {
		t.Fatal("retried during backoff")
	}
	if _, err := p.Acquire(context.Background(), "nope"); err != ErrUnknownService {
		t.Fatal(err)
	}
}

func TestDisplayCommandMasksSecrets(t *testing.T) {
	def := &registry.Service{Command: "npx", Args: []string{"-y", "pkg", "--api-key", "sk-1", "--token=abc", "--port", "80"}}
	if got := DisplayCommand(def); got != "npx -y pkg --api-key *** --token=*** --port 80" {
		t.Fatal(got)
	}
}

type dummy struct{}

func (dummy) ID() string             { return "dummy" }
func (dummy) Notify(jsonrpc.Message) {}

func newPoolWith(t *testing.T, cfgJSON string, opts Options) *Pool {
	t.Helper()
	cfg, err := registry.Parse([]byte(cfgJSON))
	if err != nil {
		t.Fatal(err)
	}
	opts.Version = "test"
	if opts.SpecFor == nil {
		opts.SpecFor = func(string, *registry.Service) proc.Spec { return fakemcp.Spec() }
	}
	if opts.MaxInstances == 0 {
		opts.MaxInstances = cfg.Limits.MaxInstances
	}
	p := New(cfg, opts)
	t.Cleanup(p.Close)
	return p
}

func TestBudgetEvictsLeastRecentlyUsedIdle(t *testing.T) {
	p := newPoolWith(t, `{"limits":{"maxInstances":2},"services":{"a":{"command":"x"},"b":{"command":"x"},"c":{"command":"x"}}}`, Options{})
	a, _ := p.Acquire(context.Background(), "a")
	time.Sleep(20 * time.Millisecond)
	b, _ := p.Acquire(context.Background(), "b")
	if _, err := p.Acquire(context.Background(), "c"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("least recently used instance a was not evicted")
	}
	if !b.Alive() {
		t.Fatal("b was evicted instead of a")
	}
	// Both remaining instances busy: a new start must be refused, not evict.
	slow, _ := jsonrpc.NewRequest(json.RawMessage(`1`), "tools/call", map[string]any{"name": "slow", "arguments": map[string]any{"ms": 1500}})
	for _, n := range []string{"b", "c"} {
		inst := p.Current(n)
		go inst.Call(context.Background(), dummy{}, slow, func(jsonrpc.Message) {})
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := p.Acquire(context.Background(), "a"); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("expected budget refusal, got %v", err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestCatalogServesWhileCold(t *testing.T) {
	store := catalog.OpenStore(t.TempDir())
	p := newPoolWith(t, `{"services":{"fake":{"command":"x"},"live":{"command":"x","catalog":"off"}}}`, Options{Catalog: store})
	if _, _, ok := p.CachedInit("fake"); ok {
		t.Fatal("catalog before first start")
	}
	if _, err := p.Acquire(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "catalog", func() bool { _, ok := p.CachedList("fake", "tools/list"); return ok })
	p.Stop("fake", "test")
	if p.Current("fake") != nil {
		t.Fatal("still running")
	}
	if _, v, ok := p.CachedInit("fake"); !ok || v == "" {
		t.Fatal("initialize not answerable while cold")
	}
	raw, ok := p.CachedList("fake", "tools/list")
	if !ok || !strings.Contains(string(raw), `"slow"`) {
		t.Fatalf("tools/list while cold: %s", raw)
	}
	if st := p.Status()[0]; st.Catalog == nil || st.Catalog.Tools != 5 || st.Catalog.Prompts != 1 {
		t.Fatalf("status catalog %+v", st.Catalog)
	}
	// catalog "off" never caches.
	if _, err := p.Acquire(context.Background(), "live"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, ok := p.CachedList("live", "tools/list"); ok {
		t.Fatal(`catalog "off" service was cached`)
	}
}

func TestListChangedRefreshesCatalog(t *testing.T) {
	store := catalog.OpenStore(t.TempDir())
	p := newPoolWith(t, `{"services":{"fake":{"command":"x"}}}`, Options{Catalog: store})
	changed := make(chan string, 4)
	p.OnListChange(func(service, list string) { changed <- service + " " + list })
	inst, _ := p.Acquire(context.Background(), "fake")
	waitFor(t, "catalog", func() bool { _, ok := p.CachedList("fake", "tools/list"); return ok })
	req, _ := jsonrpc.NewRequest(json.RawMessage(`1`), "tools/call", map[string]any{"name": "add_tool", "arguments": map[string]any{"text": "z"}})
	inst.Call(context.Background(), dummy{}, req, func(jsonrpc.Message) {})
	select {
	case got := <-changed:
		if got != "fake tools/list" {
			t.Fatal(got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no catalog change reported")
	}
	raw, ok := p.CachedList("fake", "tools/list")
	if !ok || !strings.Contains(string(raw), "extra_z") {
		t.Fatalf("catalog not refreshed: %s", raw)
	}
}

func TestBuildSpecFlattensNpx(t *testing.T) {
	if _, err := proc.ResolveArgv([]string{"node"}, os.Environ(), os.TempDir()); err != nil {
		t.Skip("node not installed")
	}
	var installs int
	pkgs, err := npmcache.Open(t.TempDir(), func(ctx context.Context, spec, prefix string, env []string) error {
		installs++
		dir := filepath.Join(prefix, "node_modules", "@x", "srv")
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "cli.js"), nil, 0o644)
		return os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"version":"3.1.4","bin":"cli.js"}`), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := registry.Parse([]byte(`{"services":{
		"srv":{"command":"cmd","args":["/c","npx","-y","@x/srv","--flag"],"memoryLimit":"256MB"},
		"keep":{"command":"npx","args":["-y","@x/srv"],"flatten":false}}}`))
	p := New(cfg, Options{Packages: pkgs, ParentEnv: os.Environ(), DefaultDir: os.TempDir()})
	defer p.Close()

	before := p.fingerprint(cfg.Services["srv"])
	spec, direct, note := p.buildSpec(context.Background(), "srv", cfg.Services["srv"])
	if !direct || note != "" || len(spec.Argv) != 5 || !strings.HasSuffix(strings.ToLower(filepath.Base(spec.Argv[0])), "node.exe") && filepath.Base(spec.Argv[0]) != "node" {
		t.Fatalf("argv %q direct=%v note=%q", spec.Argv, direct, note)
	}
	if strings.Join(spec.Argv[1:3], " ") != "--max-semi-space-size=1 --optimize-for-size" {
		t.Fatalf("lean node flags missing: %q", spec.Argv)
	}
	if !strings.HasSuffix(filepath.ToSlash(spec.Argv[3]), "@x+srv@3.1.4/node_modules/@x/srv/cli.js") || spec.Argv[4] != "--flag" {
		t.Fatalf("argv %q", spec.Argv)
	}
	if spec.MemoryLimit != 256<<20 {
		t.Fatalf("memory limit %d", spec.MemoryLimit)
	}
	if after := p.fingerprint(cfg.Services["srv"]); after == before {
		t.Fatal("pinning a version must change the catalog fingerprint")
	}
	if st := p.Status(); st[1].Package == nil || st[1].Package.Version != "3.1.4" {
		t.Fatalf("status package %+v", st[1].Package)
	}
	spec, direct, _ = p.buildSpec(context.Background(), "keep", cfg.Services["keep"])
	if direct || spec.Argv[0] != "npx" || installs != 1 {
		t.Fatalf(`flatten:false still flattened: %q (installs %d)`, spec.Argv, installs)
	}
}

func TestBuildSpecFallsBackToNpx(t *testing.T) {
	pkgs, _ := npmcache.Open(t.TempDir(), func(context.Context, string, string, []string) error {
		return errors.New("offline")
	})
	cfg, _ := registry.Parse([]byte(`{"services":{"srv":{"command":"npx","args":["-y","@x/srv"]}}}`))
	p := New(cfg, Options{Packages: pkgs, ParentEnv: os.Environ(), DefaultDir: os.TempDir()})
	defer p.Close()
	spec, direct, note := p.buildSpec(context.Background(), "srv", cfg.Services["srv"])
	if direct || spec.Argv[0] != "npx" || !strings.Contains(note, "offline") {
		t.Fatalf("argv %q direct=%v note=%q", spec.Argv, direct, note)
	}
}

func TestPutAndRemove(t *testing.T) {
	p := newPoolWith(t, `{"services":{"fake":{"command":"x"}}}`, Options{})
	inst, _ := p.Acquire(context.Background(), "fake")
	def, _ := registry.ParseService("fake", []byte(`{"command":"x","idleTimeout":"5m"}`))
	p.Put("fake", def)
	select {
	case <-inst.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("editing a service did not stop its old instance")
	}
	if st := p.Status()[0]; st.IdleTimeoutSec != 300 || st.State != "cold" {
		t.Fatalf("after edit: %+v", st)
	}
	added, _ := registry.ParseService("added", []byte(`{"command":"x"}`))
	p.Put("added", added)
	if !p.Has("added") {
		t.Fatal("added service missing")
	}
	again, _ := p.Acquire(context.Background(), "fake")
	if !p.Remove("fake") || p.Has("fake") {
		t.Fatal("remove failed")
	}
	select {
	case <-again.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("removed service kept running")
	}
}

func TestLeanSpec(t *testing.T) {
	cases := []struct {
		argv, want string
		env        bool
	}{
		{"node server.js --port 1", "node --max-semi-space-size=1 --optimize-for-size server.js --port 1", false},
		{`cmd /c C:\n\node.exe s.js`, `C:\n\node.exe --max-semi-space-size=1 --optimize-for-size s.js`, false},
		{"npx -y pkg", "npx -y pkg", true},
		{"python server.py", "python server.py", false},
		{"node --inspect s.js", "node --inspect s.js", true},
	}
	for _, c := range cases {
		got := leanSpec(proc.Spec{Argv: strings.Fields(c.argv)})
		hasEnv := len(got.Env) > 0 && got.Env[len(got.Env)-1] == "NODE_OPTIONS=--max-semi-space-size=1"
		if strings.Join(got.Argv, " ") != c.want || hasEnv != c.env {
			t.Errorf("%q -> %q env=%v", c.argv, got.Argv, got.Env)
		}
	}
}

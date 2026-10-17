package pool

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/instance"
	"github.com/luohui1/MCP-cell/internal/jsonrpc"
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

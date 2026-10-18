package pool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/instance"
	"github.com/luohui1/MCP-cell/internal/jsonrpc"
)

// fakeSession answers roots/list with one root named after itself.
type fakeSession struct {
	id   string
	asks chan string
}

func newSession(id string) *fakeSession { return &fakeSession{id: id, asks: make(chan string, 4)} }

func (f *fakeSession) ID() string             { return f.id }
func (f *fakeSession) Notify(jsonrpc.Message) {}
func (f *fakeSession) InitParams() json.RawMessage {
	return json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{"roots":{}},"clientInfo":{"name":"client-` + f.id + `","version":"1"}}`)
}
func (f *fakeSession) Request(ctx context.Context, m jsonrpc.Message) (jsonrpc.Message, error) {
	f.asks <- m.Method
	return jsonrpc.NewResult(m.ID, json.RawMessage(`{"roots":[{"uri":"file:///work/`+f.id+`"}]}`)), nil
}

func callText(t *testing.T, inst *instance.Instance, ds instance.Downstream, tool string) string {
	t.Helper()
	req, _ := jsonrpc.NewRequest(json.RawMessage(`1`), "tools/call", map[string]any{"name": tool, "arguments": map[string]any{}})
	resp := inst.Call(context.Background(), ds, req, func(jsonrpc.Message) {})
	var res struct {
		Content []struct{ Text string } `json:"content"`
	}
	if len(resp.Error) > 0 || json.Unmarshal(resp.Result, &res) != nil || len(res.Content) == 0 {
		t.Fatalf("%s: %s %s", tool, resp.Error, resp.Result)
	}
	return res.Content[0].Text
}

func TestPerSessionInstances(t *testing.T) {
	p := newPoolWith(t, `{"services":{"s":{"command":"x","mode":"per-session"}}}`, Options{})
	a, b := newSession("aaaaaaaaaaaa"), newSession("bbbbbbbbbbbb")
	if _, err := p.Acquire(context.Background(), "s", nil); !errors.Is(err, ErrNeedsSession) {
		t.Fatalf("acquire without a session: %v", err)
	}
	ia, err := p.Acquire(context.Background(), "s", a)
	if err != nil {
		t.Fatal(err)
	}
	ib, _ := p.Acquire(context.Background(), "s", b)
	again, _ := p.Acquire(context.Background(), "s", a)
	if ia == ib || ia != again || p.Current("s", a) != ia || p.Current("s", newSession("other")) != nil {
		t.Fatal("each session must get its own instance, and keep it")
	}
	// The instance introduces itself as its client and can ask it for roots.
	if got := callText(t, ia, a, "client"); got != "client-"+a.id {
		t.Fatalf("handshake clientInfo = %q", got)
	}
	if got := callText(t, ib, b, "roots"); got != "file:///work/"+b.id {
		t.Fatalf("roots = %q", got)
	}
	if m := <-b.asks; m != "roots/list" {
		t.Fatalf("session b was asked %q", m)
	}
	if len(a.asks) != 0 {
		t.Fatal("session a was asked on b's behalf")
	}
	st := p.Status()[0]
	if st.Instance != nil || len(st.Instances) != 2 || st.State != "ready" {
		t.Fatalf("status: instance %v, %d session instances, %s", st.Instance, len(st.Instances), st.State)
	}
	p.Release("s", a)
	if ia.Alive() || !ib.Alive() {
		t.Fatal("release must stop only that session's instance")
	}
	if got := ia.StopReason(); got != "会话结束" {
		t.Fatalf("stop reason %q", got)
	}
}

func TestSharedInstanceRefusesAmbiguousServerRequests(t *testing.T) {
	p := newPoolWith(t, `{"services":{"s":{"command":"x"}}}`, Options{})
	a, b := newSession("aaaaaaaaaaaa"), newSession("bbbbbbbbbbbb")
	inst, err := p.Acquire(context.Background(), "s", a)
	if err != nil {
		t.Fatal(err)
	}
	if got := callText(t, inst, a, "client"); got != "mcpcell" {
		t.Fatalf("shared handshake clientInfo = %q", got)
	}
	// With calls of two sessions in flight, the request could be either's.
	slow, _ := jsonrpc.NewRequest(json.RawMessage(`1`), "tools/call", map[string]any{"name": "slow", "arguments": map[string]any{"ms": 2000}})
	go inst.Call(context.Background(), b, slow, func(jsonrpc.Message) {})
	time.Sleep(200 * time.Millisecond)
	req, _ := jsonrpc.NewRequest(json.RawMessage(`2`), "tools/call", map[string]any{"name": "roots", "arguments": map[string]any{}})
	resp := inst.Call(context.Background(), a, req, func(jsonrpc.Message) {})
	if !strings.Contains(string(resp.Result), `"isError":true`) || len(a.asks)+len(b.asks) != 0 {
		t.Fatalf("an ambiguous server request reached a client: %s", resp.Result)
	}
	if !p.PerSession("s") {
		t.Fatal("the service still shares instances after its server asked a client")
	}
}

func TestBudgetEvictsSharedBeforeSessionInstances(t *testing.T) {
	p := newPoolWith(t, `{"limits":{"maxInstances":2},"services":{"s":{"command":"x","mode":"per-session"},"x":{"command":"x"},"y":{"command":"x"}}}`, Options{})
	own, _ := p.Acquire(context.Background(), "s", newSession("aaaaaaaaaaaa"))
	time.Sleep(20 * time.Millisecond)
	shared, _ := p.Acquire(context.Background(), "x", nil)
	if _, err := p.Acquire(context.Background(), "y", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-shared.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the idle shared instance was not evicted")
	}
	if !own.Alive() {
		t.Fatal("a session's own instance was evicted although a shared one was idle")
	}
}

func TestReapSessionInstance(t *testing.T) {
	p := newPoolWith(t, `{"services":{"s":{"command":"x","mode":"per-session","idleTimeout":"1s"}}}`, Options{})
	inst, _ := p.Acquire(context.Background(), "s", newSession("aaaaaaaaaaaa"))
	p.Reap(time.Now().Add(2 * time.Second))
	if inst.Alive() || len(p.Status()[0].Instances) != 0 {
		t.Fatal("idle session instance was not reaped")
	}
}

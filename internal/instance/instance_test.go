package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/jsonrpc"
	"github.com/luohui1/MCP-cell/internal/testutil/fakemcp"
)

func TestMain(m *testing.M) {
	fakemcp.MaybeRun()
	os.Exit(m.Run())
}

type fakeDS struct {
	id    string
	mu    sync.Mutex
	notes []jsonrpc.Message
}

func (d *fakeDS) ID() string { return d.id }
func (d *fakeDS) Notify(m jsonrpc.Message) {
	d.mu.Lock()
	d.notes = append(d.notes, m)
	d.mu.Unlock()
}
func (d *fakeDS) methods() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, n := range d.notes {
		out = append(out, n.Method)
	}
	return out
}

func start(t *testing.T) *Instance {
	t.Helper()
	inst, err := Start(Options{Name: "fake", Spec: fakemcp.Spec(), Version: "test", InitTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Stop("test done") })
	return inst
}

func toolCall(id, tool string, args map[string]any, progressToken any) jsonrpc.Message {
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{"name": tool, "arguments": args}
	if progressToken != nil {
		params["_meta"] = map[string]any{"progressToken": progressToken}
	}
	m, _ := jsonrpc.NewRequest(json.RawMessage(id), "tools/call", params)
	return m
}

func resultText(t *testing.T, m jsonrpc.Message) string {
	t.Helper()
	if len(m.Error) > 0 {
		t.Fatalf("error response: %s", m.Error)
	}
	var r struct {
		Content []struct{ Text string } `json:"content"`
	}
	if err := json.Unmarshal(m.Result, &r); err != nil || len(r.Content) == 0 {
		t.Fatalf("bad result %s", m.Result)
	}
	return r.Content[0].Text
}

func noEvents(jsonrpc.Message) {}

func TestHandshake(t *testing.T) {
	inst := start(t)
	res, version := inst.InitResult()
	if version == "" || !strings.Contains(string(res), `"fake"`) {
		t.Fatalf("init result %s version %q", res, version)
	}
}

// Six sessions all use ids 1..40 at the same time on one upstream.
func TestSharedCallsDoNotCrossTalk(t *testing.T) {
	inst := start(t)
	var wg sync.WaitGroup
	errs := make(chan error, 6*40)
	for s := range 6 {
		ds := &fakeDS{id: fmt.Sprintf("s%d", s)}
		for n := 1; n <= 40; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				want := fmt.Sprintf("%s-%d", ds.id, n)
				id := fmt.Sprint(n)
				if n%2 == 0 {
					id = `"` + id + `"` // string and number ids must both survive
				}
				resp := inst.Call(context.Background(), ds, toolCall(id, "echo", map[string]any{"text": want}, nil), noEvents)
				var got struct {
					Content []struct{ Text string } `json:"content"`
				}
				_ = json.Unmarshal(resp.Result, &got)
				if string(resp.ID) != id || len(got.Content) == 0 || got.Content[0].Text != want {
					errs <- fmt.Errorf("session %s id %s: got id %s result %s", ds.id, id, resp.ID, resp.Result)
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if snap := inst.Snapshot(); snap.Sessions != 6 || snap.Calls != 240 || snap.Inflight != 0 {
		t.Errorf("snapshot %+v", snap)
	}
}

func TestProgressGoesToItsOwnRequest(t *testing.T) {
	inst := start(t)
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b"} {
		ds := &fakeDS{id: name}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var events []jsonrpc.Message
			resp := inst.Call(context.Background(), ds, toolCall("1", "slow", map[string]any{"ms": 200, "steps": 4}, "tok"),
				func(m jsonrpc.Message) { events = append(events, m) })
			if got := resultText(t, resp); got != "done" {
				t.Errorf("%s: %s", name, got)
			}
			if len(events) != 4 {
				t.Errorf("%s got %d progress events, want 4", name, len(events))
			}
			for _, e := range events {
				if tok := string(jsonrpc.Field(e.Params, "progressToken")); tok != `"tok"` {
					t.Errorf("%s: progress token %s not restored", name, tok)
				}
			}
		}()
	}
	wg.Wait()
}

func TestCancelOnlyAffectsOwnRequest(t *testing.T) {
	inst := start(t)
	a, b := &fakeDS{id: "a"}, &fakeDS{id: "b"}
	bDone := make(chan jsonrpc.Message, 1)
	go func() {
		bDone <- inst.Call(context.Background(), b, toolCall("7", "slow", map[string]any{"ms": 800}, nil), noEvents)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	aDone := make(chan jsonrpc.Message, 1)
	go func() {
		aDone <- inst.Call(ctx, a, toolCall("7", "slow", map[string]any{"ms": 800}, nil), noEvents)
	}()
	time.Sleep(150 * time.Millisecond)
	note, _ := jsonrpc.NewNotification("notifications/cancelled", map[string]any{"requestId": 7, "reason": "user"})
	inst.Forward(a, note)
	if got := resultText(t, <-bDone); got != "done" {
		t.Fatalf("b was affected by a's cancel: %s", got)
	}
	// The SDK server reports a cancelled handler as an isError tool result.
	if resp := <-aDone; len(resp.Error) == 0 && !strings.Contains(string(resp.Result), "context canceled") {
		t.Fatalf("a's call was not cancelled upstream: %s", resp.Result)
	}
}

func TestUpstreamExitIsResultUnknown(t *testing.T) {
	inst := start(t)
	a, b := &fakeDS{id: "a"}, &fakeDS{id: "b"}
	slow := make(chan jsonrpc.Message, 1)
	go func() {
		slow <- inst.Call(context.Background(), a, toolCall("1", "slow", map[string]any{"ms": 5000}, nil), noEvents)
	}()
	time.Sleep(150 * time.Millisecond)
	go inst.Call(context.Background(), b, toolCall("1", "crash", nil, nil), noEvents)
	select {
	case resp := <-slow:
		if code := resp.ErrorCode(); code != jsonrpc.CodeResultUnknown || string(resp.ID) != "1" {
			t.Fatalf("got %s / %s", resp.ID, resp.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight call was not failed when the upstream exited")
	}
	select {
	case <-inst.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("instance not marked done")
	}
	if resp := inst.Call(context.Background(), a, toolCall("2", "echo", nil, nil), noEvents); resp.ErrorCode() != jsonrpc.CodeUpstreamUnavailable {
		t.Fatalf("call after exit: %s", resp.Error)
	}
}

func TestListChangedReachesEverySession(t *testing.T) {
	inst := start(t)
	a, b := &fakeDS{id: "a"}, &fakeDS{id: "b"}
	resultText(t, inst.Call(context.Background(), b, toolCall("1", "echo", map[string]any{"text": "hi"}, nil), noEvents))
	resultText(t, inst.Call(context.Background(), a, toolCall("1", "add_tool", map[string]any{"text": "x"}, nil), noEvents))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(strings.Join(a.methods(), ","), "tools/list_changed") &&
			strings.Contains(strings.Join(b.methods(), ","), "tools/list_changed") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("list_changed not broadcast: a=%v b=%v", a.methods(), b.methods())
}

func TestDuplicateInflightIDRejected(t *testing.T) {
	inst := start(t)
	a := &fakeDS{id: "a"}
	go inst.Call(context.Background(), a, toolCall("5", "slow", map[string]any{"ms": 500}, nil), noEvents)
	time.Sleep(100 * time.Millisecond)
	resp := inst.Call(context.Background(), a, toolCall("5", "echo", nil, nil), noEvents)
	if resp.ErrorCode() != jsonrpc.CodeInvalidRequest {
		t.Fatalf("duplicate id accepted: %s %s", resp.Result, resp.Error)
	}
}

// lockedBuffer is a log destination safe for the instance's goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestStopOnPurposeLogsNoWarning(t *testing.T) {
	var logs lockedBuffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	// Stopping closes the pipes, possibly under the reader (servers whose
	// child processes outlive them a moment); a few rounds for the race.
	for range 3 {
		inst, err := Start(Options{Name: "fake", Spec: fakemcp.Spec(), Version: "test", InitTimeout: 20 * time.Second, Log: log})
		if err != nil {
			t.Fatal(err)
		}
		inst.Stop("预热完成")
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("warnings for instances stopped on purpose:\n%s", logs.String())
	}
}

func TestExitDuringHandshake(t *testing.T) {
	spec := fakemcp.Spec()
	spec.Env = append(spec.Env, fakemcp.ExitEnv+"=1")
	_, err := Start(Options{Name: "fake", Spec: spec, Version: "test", InitTimeout: 20 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "退出") || !strings.Contains(err.Error(), fakemcp.ExitMessage) {
		t.Fatalf("err = %v, want that the process exited, with its stderr", err)
	}
}

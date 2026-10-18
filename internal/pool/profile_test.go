package pool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/jsonrpc"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
	"github.com/luohui1/MCP-cell/internal/testutil/fakemcp"
)

// client is a session whose initialize params are given.
type client struct {
	id     string
	params string
}

func (c client) ID() string                  { return c.id }
func (c client) Notify(jsonrpc.Message)      {}
func (c client) InitParams() json.RawMessage { return json.RawMessage(c.params) }
func (c client) Request(_ context.Context, m jsonrpc.Message) (jsonrpc.Message, error) {
	return jsonrpc.NewResult(m.ID, json.RawMessage(`{"roots":[]}`)), nil
}

func asking(version, caps string) string {
	return `{"protocolVersion":"` + version + `","capabilities":` + caps + `,"clientInfo":{"name":"c","version":"1"}}`
}

func TestSharedInstancePerProtocolVersion(t *testing.T) {
	store := catalog.OpenStore(t.TempDir())
	p := newPoolWith(t, `{"services":{"fake":{"command":"x"}}}`, Options{Catalog: store})
	old1 := client{"a", asking("2025-03-26", `{}`)}
	old2 := client{"b", asking("2025-03-26", `{"roots":{}}`)}
	cur := client{"c", asking("2025-11-25", `{}`)}
	i1, err := p.Acquire(context.Background(), "fake", old1)
	if err != nil {
		t.Fatal(err)
	}
	i2, _ := p.Acquire(context.Background(), "fake", old2)
	i3, _ := p.Acquire(context.Background(), "fake", cur)
	if i1 != i2 || i1 == i3 {
		t.Fatal("sessions asking for one version must share an instance, and only they")
	}
	if _, v := i1.InitResult(); v != "2025-03-26" {
		t.Fatalf("old clients' instance negotiated %s", v)
	}
	if _, v := i3.InitResult(); v != "2025-11-25" {
		t.Fatalf("current clients' instance negotiated %s", v)
	}
	waitFor(t, "both catalogs", func() bool {
		_, a, ok1 := p.CachedInit("fake", old1.InitParams())
		_, b, ok2 := p.CachedInit("fake", cur.InitParams())
		return ok1 && ok2 && a == "2025-03-26" && b == "2025-11-25"
	})
	if st := p.Status()[0]; st.Instance == nil || len(st.Instances) != 1 {
		t.Fatalf("status shows %v shared and %d more instances", st.Instance != nil, len(st.Instances))
	}
}

func TestPerSessionCatalogIsPerKindOfClient(t *testing.T) {
	store := catalog.OpenStore(t.TempDir())
	p := newPoolWith(t, `{"services":{"s":{"command":"x","mode":"per-session"}}}`, Options{Catalog: store})
	withRoots := client{"a", asking("2025-11-25", `{"roots":{"listChanged":true}}`)}
	without := client{"b", asking("2025-11-25", `{}`)}
	if _, err := p.Acquire(context.Background(), "s", withRoots); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "catalog", func() bool { _, _, ok := p.CachedInit("s", withRoots.InitParams()); return ok })
	if _, _, ok := p.CachedInit("s", without.InitParams()); ok {
		t.Fatal("a client with other capabilities was answered from another client's catalog")
	}
	same := client{"c", asking("2025-11-25", `{"roots":{"listChanged":true}}`)}
	if _, ok := p.CachedList("s", "tools/list", same.InitParams()); !ok {
		t.Fatal("a client like the first was not answered from its catalog")
	}
	// Warming starts the service as the clients seen so far.
	p.Stop("s", "test")
	if err := p.Warm(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if seen := store.Clients(3); len(seen) != 1 || !strings.Contains(string(seen[0]), `"roots"`) {
		t.Fatalf("clients seen: %s", seen)
	}
}

func TestShareabilityCheck(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"services":{"asks":{"command":"asks"},"plain":{"command":"plain"}}}`
	spec := func(name string, _ *registry.Service) proc.Spec {
		s := fakemcp.Spec()
		if name == "asks" {
			s.Env = append(s.Env, fakemcp.AskRootsEnv+"=1")
		}
		return s
	}
	p := newPoolWith(t, cfg, Options{Catalog: catalog.OpenStore(dir), SpecFor: spec})
	ss := client{"a", asking("2025-11-25", `{"roots":{}}`)}
	for _, name := range []string{"asks", "plain"} {
		if _, err := p.Acquire(context.Background(), name, ss); err != nil {
			t.Fatal(err)
		}
		waitFor(t, name+" catalog", func() bool { _, _, ok := p.CachedInit(name, ss.InitParams()); return ok })
		p.CheckShared(name, ss)
	}
	if !p.PerSession("asks") || p.PerSession("plain") {
		t.Fatalf("after the check: asks per-session %v, plain per-session %v", p.PerSession("asks"), p.PerSession("plain"))
	}
	for _, st := range p.Status() {
		if st.Name == "asks" && (st.Mode != registry.ModePerSession || !strings.Contains(st.Note, "roots/list")) {
			t.Fatalf("status: %s %q", st.Mode, st.Note)
		}
	}
	// The next session of the moved service gets an instance of its own.
	mine, err := p.Acquire(context.Background(), "asks", client{"b", ss.params})
	if err != nil || mine == p.Current("asks", ss) {
		t.Fatalf("new session after the check: %v", err)
	}
	// A new daemon run keeps the result.
	p2 := newPoolWith(t, cfg, Options{Catalog: catalog.OpenStore(dir), SpecFor: spec})
	if !p2.PerSession("asks") || p2.PerSession("plain") {
		t.Fatal("check results not kept")
	}
}

func TestSharedInstanceForwardsToItsOnlyCaller(t *testing.T) {
	dir := t.TempDir()
	p := newPoolWith(t, `{"services":{"fake":{"command":"x"}}}`, Options{Catalog: catalog.OpenStore(dir)})
	a := newSession("a")
	inst, err := p.Acquire(context.Background(), "fake", a)
	if err != nil {
		t.Fatal(err)
	}
	// The only session with a call in flight is asked, so its call works.
	if got := callText(t, inst, a, "roots"); got != "file:///work/a" {
		t.Fatalf("roots through the shared instance: %q", got)
	}
	waitFor(t, "move to per-session", func() bool { return p.PerSession("fake") })
	if mine, _ := p.Acquire(context.Background(), "fake", newSession("b")); mine == inst {
		t.Fatal("a later session still got the shared instance")
	}
	if p2 := newPoolWith(t, `{"services":{"fake":{"command":"x"}}}`, Options{Catalog: catalog.OpenStore(dir)}); !p2.PerSession("fake") {
		t.Fatal("not kept across daemon runs")
	}
}

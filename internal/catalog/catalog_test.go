package catalog

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/instance"
	"github.com/luohui1/MCP-cell/internal/testutil/fakemcp"
)

func TestMain(m *testing.M) {
	fakemcp.MaybeRun()
	os.Exit(m.Run())
}

func TestFetchFollowsPagination(t *testing.T) {
	inst, err := instance.Start(instance.Options{Name: "fake", Spec: fakemcp.SpecWith(2), Version: "test", InitTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Stop("test")
	e, err := Fetch(context.Background(), inst, "fp1")
	if err != nil {
		t.Fatal(err)
	}
	if n := e.Count("tools/list"); n != 5 {
		t.Fatalf("tools across pages = %d, want 5: %s", n, e.Lists["tools/list"])
	}
	if strings.Contains(string(e.Lists["tools/list"]), "nextCursor") {
		t.Fatal("merged list still has a cursor")
	}
	if n := e.Count("prompts/list"); n != 1 {
		t.Fatalf("prompts = %d", n)
	}
	if n := e.Count("resources/list"); n != -1 {
		t.Fatalf("server has no resources capability, but resources/list was cached (%d)", n)
	}
	if e.ProtocolVersion == "" || !strings.Contains(string(e.InitResult), `"fake"`) {
		t.Fatalf("init result %s", e.InitResult)
	}
}

func list(names ...string) json.RawMessage {
	var tools []map[string]string
	for _, n := range names {
		tools = append(tools, map[string]string{"name": n})
	}
	b, _ := json.Marshal(map[string]any{"tools": tools})
	return b
}

func TestStoreValidityStalenessAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s := OpenStore(dir)
	e := &Entry{Fingerprint: "a", ProtocolVersion: "2025-06-18", InitResult: json.RawMessage(`{"serverInfo":{}}`),
		Lists: map[string]json.RawMessage{"tools/list": list("x", "y")}, FetchedAt: time.Now()}
	if changed, err := s.Put("svc", e); err != nil || changed != nil {
		t.Fatalf("first put: %v %v", changed, err)
	}
	if _, ok := s.List("svc", "b", "tools/list"); ok {
		t.Fatal("served a list for a different configuration fingerprint")
	}
	if _, ok := s.List("svc", "a", "tools/list"); !ok {
		t.Fatal("list not served")
	}
	s.MarkStale("svc", ListFor("notifications/tools/list_changed")...)
	if _, ok := s.List("svc", "a", "tools/list"); ok {
		t.Fatal("served a list the upstream said changed")
	}
	if _, _, ok := s.Init("svc", "a"); !ok {
		t.Fatal("init result should survive a list going stale")
	}

	// A new daemon run reads the entry back from disk.
	s2 := OpenStore(dir)
	if raw, ok := s2.List("svc", "a", "tools/list"); !ok || !strings.Contains(string(raw), `"y"`) {
		t.Fatalf("not persisted: %s", raw)
	}
	e2 := *e
	e2.Lists = map[string]json.RawMessage{"tools/list": list("x", "y", "z")}
	changed, _ := s2.Put("svc", &e2)
	if len(changed) != 1 || changed[0] != "tools/list" {
		t.Fatalf("changed = %v", changed)
	}
	if n := s2.Get("svc", "a").Count("tools/list"); n != 3 {
		t.Fatalf("count = %d", n)
	}
}

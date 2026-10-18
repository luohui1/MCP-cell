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
	if n := e.Count("tools/list"); n != 9 {
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
	if changed, err := s.Put("svc", "p", e); err != nil || changed != nil {
		t.Fatalf("first put: %v %v", changed, err)
	}
	if _, ok := s.List("svc", "b", "p", "tools/list"); ok {
		t.Fatal("served a list for a different configuration fingerprint")
	}
	if _, ok := s.List("svc", "a", "p", "tools/list"); !ok {
		t.Fatal("list not served")
	}
	s.MarkStale("svc", ListFor("notifications/tools/list_changed")...)
	if _, ok := s.List("svc", "a", "p", "tools/list"); ok {
		t.Fatal("served a list the upstream said changed")
	}
	if _, _, ok := s.Init("svc", "a", "p"); !ok {
		t.Fatal("init result should survive a list going stale")
	}

	// A new daemon run reads the entry back from disk.
	s2 := OpenStore(dir)
	if raw, ok := s2.List("svc", "a", "p", "tools/list"); !ok || !strings.Contains(string(raw), `"y"`) {
		t.Fatalf("not persisted: %s", raw)
	}
	e2 := *e
	e2.Lists = map[string]json.RawMessage{"tools/list": list("x", "y", "z")}
	changed, _ := s2.Put("svc", "p", &e2)
	if len(changed) != 1 || changed[0] != "tools/list" {
		t.Fatalf("changed = %v", changed)
	}
	if n := s2.Get("svc", "a").Count("tools/list"); n != 3 {
		t.Fatalf("count = %d", n)
	}
}

func TestStoreProfilesChecksAndOldFiles(t *testing.T) {
	dir := t.TempDir()
	// A file from before entries were kept per profile: ignored, not an error.
	if err := os.WriteFile(dir+"/svc.json", []byte(`{"fingerprint":"a","initResult":{},"lists":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenStore(dir)
	if _, _, ok := s.Init("svc", "a", "p"); ok {
		t.Fatal("old-format entry served")
	}
	roots := json.RawMessage(`{"capabilities":{"roots":{}},"protocolVersion":"2025-11-25"}`)
	for profile, client := range map[string]json.RawMessage{"old": nil, string(roots): roots} {
		e := &Entry{Fingerprint: "a", ProtocolVersion: "v-" + profile, InitResult: json.RawMessage(`{}`),
			Lists: map[string]json.RawMessage{"tools/list": list(profile)}, FetchedAt: time.Now(), Client: client}
		if _, err := s.Put("svc", profile, e); err != nil {
			t.Fatal(err)
		}
	}
	if _, v, _ := s.Init("svc", "a", "old"); v != "v-old" {
		t.Fatalf("profiles mixed up: %s", v)
	}
	s.MarkStale("svc", "tools/list")
	if _, ok := s.List("svc", "a", "old", "tools/list"); ok {
		t.Fatal("stale list served")
	}
	if err := s.SetCheck("svc", "a", `{"roots":{}}`, "asks for roots"); err != nil {
		t.Fatal(err)
	}
	s2 := OpenStore(dir)
	if why := s2.NeedsSession("svc", "a"); why != "asks for roots" {
		t.Fatalf("check not kept: %q", why)
	}
	if why := s2.NeedsSession("svc", "b"); why != "" {
		t.Fatal("check applied to another configuration")
	}
	if seen := s2.Clients(3); len(seen) != 1 || string(seen[0]) != string(roots) {
		t.Fatalf("clients: %s", seen)
	}
	// A new configuration starts over.
	if _, err := s2.Put("svc", "old", &Entry{Fingerprint: "b", InitResult: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s2.Init("svc", "a", string(roots)); ok || s2.NeedsSession("svc", "b") != "" {
		t.Fatal("entries of the old configuration survived")
	}
}

package clients

import (
	"os"
	"path/filepath"
	"testing"
)

// What automatic takeover remembers survives a restart, and a takeover.json
// written before it existed still loads.
func TestStoreAutoState(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "takeover.json"), []byte(`{"Records":[{"client":"codex","name":"a","service":"a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("codex", "a"); !ok || s.Baselined("codex") {
		t.Fatal("old file not read as it was")
	}
	_ = s.MarkSeen("codex", []string{"a", "b"}, true)
	_ = s.Exclude("codex", "b", "restored")
	_ = s.Exclude("codex", "b", "declined") // replaces, does not pile up
	_ = s.AddEvent(AutoEvent{Client: "codex", Name: "c", OK: true, Service: "c"})
	for i := 0; i < maxEvents+5; i++ {
		_ = s.AddEvent(AutoEvent{Client: "codex", Name: "x"})
	}

	s, err = OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Baselined("codex") || !s.SeenEntry("codex", "b") || s.SeenEntry("codex", "c") || s.Baselined("cursor") {
		t.Fatal("seen entries lost")
	}
	if e, ok := s.Excluded("codex", "b"); !ok || e.Reason != "declined" || len(s.Exclusions()) != 1 {
		t.Fatalf("exclusions: %+v", s.Exclusions())
	}
	if n := len(s.Events()); n != maxEvents {
		t.Fatalf("%d events kept, want %d", n, maxEvents)
	}
	_ = s.AckEvents()
	_ = s.Unexclude("codex", "b")
	s, _ = OpenStore(home)
	if _, ok := s.Excluded("codex", "b"); ok || !s.Events()[0].Seen {
		t.Fatal("unexclude or ack not saved")
	}
	if _, ok := s.Get("codex", "a"); !ok {
		t.Fatal("records lost")
	}
}

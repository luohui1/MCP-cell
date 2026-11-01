package events

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestEventsFromLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	l := Open(path)
	log := slog.New(l.Handler(slog.NewTextHandler(io.Discard, nil)))
	inst := log.With("service", "context7", "pid", 42)
	inst.Info("upstream ready", "protocol", "2025-11-25")
	inst.Info("upstream exited", "reason", "空闲回收", "err", errors.New("exit status 1"))
	inst.Info("upstream exited", "reason", "", "err", errors.New("exit status 3"))
	log.With("check", true).With("service", "ssh").Info("upstream ready") // a start check
	log.With("service", "ssh", "check", true).Info("upstream exited", "reason", "预热完成")
	log.Info("upstream exited", "service", "x", "reason", "MCPcell 退出")
	log.Warn("start failed", "service", "ssh", "err", errors.New("进程在完成初始化前退出了\nstderr：missing host"))
	log.Info("taken over", "client", "codex", "entry", "notes", "service", "notes")
	log.Info("session opened", "service", "notes") // not an event
	log.Info("daemon started", "port", 1, "pid", 7)

	got := l.Recent()
	want := []Event{
		{Kind: "daemon_started", Detail: "7"},
		{Kind: "taken_over", Client: "codex", Entry: "notes", Service: "notes"},
		{Kind: "start_failed", Service: "ssh", Detail: "进程在完成初始化前退出了"},
		{Kind: "crashed", Service: "context7", Detail: "exit status 3"},
		{Kind: "stopped_idle", Service: "context7"},
		{Kind: "started", Service: "context7"},
	}
	if len(got) != len(want) {
		t.Fatalf("events: %+v", got)
	}
	for i, w := range want {
		g := got[i]
		g.At = w.At
		if g != w {
			t.Errorf("event %d = %+v, want %+v", i, g, w)
		}
	}

	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < Max+10; i++ {
		l.Add(Event{Kind: "started", Service: "s"})
	}
	if n := len(l.Recent()); n != Max {
		t.Fatalf("%d kept, want %d", n, Max)
	}
	_ = l.Save()
	if n := len(Open(path).Recent()); n != Max {
		t.Fatalf("%d after reopening", n)
	}
}

func TestAutoTakeoverIsOneEvent(t *testing.T) {
	l := Open("")
	log := slog.New(l.Handler(slog.NewTextHandler(io.Discard, nil)))
	log.Info("taken over", "client", "codex", "entry", "notes", "service", "notes")
	log.Info("auto takeover", "client", "codex", "entry", "notes", "service", "notes")
	log.Info("taken over", "client", "claude-code", "entry", "notes", "service", "notes")
	got := l.Recent()
	if len(got) != 2 || got[0].Kind != "taken_over" || got[1].Kind != "auto_taken" {
		t.Fatalf("events: %+v", got)
	}
}

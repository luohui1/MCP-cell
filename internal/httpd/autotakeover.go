package httpd

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// Automatic takeover: an MCP the user adds to an agent's configuration is
// taken over the way 一键托管 would (same start check, same rewrite, backup
// and byte-for-byte restore), without anyone pressing a button. Only entries
// that appear after the first look count as new; an entry the user restored
// or dismissed is left alone for good (takeover.json's exclusions), and an
// agent that keeps putting its original entry back (Codex rewrites
// config.toml) is followed once, not fought.

// A configuration file is only read again when its modification time or
// size changed (Options.AutoTakeoverEvery sets how often that is checked).
type fileStamp struct {
	mod  time.Time
	size int64
}

// projectsBaseline is recorded once the Claude Code projects that existed at
// the first look have been: a project that shows up later (the user ran
// `claude mcp add` in it) has nothing but new servers.
const projectsBaseline = "claude-code@*"

// stamp is what tells whether c's configuration changed: its file, and for
// a Claude Code project its .mcp.json too. ok is false when the file is gone.
func stamp(c clients.Client) (st [2]fileStamp, ok bool) {
	info, err := os.Stat(c.Path())
	if err != nil {
		return st, false
	}
	st[0] = fileStamp{info.ModTime(), info.Size()}
	if p, isProject := c.(*clients.ClaudeProject); isProject {
		if info, err := os.Stat(filepath.Join(p.Dir(), ".mcp.json")); err == nil {
			st[1] = fileStamp{info.ModTime(), info.Size()}
		}
	}
	return st, true
}

func (s *Server) runAutoTakeover(ctx context.Context, every time.Duration) {
	t := time.NewTimer(min(10*time.Second, every)) // a first look soon after start
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.autoTakeover()
			t.Reset(every)
		}
	}
}

// autoTakeover looks at every agent once and takes over what is new.
func (s *Server) autoTakeover() {
	if s.opts.Takeovers == nil {
		return
	}
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return
	}
	took := false
	for _, c := range clients.Expand(s.opts.Clients) {
		st, ok := stamp(c)
		if !ok {
			continue
		}
		s.mu.Lock()
		same := s.autoStamps[c.ID()] == st
		s.autoStamps[c.ID()] = st
		s.mu.Unlock()
		if !same && s.autoClient(c, cfg) {
			took = true
		}
	}
	if !s.opts.Takeovers.Baselined(projectsBaseline) {
		_ = s.opts.Takeovers.MarkSeen(projectsBaseline, nil, true)
	}
	if took && cfg.SyncAppsEnabled() {
		// What one app got, the others get too (sync.go).
		s.takeMu.Lock()
		results, _ := s.syncApps(nil)
		s.takeMu.Unlock()
		for _, r := range results {
			if !r.OK {
				s.opts.Log.Warn("sync: not added", "client", r.Client, "service", r.Service, "err", firstLine(r.Error))
			}
		}
	}
}

// autoClient takes over c's new entries; took is whether any was.
func (s *Server) autoClient(c clients.Client, cfg *registry.Config) (took bool) {
	store := s.opts.Takeovers
	views := clients.Overview([]clients.Client{c}, store, cfg.Services, s.baseURL())
	v := views[0]
	if !v.Found || v.Error != "" {
		return
	}
	if !store.Baselined(c.ID()) {
		if _, project := clients.ProjectDir(c.ID()); project && store.Baselined(projectsBaseline) {
			// A project that appeared since the first look: all of it is new.
			_ = store.MarkSeen(c.ID(), nil, true)
		} else {
			// The first look: what is there already is not new. 一键托管 is for those.
			names := make([]string, 0, len(v.Entries))
			for _, ev := range v.Entries {
				names = append(names, ev.Name)
			}
			_ = store.MarkSeen(c.ID(), names, true)
			return
		}
	}
	var take, seen []string
	again := map[string]int{} // entries the agent put back, with the times already re-taken
	for _, ev := range v.Entries {
		if rec, ok := store.Get(c.ID(), ev.Name); ok {
			if ev.Changed && putBack(ev, cfg.Services[rec.Service]) {
				if _, no := store.Excluded(c.ID(), ev.Name); !no {
					again[ev.Name] = rec.Reattached
				}
			}
			continue
		}
		if store.SeenEntry(c.ID(), ev.Name) {
			continue
		}
		if !ev.Enabled {
			continue // looked at again once the user turns it on
		}
		seen = append(seen, ev.Name)
		if _, no := store.Excluded(c.ID(), ev.Name); no || ev.TakenOver || !cfg.AutoTakeoverEnabled() {
			continue
		}
		if ev.Kind == "stdio" && ev.Class != clients.ClassInternal && clients.Takeable(ev) && ev.Dead == "" {
			take = append(take, ev.Name)
		}
	}
	_ = store.MarkSeen(c.ID(), seen, false)
	if !cfg.AutoTakeoverEnabled() {
		return
	}
	for name, n := range again {
		if n >= 1 {
			// Put back a second time: stop following, and say so.
			_ = store.Exclude(c.ID(), name, "lost")
			_ = store.AddEvent(clients.AutoEvent{Client: c.ID(), Name: name, Error: c.Name() + " 一直把它改回原来的设置，已停止自动托管"})
			s.opts.Log.Warn("auto takeover: stopped, the agent keeps undoing it", "client", c.ID(), "entry", name)
			continue
		}
		_ = store.Delete(c.ID(), name) // the entry is its original again
		take = append(take, name)
	}
	if len(take) == 0 {
		return
	}
	s.takeMu.Lock()
	defer s.takeMu.Unlock()
	req := takeoverRequest{Names: take}
	tried := s.tryForTakeover(c, req)
	for _, name := range take {
		var errText string
		service, _, ok := s.attachOne(c, name, req, tried, &errText)
		if ok {
			took = true
			if rec, found := store.Get(c.ID(), name); found {
				rec.Auto = true
				if n, back := again[name]; back {
					rec.Reattached = n + 1
				}
				_ = store.Put(rec)
			}
		}
		_ = store.AddEvent(clients.AutoEvent{Client: c.ID(), Name: name, Service: service, OK: ok, Error: errText})
		if ok {
			s.opts.Log.Info("auto takeover", "client", c.ID(), "entry", name, "service", service)
		} else {
			s.opts.Log.Warn("auto takeover: not taken over", "client", c.ID(), "entry", name, "err", firstLine(errText))
		}
	}
	return took
}

// putBack reports whether an entry that was taken over is its original
// command again (the agent rewrote its file), as opposed to an edit.
func putBack(ev clients.EntryView, def *registry.Service) bool {
	if def == nil || ev.Kind != "stdio" {
		return false
	}
	return clients.ServiceKey(ev.Command, ev.Args, nil, "") == clients.ServiceKey(def.Command, def.Args, nil, "")
}

type autoStatus struct {
	Enabled  bool                `json:"enabled"`
	Sync     bool                `json:"sync"` // every app keeps the same set (sync.go)
	Events   []clients.AutoEvent `json:"events"`
	Excluded []clients.Exclusion `json:"excluded"`
}

func (s *Server) autoStatus() (autoStatus, error) {
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return autoStatus{}, err
	}
	st := autoStatus{Enabled: cfg.AutoTakeoverEnabled(), Sync: cfg.SyncAppsEnabled(), Events: []clients.AutoEvent{}, Excluded: []clients.Exclusion{}}
	if s.opts.Takeovers != nil {
		// Never null in JSON: the page iterates them.
		st.Events = append(st.Events, s.opts.Takeovers.Events()...)
		st.Excluded = append(st.Excluded, s.opts.Takeovers.Exclusions()...)
	}
	return st, nil
}

// handleAuto is GET/POST /api/auto: the settings (automatic takeover, and
// keeping every app on the same set), recent automatic takeovers, and the
// entries left alone.
func (s *Server) handleAuto(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct{ Enabled, Sync *bool }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || req.Enabled == nil && req.Sync == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		err := s.editConfig(func(cfg *registry.Config) error {
			if req.Enabled != nil {
				cfg.AutoTakeover = req.Enabled
			}
			if req.Sync != nil {
				cfg.SyncApps = req.Sync
			}
			return nil
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.opts.Log.Info("auto takeover setting", "enabled", req.Enabled != nil && *req.Enabled, "sync", req.Sync != nil && *req.Sync)
	}
	st, err := s.autoStatus()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleAutoAck is POST /api/auto/ack: the page has shown the events.
func (s *Server) handleAutoAck(w http.ResponseWriter, r *http.Request) {
	if s.opts.Takeovers != nil {
		_ = s.opts.Takeovers.AckEvents()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

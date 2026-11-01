// Package events keeps the daemon's recent happenings for the page's
// timeline: an MCP opened or was closed for being idle, failed to start, was
// taken over or restored, the daemon started or stopped. They come from the
// log records the daemon writes anyway (Handler picks the ones that matter),
// so every place that logs such a thing feeds the timeline without a second
// call. The last 200 are kept, and saved so a restart does not lose them.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Max is how many events are kept.
const Max = 200

// Event is one happening, with the fields the page needs to say it.
type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Service string    `json:"service,omitempty"`
	Client  string    `json:"client,omitempty"`
	Entry   string    `json:"entry,omitempty"`
	// Detail is a short reason: the error of a failed start, a PID.
	Detail string `json:"detail,omitempty"`
}

// Log is the ring of recent events.
type Log struct {
	path  string
	mu    sync.Mutex
	list  []Event // oldest first, at most Max
	dirty bool
}

// Open loads the events saved at path; a missing or unreadable file starts
// an empty log.
func Open(path string) *Log {
	l := &Log{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &l.list)
		if len(l.list) > Max {
			l.list = l.list[len(l.list)-Max:]
		}
	}
	return l
}

func (l *Log) Add(e Event) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// Automatic takeover logs the takeover itself first: one event, not two.
	if n := len(l.list); n > 0 && e.Kind == "auto_taken" {
		if last := l.list[n-1]; last.Kind == "taken_over" && last.Client == e.Client && last.Entry == e.Entry {
			l.list = l.list[:n-1]
		}
	}
	l.list = append(l.list, e)
	if len(l.list) > Max {
		l.list = append(l.list[:0], l.list[len(l.list)-Max:]...)
	}
	l.dirty = true
}

// Recent returns the events newest first.
func (l *Log) Recent() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, len(l.list))
	for i, e := range l.list {
		out[len(l.list)-1-i] = e
	}
	return out
}

// Save writes the events if they changed since the last save.
func (l *Log) Save() error {
	l.mu.Lock()
	if !l.dirty || l.path == "" {
		l.mu.Unlock()
		return nil
	}
	b, _ := json.Marshal(l.list)
	l.dirty = false
	l.mu.Unlock()
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// kinds maps the log messages that are events to their kind.
var kinds = map[string]string{
	"daemon started":                "daemon_started",
	"daemon stopped":                "daemon_stopped",
	"upstream ready":                "started",
	"upstream exited":               "", // decided by its reason
	"start failed":                  "start_failed",
	"quarantined":                   "quarantined",
	"taken over":                    "taken_over",
	"restored":                      "restored",
	"added to agent":                "added",
	"auto takeover":                 "auto_taken",
	"auto takeover: not taken over": "auto_failed",
	"auto takeover: stopped, the agent keeps undoing it": "auto_stopped",
	"service saved":   "saved",
	"service deleted": "deleted",
}

// exits maps an instance's stop reason to the event it is; missing means
// internal (a start check, the daemon going away) and not shown.
var exits = map[string]string{
	"空闲回收":      "stopped_idle",
	"手动回收":      "stopped_manual",
	"会话结束":      "stopped_session",
	"为其他实例腾出名额": "stopped_budget",
	"配置已修改":     "stopped_config",
	"更新版本":      "stopped_update",
	"服务已删除":     "stopped_deleted",
}

// Handler passes every record on to next and records the ones that are
// events.
func (l *Log) Handler(next slog.Handler) slog.Handler { return &handler{log: l, next: next} }

type handler struct {
	log   *Log
	next  slog.Handler
	attrs []slog.Attr // from WithAttrs
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &handler{log: h.log, next: h.next.WithAttrs(as), attrs: append(append([]slog.Attr(nil), h.attrs...), as...)}
}

// WithGroup is passed on; grouped attributes are not looked at.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{log: h.log, next: h.next.WithGroup(name), attrs: h.attrs}
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	err := h.next.Handle(ctx, r)
	kind, ok := kinds[r.Message]
	if !ok {
		return err
	}
	e := Event{At: r.Time}
	var reason, failure string
	check := false
	take := func(a slog.Attr) bool {
		v := a.Value.String()
		switch a.Key {
		case "service":
			e.Service = v
		case "client":
			e.Client = v
		case "entry":
			e.Entry = v
		case "reason":
			reason = v
		case "err":
			if a.Value.Any() != nil {
				failure = v
			}
		case "pid":
			if kind == "daemon_started" {
				e.Detail = v
			}
		case "check":
			check = a.Value.Bool()
		}
		return true
	}
	for _, a := range h.attrs {
		take(a)
	}
	r.Attrs(take)
	if check {
		return err // a start or shareability check, not a real run
	}
	if r.Message == "upstream exited" {
		switch k, known := exits[reason]; {
		case known:
			kind = k
		case reason == "" && failure != "":
			kind, e.Detail = "crashed", failure
		default:
			return err
		}
	}
	switch kind {
	case "start_failed", "auto_failed", "quarantined":
		e.Detail = firstLine(failure)
	}
	e.Kind = kind
	h.log.Add(e)
	return err
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

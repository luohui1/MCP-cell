// Package catalog caches what a service offers (initialize result and the
// tools/prompts/resources lists) so sessions can connect and list without an
// upstream process. An entry is only valid for the configuration fingerprint
// it was fetched with, and a list is dropped the moment the upstream says it
// changed.
package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/jsonrpc"
)

// Lists maps each cacheable list method to the result member holding items.
var Lists = map[string]string{
	"tools/list":               "tools",
	"prompts/list":             "prompts",
	"resources/list":           "resources",
	"resources/templates/list": "resourceTemplates",
}

// ListFor maps a */list_changed notification to the list methods it affects.
func ListFor(notification string) []string {
	switch notification {
	case "notifications/tools/list_changed":
		return []string{"tools/list"}
	case "notifications/prompts/list_changed":
		return []string{"prompts/list"}
	case "notifications/resources/list_changed":
		return []string{"resources/list", "resources/templates/list"}
	}
	return nil
}

// ChangedNotification is the notification to send when list changed.
func ChangedNotification(list string) string {
	switch list {
	case "tools/list":
		return "notifications/tools/list_changed"
	case "prompts/list":
		return "notifications/prompts/list_changed"
	}
	return "notifications/resources/list_changed"
}

type Entry struct {
	Fingerprint     string                     `json:"fingerprint"`
	ProtocolVersion string                     `json:"protocolVersion"`
	InitResult      json.RawMessage            `json:"initResult"`
	Lists           map[string]json.RawMessage `json:"lists"`
	FetchedAt       time.Time                  `json:"fetchedAt"`
}

// Count returns how many items list holds, or -1 if it is not cached.
func (e *Entry) Count(list string) int {
	raw, ok := e.Lists[list]
	if !ok {
		return -1
	}
	var items []json.RawMessage
	_ = json.Unmarshal(jsonrpc.Field(raw, Lists[list]), &items)
	return len(items)
}

// Fingerprint identifies a configuration. Anything that can change what the
// server offers belongs in parts (command, args, env values, cwd, pinned
// package version).
func Fingerprint(parts ...any) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

type Store struct {
	dir     string
	mu      sync.Mutex
	entries map[string]*Entry
	loaded  map[string]bool
	stale   map[string]map[string]bool
}

func OpenStore(dir string) *Store {
	return &Store{dir: dir, entries: map[string]*Entry{}, loaded: map[string]bool{}, stale: map[string]map[string]bool{}}
}

func (s *Store) path(service string) string { return filepath.Join(s.dir, service+".json") }

// entryLocked returns the entry for service if it matches fingerprint.
func (s *Store) entryLocked(service, fingerprint string) *Entry {
	if !s.loaded[service] {
		s.loaded[service] = true
		if b, err := os.ReadFile(s.path(service)); err == nil {
			var e Entry
			if json.Unmarshal(b, &e) == nil && e.Fingerprint != "" {
				s.entries[service] = &e
			}
		}
	}
	e := s.entries[service]
	if e == nil || e.Fingerprint != fingerprint {
		return nil
	}
	return e
}

// Get returns a copy of the matching entry, or nil.
func (s *Store) Get(service, fingerprint string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entryLocked(service, fingerprint)
	if e == nil {
		return nil
	}
	cp := *e
	cp.Lists = map[string]json.RawMessage{}
	for k, v := range e.Lists {
		if !s.stale[service][k] {
			cp.Lists[k] = v
		}
	}
	return &cp
}

// Init returns the cached initialize result and protocol version.
func (s *Store) Init(service, fingerprint string) (json.RawMessage, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entryLocked(service, fingerprint); e != nil && len(e.InitResult) > 0 {
		return e.InitResult, e.ProtocolVersion, true
	}
	return nil, "", false
}

// List returns a cached list result unless it is missing or stale.
func (s *Store) List(service, fingerprint, method string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entryLocked(service, fingerprint)
	if e == nil || s.stale[service][method] {
		return nil, false
	}
	raw, ok := e.Lists[method]
	return raw, ok
}

// MarkStale stops serving the given lists until the next Put.
func (s *Store) MarkStale(service string, methods ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stale[service] == nil {
		s.stale[service] = map[string]bool{}
	}
	for _, m := range methods {
		s.stale[service][m] = true
	}
}

// Put stores a fresh entry and returns the lists whose content differs from
// what was served before (sessions should be told they changed).
func (s *Store) Put(service string, e *Entry) (changed []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded[service] = true
	if prev := s.entries[service]; prev != nil {
		for m := range Lists {
			if !bytes.Equal(prev.Lists[m], e.Lists[m]) {
				changed = append(changed, m)
			}
		}
		sort.Strings(changed)
	}
	s.entries[service] = e
	delete(s.stale, service)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return changed, err
	}
	b, _ := json.Marshal(e)
	tmp := s.path(service) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return changed, err
	}
	return changed, os.Rename(tmp, s.path(service))
}

// Requester is what Fetch needs from a running instance.
type Requester interface {
	Request(ctx context.Context, method string, params any) (jsonrpc.Message, error)
	InitResult() (json.RawMessage, string)
}

const maxPages = 100

// Fetch reads every list the server's capabilities advertise, following
// pagination and merging the pages into one result without nextCursor.
func Fetch(ctx context.Context, r Requester, fingerprint string) (*Entry, error) {
	init, version := r.InitResult()
	var caps map[string]json.RawMessage
	_ = json.Unmarshal(jsonrpc.Field(init, "capabilities"), &caps)
	e := &Entry{Fingerprint: fingerprint, ProtocolVersion: version, InitResult: init,
		Lists: map[string]json.RawMessage{}, FetchedAt: time.Now()}
	for method, member := range Lists {
		capName, _, _ := strings.Cut(method, "/")
		if _, ok := caps[capName]; !ok {
			continue
		}
		merged, err := fetchAll(ctx, r, method, member)
		if err != nil {
			// Not cached means this list is always fetched live; that is safe.
			continue
		}
		e.Lists[method] = merged
	}
	return e, nil
}

func fetchAll(ctx context.Context, r Requester, method, member string) (json.RawMessage, error) {
	var first map[string]json.RawMessage
	var items []json.RawMessage
	var params any
	for page := 0; page < maxPages; page++ {
		resp, err := r.Request(ctx, method, params)
		if err != nil {
			return nil, err
		}
		if len(resp.Error) > 0 {
			return nil, fmt.Errorf("%s: %s", method, resp.Error)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(resp.Result, &obj); err != nil {
			return nil, err
		}
		var pageItems []json.RawMessage
		if err := json.Unmarshal(obj[member], &pageItems); err != nil {
			return nil, fmt.Errorf("%s: %s is not a list", method, member)
		}
		items = append(items, pageItems...)
		if first == nil {
			first = obj
		}
		var cursor string
		if json.Unmarshal(obj["nextCursor"], &cursor) != nil || cursor == "" {
			delete(first, "nextCursor")
			if items == nil {
				items = []json.RawMessage{}
			}
			first[member], _ = json.Marshal(items)
			return json.Marshal(first)
		}
		params = map[string]string{"cursor": cursor}
	}
	return nil, fmt.Errorf("%s: more than %d pages", method, maxPages)
}

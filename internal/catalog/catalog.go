// Package catalog caches what a service offers (initialize result and the
// tools/prompts/resources lists) so sessions can connect and list without an
// upstream process. A server may answer differently depending on what the
// client's initialize asked for (protocol version, capabilities), so there is
// one entry per client profile. Entries are only valid for the configuration
// fingerprint they were fetched with, and a list is dropped the moment the
// upstream says it changed.
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
	// Client is the initialize params the entry was fetched with, when they
	// matter beyond the protocol version (a session's own instance), so the
	// entry can be fetched again for the same kind of client.
	Client json.RawMessage `json:"client,omitempty"`
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

// maxProfiles bounds the entries kept per service; the oldest go first.
const maxProfiles = 8

// file is a service's catalog file: entries by client profile, all for one
// configuration fingerprint, and what shareability checks found.
type file struct {
	Fingerprint string            `json:"fingerprint"`
	Entries     map[string]*Entry `json:"entries"`
	// Checks maps a client's capabilities to "" when a shared instance serves
	// such clients as their own would, or to why it does not.
	Checks map[string]string `json:"checks,omitempty"`
	// Failure is the last failed start, kept until the service starts or its
	// configuration changes, so a restarted daemon still shows it.
	Failure *Failure `json:"failure,omitempty"`
}

// Failure is a service's failed start as the daemon reports it.
type Failure struct {
	Error    string    `json:"error"`
	At       time.Time `json:"at"`
	Failures int       `json:"failures"` // consecutive failed starts
}

type Store struct {
	dir    string
	mu     sync.Mutex
	files  map[string]*file
	loaded map[string]bool
	stale  map[string]map[string]bool // service + "\x00" + profile -> list methods
}

func OpenStore(dir string) *Store {
	return &Store{dir: dir, files: map[string]*file{}, loaded: map[string]bool{}, stale: map[string]map[string]bool{}}
}

func (s *Store) path(service string) string { return filepath.Join(s.dir, service+".json") }

// fileLocked returns service's file if it matches fingerprint. Files written
// before entries were kept per profile load without entries.
func (s *Store) fileLocked(service, fingerprint string) *file {
	if !s.loaded[service] {
		s.loaded[service] = true
		if b, err := os.ReadFile(s.path(service)); err == nil {
			var f file
			if json.Unmarshal(b, &f) == nil && f.Fingerprint != "" {
				s.files[service] = &f
			}
		}
	}
	f := s.files[service]
	if f == nil || f.Fingerprint != fingerprint {
		return nil
	}
	return f
}

// fileForWriteLocked returns service's file for fingerprint, replacing one
// that belongs to an older configuration.
func (s *Store) fileForWriteLocked(service, fingerprint string) *file {
	f := s.fileLocked(service, fingerprint)
	if f == nil {
		f = &file{Fingerprint: fingerprint}
		s.files[service] = f
		for k := range s.stale {
			if strings.HasPrefix(k, service+"\x00") {
				delete(s.stale, k)
			}
		}
	}
	if f.Entries == nil {
		f.Entries = map[string]*Entry{}
	}
	if f.Checks == nil {
		f.Checks = map[string]string{}
	}
	return f
}

func (s *Store) saveLocked(service string, f *file) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(f)
	tmp := s.path(service) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(service))
}

func (s *Store) entryLocked(service, fingerprint, profile string) *Entry {
	if f := s.fileLocked(service, fingerprint); f != nil {
		return f.Entries[profile]
	}
	return nil
}

// Get returns a copy of the most recently fetched entry for any profile, or
// nil. It describes the service (status), never answers a client.
func (s *Store) Get(service, fingerprint string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileLocked(service, fingerprint)
	if f == nil {
		return nil
	}
	var newest *Entry
	var profile string
	for p, e := range f.Entries {
		if newest == nil || e.FetchedAt.After(newest.FetchedAt) {
			newest, profile = e, p
		}
	}
	if newest == nil {
		return nil
	}
	cp := *newest
	cp.Lists = map[string]json.RawMessage{}
	for k, v := range newest.Lists {
		if !s.stale[service+"\x00"+profile][k] {
			cp.Lists[k] = v
		}
	}
	return &cp
}

// Failure returns the failed start recorded for this configuration, or nil.
func (s *Store) Failure(service, fingerprint string) *Failure {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.fileLocked(service, fingerprint); f != nil && f.Failure != nil {
		cp := *f.Failure
		return &cp
	}
	return nil
}

// SetFailure records a failed start for this configuration; nil clears it.
func (s *Store) SetFailure(service, fingerprint string, fail *Failure) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fail == nil {
		f := s.fileLocked(service, fingerprint)
		if f == nil || f.Failure == nil {
			return nil
		}
		f.Failure = nil
		return s.saveLocked(service, f)
	}
	f := s.fileForWriteLocked(service, fingerprint)
	cp := *fail
	f.Failure = &cp
	return s.saveLocked(service, f)
}

// Init returns the cached initialize result and protocol version for clients
// of profile.
func (s *Store) Init(service, fingerprint, profile string) (json.RawMessage, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entryLocked(service, fingerprint, profile); e != nil && len(e.InitResult) > 0 {
		return e.InitResult, e.ProtocolVersion, true
	}
	return nil, "", false
}

// List returns a cached list result for clients of profile unless it is
// missing or stale.
func (s *Store) List(service, fingerprint, profile, method string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entryLocked(service, fingerprint, profile)
	if e == nil || s.stale[service+"\x00"+profile][method] {
		return nil, false
	}
	raw, ok := e.Lists[method]
	return raw, ok
}

// MarkStale stops serving the given lists, for every profile, until each
// profile's next Put.
func (s *Store) MarkStale(service string, methods ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.files[service]
	if f == nil {
		return
	}
	for profile := range f.Entries {
		key := service + "\x00" + profile
		if s.stale[key] == nil {
			s.stale[key] = map[string]bool{}
		}
		for _, m := range methods {
			s.stale[key][m] = true
		}
	}
}

// Put stores a fresh entry for clients of profile and returns the lists whose
// content differs from what was served to them before (sessions should be
// told they changed).
func (s *Store) Put(service, profile string, e *Entry) (changed []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileForWriteLocked(service, e.Fingerprint)
	if prev := f.Entries[profile]; prev != nil {
		for m := range Lists {
			if !bytes.Equal(prev.Lists[m], e.Lists[m]) {
				changed = append(changed, m)
			}
		}
		sort.Strings(changed)
	}
	f.Entries[profile] = e
	for len(f.Entries) > maxProfiles {
		var oldest string
		for p, x := range f.Entries {
			if oldest == "" || x.FetchedAt.Before(f.Entries[oldest].FetchedAt) {
				oldest = p
			}
		}
		delete(f.Entries, oldest)
	}
	delete(s.stale, service+"\x00"+profile)
	return changed, s.saveLocked(service, f)
}

// Check returns what a shareability check found for clients with these
// capabilities: "" when shareable, else why not; ok is false when unchecked.
func (s *Store) Check(service, fingerprint, capabilities string) (reason string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.fileLocked(service, fingerprint); f != nil {
		reason, ok = f.Checks[capabilities]
	}
	return reason, ok
}

// SetCheck records a shareability check's result.
func (s *Store) SetCheck(service, fingerprint, capabilities, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileForWriteLocked(service, fingerprint)
	f.Checks[capabilities] = reason
	return s.saveLocked(service, f)
}

// NeedsSession returns why the service cannot be shared, if any check found
// a reason.
func (s *Store) NeedsSession(service, fingerprint string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.fileLocked(service, fingerprint)
	if f == nil {
		return ""
	}
	keys := make([]string, 0, len(f.Checks))
	for k := range f.Checks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if f.Checks[k] != "" {
			return f.Checks[k]
		}
	}
	return ""
}

// Clients returns the initialize params of up to max kinds of clients seen
// recently, newest first: what a per-session service is warmed for.
func (s *Store) Clients(max int) []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if names, err := os.ReadDir(s.dir); err == nil {
		for _, n := range names {
			if service, ok := strings.CutSuffix(n.Name(), ".json"); ok && !s.loaded[service] {
				s.fileLocked(service, "")
			}
		}
	}
	type seen struct {
		client json.RawMessage
		at     time.Time
	}
	newest := map[string]seen{}
	for _, f := range s.files {
		for _, e := range f.Entries {
			if len(e.Client) == 0 {
				continue
			}
			k := string(e.Client)
			if prev, ok := newest[k]; !ok || e.FetchedAt.After(prev.at) {
				newest[k] = seen{e.Client, e.FetchedAt}
			}
		}
	}
	list := make([]seen, 0, len(newest))
	for _, v := range newest {
		list = append(list, v)
	}
	sort.Slice(list, func(a, b int) bool { return list[a].at.After(list[b].at) })
	out := make([]json.RawMessage, 0, max)
	for i := 0; i < len(list) && i < max; i++ {
		out = append(out, list[i].client)
	}
	return out
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

package clients

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// Store keeps takeover records in ~/.mcpcell/takeover.json. The originals can
// hold credentials copied from client configs, so the file is user-private
// like the client configs themselves.
//
// It also remembers what automatic takeover needs: which entries it has
// already seen (only new ones are taken over), which the user does not want
// taken over (they restored or dismissed them), and recent automatic
// takeovers for the page to mention.
type Store struct {
	path string
	mu   sync.Mutex
	doc  storeDoc
}

type storeDoc struct {
	Records  []Record
	Excluded []Exclusion `json:",omitempty"`
	// Seen holds "client/name" of entries automatic takeover has looked at;
	// Baseline the clients whose existing entries were recorded first.
	Seen     []string    `json:",omitempty"`
	Baseline []string    `json:",omitempty"`
	Events   []AutoEvent `json:",omitempty"`
}

// Exclusion is an entry not to take over by itself: the user restored it or
// told MCPcell to leave it (Reason restored or declined), or the agent kept
// undoing the takeover (lost). Taking it over by hand clears it.
type Exclusion struct {
	Client string    `json:"client"`
	Name   string    `json:"name"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// AutoEvent is one automatic takeover, or an attempt that did not work.
type AutoEvent struct {
	Client  string    `json:"client"`
	Name    string    `json:"name"`
	Service string    `json:"service,omitempty"`
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	At      time.Time `json:"at"`
	// Seen: the user has acknowledged it on the page.
	Seen bool `json:"seen,omitempty"`
}

const maxEvents = 50

func OpenStore(home string) (*Store, error) {
	s := &Store{path: filepath.Join(home, "takeover.json")}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.doc); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Get(client, name string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.doc.Records {
		if r.Client == client && r.Name == name {
			return r, true
		}
	}
	return Record{}, false
}

func (s *Store) All() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.doc.Records...)
}

func (s *Store) Put(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.doc.Records[:0]
	for _, r := range s.doc.Records {
		if r.Client != rec.Client || r.Name != rec.Name {
			kept = append(kept, r)
		}
	}
	s.doc.Records = append(kept, rec)
	return s.saveLocked()
}

func (s *Store) Delete(client, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.doc.Records[:0]
	for _, r := range s.doc.Records {
		if r.Client != client || r.Name != name {
			kept = append(kept, r)
		}
	}
	s.doc.Records = kept
	return s.saveLocked()
}

// Excluded reports whether the entry is not to be taken over by itself.
func (s *Store) Excluded(client, name string) (Exclusion, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.doc.Excluded {
		if e.Client == client && e.Name == name {
			return e, true
		}
	}
	return Exclusion{}, false
}

func (s *Store) Exclusions() []Exclusion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Exclusion(nil), s.doc.Excluded...)
}

// Exclude keeps automatic takeover away from the entry, whatever it is
// changed to later.
func (s *Store) Exclude(client, name, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExclusionLocked(client, name)
	s.doc.Excluded = append(s.doc.Excluded, Exclusion{Client: client, Name: name, Reason: reason, At: time.Now()})
	return s.saveLocked()
}

// Unexclude is a takeover by hand: the user wants it after all.
func (s *Store) Unexclude(client, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dropExclusionLocked(client, name) {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) dropExclusionLocked(client, name string) bool {
	n := len(s.doc.Excluded)
	s.doc.Excluded = slices.DeleteFunc(s.doc.Excluded, func(e Exclusion) bool { return e.Client == client && e.Name == name })
	return len(s.doc.Excluded) != n
}

func seenKey(client, name string) string { return client + "/" + name }

// Baselined reports whether client's entries were recorded once already.
func (s *Store) Baselined(client string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.doc.Baseline, client)
}

// SeenEntry reports whether automatic takeover has looked at the entry.
func (s *Store) SeenEntry(client, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.doc.Seen, seenKey(client, name))
}

// MarkSeen records entries automatic takeover has looked at; baseline also
// records that client's existing entries are known, so that only entries
// added from now on count as new.
func (s *Store) MarkSeen(client string, names []string, baseline bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, n := range names {
		if k := seenKey(client, n); !slices.Contains(s.doc.Seen, k) {
			s.doc.Seen = append(s.doc.Seen, k)
			changed = true
		}
	}
	if baseline && !slices.Contains(s.doc.Baseline, client) {
		s.doc.Baseline = append(s.doc.Baseline, client)
		changed = true
	}
	if !changed {
		return nil
	}
	sort.Strings(s.doc.Seen)
	return s.saveLocked()
}

// AddEvent records an automatic takeover (or a failed one), newest last.
func (s *Store) AddEvent(e AutoEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.At.IsZero() {
		e.At = time.Now()
	}
	s.doc.Events = append(s.doc.Events, e)
	if n := len(s.doc.Events); n > maxEvents {
		s.doc.Events = s.doc.Events[n-maxEvents:]
	}
	return s.saveLocked()
}

func (s *Store) Events() []AutoEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AutoEvent(nil), s.doc.Events...)
}

// AckEvents marks every event as seen by the user.
func (s *Store) AckEvents() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.doc.Events {
		s.doc.Events[i].Seen = true
	}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	sort.Slice(s.doc.Records, func(a, b int) bool {
		if s.doc.Records[a].Client != s.doc.Records[b].Client {
			return s.doc.Records[a].Client < s.doc.Records[b].Client
		}
		return s.doc.Records[a].Name < s.doc.Records[b].Name
	})
	b, _ := json.MarshalIndent(s.doc, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

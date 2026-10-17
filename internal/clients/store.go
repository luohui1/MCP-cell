package clients

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Store keeps takeover records in ~/.mcpcell/takeover.json. The originals can
// hold credentials copied from client configs, so the file is user-private
// like the client configs themselves.
type Store struct {
	path string
	mu   sync.Mutex
	recs []Record
}

func OpenStore(home string) (*Store, error) {
	s := &Store{path: filepath.Join(home, "takeover.json")}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct{ Records []Record }
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	s.recs = doc.Records
	return s, nil
}

func (s *Store) Get(client, name string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.recs {
		if r.Client == client && r.Name == name {
			return r, true
		}
	}
	return Record{}, false
}

func (s *Store) All() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.recs...)
}

func (s *Store) Put(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.recs[:0]
	for _, r := range s.recs {
		if r.Client != rec.Client || r.Name != rec.Name {
			kept = append(kept, r)
		}
	}
	s.recs = append(kept, rec)
	return s.saveLocked()
}

func (s *Store) Delete(client, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.recs[:0]
	for _, r := range s.recs {
		if r.Client != client || r.Name != name {
			kept = append(kept, r)
		}
	}
	s.recs = kept
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	sort.Slice(s.recs, func(a, b int) bool {
		if s.recs[a].Client != s.recs[b].Client {
			return s.recs[a].Client < s.recs[b].Client
		}
		return s.recs[a].Name < s.recs[b].Name
	})
	b, _ := json.MarshalIndent(struct{ Records []Record }{s.recs}, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

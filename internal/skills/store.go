package skills

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Record is one location entry a sync replaced or created: enough to check
// it is still what MCPcell wrote and to put the original back.
type Record struct {
	Skill    string `json:"skill"`
	Location string `json:"location"`
	Path     string `json:"path"`
	Original Entry  `json:"original"`
	// Backup is where the original directory was moved.
	Backup string `json:"backup,omitempty"`
	Wrote  Entry  `json:"wrote"`
	// MadeDir: the location's skills directory did not exist and the sync
	// created it; restore removes it again if it is empty.
	MadeDir bool      `json:"madeDir,omitempty"`
	At      time.Time `json:"at"`
}

// Entry is what a location holds under a skill's name.
type Entry struct {
	Kind     string `json:"kind"` // absent | dir | link | copy (a dir MCPcell wrote)
	Link     string `json:"link,omitempty"`
	Junction bool   `json:"junction,omitempty"`
	Hash     string `json:"hash,omitempty"` // dir or copy content
}

// Source notes where a skill in the source directory came from.
type Source struct {
	From string    `json:"from"` // location ID
	At   time.Time `json:"at"`
	// Replaced is the backup of the source's previous version, when sync
	// --from replaced it.
	Replaced string `json:"replaced,omitempty"`
}

// state is ~/.mcpcell/skills.json.
type state struct {
	Records []Record          `json:"records"`
	Sources map[string]Source `json:"sources,omitempty"`
}

func (m *Manager) statePath() string { return filepath.Join(m.Home, "skills.json") }

func (m *Manager) load() (*state, error) {
	st := &state{Sources: map[string]Source{}}
	b, err := os.ReadFile(m.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, err
	}
	if st.Sources == nil {
		st.Sources = map[string]Source{}
	}
	return st, nil
}

func (m *Manager) save(st *state) error {
	sort.Slice(st.Records, func(a, b int) bool {
		if st.Records[a].Skill != st.Records[b].Skill {
			return st.Records[a].Skill < st.Records[b].Skill
		}
		return st.Records[a].Location < st.Records[b].Location
	})
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := os.MkdirAll(m.Home, 0o700); err != nil {
		return err
	}
	tmp := m.statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.statePath())
}

func (st *state) record(skill, loc string) (Record, int) {
	for i, r := range st.Records {
		if r.Skill == skill && r.Location == loc {
			return r, i
		}
	}
	return Record{}, -1
}

func (st *state) put(rec Record) {
	if _, i := st.record(rec.Skill, rec.Location); i >= 0 {
		st.Records[i] = rec
		return
	}
	st.Records = append(st.Records, rec)
}

func (st *state) drop(skill, loc string) {
	if _, i := st.record(skill, loc); i >= 0 {
		st.Records = append(st.Records[:i], st.Records[i+1:]...)
	}
}

// lock serializes syncs and restores between the daemon and the command
// line. A lock left behind by a crash expires after two minutes.
func (m *Manager) lock() (func(), error) {
	if err := os.MkdirAll(m.Home, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(m.Home, "skills.lock")
	for range 50 {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > 2*time.Minute {
			_ = os.Remove(p)
			continue
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, errors.New("另一个 MCPcell 进程正在同步或还原 Skill，请稍后重试")
}

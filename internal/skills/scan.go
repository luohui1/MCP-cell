package skills

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Status is every skill found in the source directory and the agents'
// locations.
type Status struct {
	Source    string     `json:"source"`
	Locations []Location `json:"locations"`
	Skills    []Skill    `json:"skills"`
}

// Skill is one skill name and its copies.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// State sums the skill up:
	//   synced    every location links to the source (or holds MCPcell's copy of it)
	//   same      the copies are identical but not unified
	//   differs   the copies differ
	//   single    one location has it
	//   external  every copy links to a directory another tool manages
	//   source    only the source has it
	State string `json:"state"`
	// Variants counts the distinct contents among the copies.
	Variants int `json:"variants"`
	// Reference is the copy others are compared with: the source ("mcpcell")
	// when there is one, otherwise the most recently modified copy.
	Reference string `json:"reference,omitempty"`
	// From is the location the source copy was taken from.
	From   string `json:"from,omitempty"`
	Copies []Copy `json:"copies"`
}

// Copy is what one location (or the source) holds under the skill's name.
type Copy struct {
	Location string `json:"location"` // a Location ID, or "mcpcell" for the source
	Path     string `json:"path"`
	// Kind: dir; linked (a link to the source); link (to anywhere else);
	// broken (a link whose target is gone).
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	// Match compares the content with the reference: same, newer, older, or
	// differs (different content, same modification time).
	Match   string    `json:"match,omitempty"`
	Variant string    `json:"variant,omitempty"` // A, B, … when the copies differ
	Files   int       `json:"files"`
	Newest  time.Time `json:"newest"`
	Hash    string    `json:"-"`
	// Managed: a sync put this entry here. Changed: it is no longer what the
	// sync wrote, so MCPcell will not touch it.
	Managed bool   `json:"managed,omitempty"`
	Changed bool   `json:"changed,omitempty"`
	Diff    *Diff  `json:"diff,omitempty"`
	Error   string `json:"error,omitempty"`

	tree *Tree
	real string // the directory the copy resolves to
}

// Scan reports every skill, or only the named ones. It reads file contents
// only for skills held in more than one directory; links to one directory
// are the same copy.
func (m *Manager) Scan(names ...string) (*Status, error) {
	st, err := m.load()
	if err != nil {
		return nil, err
	}
	return m.scan(st, names, false)
}

// scanner caches directory reads by resolved path, so links to one target
// are read once.
type scanner struct {
	full  bool // hash every copy (a sync needs the hashes)
	stats map[string]*Tree
	trees map[string]*Tree
}

func (s *scanner) read(real string, full bool) (*Tree, error) {
	cache, read := s.stats, statTree
	if full {
		cache, read = s.trees, readTree
	}
	key := pathKey(real)
	if t := cache[key]; t != nil {
		return t, nil
	}
	t, err := read(real)
	if err == nil {
		cache[key] = t
	}
	return t, err
}

func (m *Manager) scan(st *state, names []string, full bool) (*Status, error) {
	refresh(m.Locations)
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	found := map[string]bool{}
	collect := func(dir string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if n := e.Name(); checkName(n) == nil && (len(want) == 0 || want[n]) {
				found[n] = true
			}
		}
	}
	collect(m.SourceDir())
	for _, l := range m.Locations {
		if l.Exists {
			collect(l.Path)
		}
	}
	for n := range want {
		found[n] = true // report a name that is nowhere as such
	}
	out := &Status{Source: m.SourceDir(), Locations: append([]Location(nil), m.Locations...)}
	sc := &scanner{full: full, stats: map[string]*Tree{}, trees: map[string]*Tree{}}
	for n := range found {
		if sk, ok := m.skill(st, n, sc); ok || want[n] {
			out.Skills = append(out.Skills, sk)
		}
	}
	sort.Slice(out.Skills, func(a, b int) bool { return out.Skills[a].Name < out.Skills[b].Name })
	return out, nil
}

// skill builds one skill's report; ok is false when no location holds a
// skill (a directory with SKILL.md, or a link) by that name.
func (m *Manager) skill(st *state, name string, sc *scanner) (Skill, bool) {
	sk := Skill{Name: name, From: st.Sources[name].From}
	src := filepath.Join(m.SourceDir(), name)
	if c, ok := m.inspectCopy(SourceID, src, sc); ok && c.Kind == "dir" {
		sk.Copies = append(sk.Copies, c)
	}
	for _, l := range m.Locations {
		if l.Exists {
			if c, ok := m.inspectCopy(l.ID, filepath.Join(l.Path, name), sc); ok {
				sk.Copies = append(sk.Copies, c)
			}
		}
	}
	if len(sk.Copies) == 0 {
		return sk, false
	}
	// Hash only when there is something to compare: copies in more than
	// one directory, or MCPcell's own copies to check.
	dirs := map[string]bool{}
	full := sc.full
	for _, c := range sk.Copies {
		if c.real != "" {
			dirs[pathKey(c.real)] = true
		}
		if rec, i := st.record(name, c.Location); i >= 0 && rec.Wrote.Kind == "copy" {
			full = true
		}
	}
	full = full || len(dirs) > 1
	for i := range sk.Copies {
		c := &sk.Copies[i]
		if c.real == "" || c.Error != "" {
			continue
		}
		if sk.Description == "" {
			sk.Description = description(c.real)
		}
		if !full {
			c.Hash = "dir:" + pathKey(c.real)
			continue
		}
		t, err := sc.read(c.real, true)
		if err != nil {
			c.Error, c.tree = err.Error(), nil
			continue
		}
		c.tree, c.Hash = t, t.Hash
	}
	for i := range sk.Copies {
		c := &sk.Copies[i]
		if rec, j := st.record(name, c.Location); j >= 0 {
			c.Managed = true
			c.Changed = !stillWritten(rec, *c)
		}
	}
	compare(&sk)
	return sk, true
}

// inspectCopy reads what path holds, without reading file contents. ok is
// false for nothing, a file, or a directory without SKILL.md.
func (m *Manager) inspectCopy(loc, path string, sc *scanner) (Copy, bool) {
	c := Copy{Location: loc, Path: path}
	fi, err := os.Lstat(path)
	if err != nil {
		return c, false
	}
	real := path
	switch {
	case isLink(fi):
		target, err := readLink(path)
		if err != nil {
			return c, false
		}
		c.Target = target
		c.Kind = "link"
		if loc != SourceID && samePath(target, filepath.Join(m.SourceDir(), filepath.Base(path))) {
			c.Kind = "linked"
		}
		if real, err = resolve(path); err != nil || !isDir(real) {
			c.Kind = "broken"
			return c, true
		}
	case fi.IsDir():
		c.Kind = "dir"
		if _, err := os.Stat(filepath.Join(real, "SKILL.md")); err != nil {
			return c, false
		}
	default:
		return c, false
	}
	c.real = real
	t, err := sc.read(real, false)
	if err != nil {
		c.Error = err.Error()
		return c, true
	}
	c.tree, c.Files, c.Newest = t, len(t.Files), t.Newest
	return c, true
}

// stillWritten reports whether a managed entry is still what the sync wrote.
func stillWritten(rec Record, c Copy) bool {
	switch rec.Wrote.Kind {
	case "link":
		return (c.Kind == "linked" || c.Kind == "link" || c.Kind == "broken") && samePath(c.Target, rec.Wrote.Link)
	case "copy":
		return c.Kind == "dir" && c.Hash == rec.Wrote.Hash
	}
	return false
}

// compare fills in the reference, variants and per-copy match.
func compare(sk *Skill) {
	var ref *Copy
	for i := range sk.Copies {
		c := &sk.Copies[i]
		if c.tree == nil {
			continue
		}
		if c.Location == SourceID {
			ref = c
			break
		}
		if ref == nil || c.Newest.After(ref.Newest) {
			ref = c
		}
	}
	if ref != nil {
		sk.Reference = ref.Location
	}
	letters := map[string]string{}
	if ref != nil {
		letters[ref.Hash] = "A"
	}
	for i := range sk.Copies {
		c := &sk.Copies[i]
		if c.tree == nil {
			continue
		}
		if letters[c.Hash] == "" {
			letters[c.Hash] = string(rune('A' + len(letters)))
		}
		c.Variant = letters[c.Hash]
		switch {
		case c.Hash == ref.Hash:
			c.Match = "same"
		case c.Newest.After(ref.Newest):
			c.Match = "newer"
		case c.Newest.Before(ref.Newest):
			c.Match = "older"
		default:
			c.Match = "differs"
		}
		if c.Match != "same" {
			c.Diff = diffTrees(ref.tree, c.tree)
		}
	}
	sk.Variants = len(letters)
	if sk.Variants <= 1 {
		for i := range sk.Copies {
			sk.Copies[i].Variant = ""
		}
	}

	var hasSource bool
	var locs, linked, external int
	targets := map[string]bool{}
	for _, c := range sk.Copies {
		if c.Location == SourceID {
			hasSource = true
			continue
		}
		locs++
		switch {
		case c.Kind == "linked" || c.Managed && !c.Changed && c.Match == "same":
			linked++
		case c.Kind == "link" || c.Kind == "broken":
			external++
			targets[pathKey(c.Target)] = true
		}
	}
	switch {
	case hasSource && locs == 0:
		sk.State = "source"
	case hasSource && linked == locs:
		sk.State = "synced"
	case !hasSource && external == locs && len(targets) == 1:
		sk.State = "external"
	case sk.Variants > 1:
		sk.State = "differs"
	case locs == 1 && !hasSource:
		sk.State = "single"
	default:
		sk.State = "same"
	}
}

var errNowhere = errors.New("没有找到这个 Skill")

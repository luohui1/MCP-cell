package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Locations honor CODEX_HOME and CLAUDE_CONFIG_DIR. Clear them so no test can
// ever reach the real skill directories of the machine running it.
func TestMain(m *testing.M) {
	os.Unsetenv("CODEX_HOME")
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	os.Exit(m.Run())
}

// inTemp fails the test unless path is under the test's temp directories.
func inTemp(t *testing.T, path string) {
	t.Helper()
	if !strings.HasPrefix(path, os.TempDir()) && !strings.Contains(path, filepath.Join("Temp", "")) {
		t.Fatalf("refusing to touch %s outside the temp dir", path)
	}
}

// fixture is a fake user home with the agents' directories, and an MCPcell
// home, both in the test's temp dir.
func fixture(t *testing.T, agents ...string) (*Manager, string) {
	t.Helper()
	user := t.TempDir()
	home := filepath.Join(t.TempDir(), ".mcpcell")
	inTemp(t, user)
	inTemp(t, home)
	for _, a := range agents {
		if err := os.MkdirAll(filepath.Join(user, a), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := New(home, user)
	for _, l := range m.Locations {
		inTemp(t, l.Path)
	}
	return m, user
}

func (m *Manager) dir(t *testing.T, id string) string {
	t.Helper()
	l, ok := m.location(id)
	if !ok {
		t.Fatalf("no location %s", id)
	}
	return l.Path
}

var old = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// mkSkill writes a skill directory; every file gets modification time at.
func mkSkill(t *testing.T, dir, name string, at time.Time, files map[string]string) string {
	t.Helper()
	inTemp(t, dir)
	root := filepath.Join(dir, name)
	for p, body := range files {
		f := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(f, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func skillMD(desc string) string {
	return "---\nname: demo\ndescription: " + desc + "\n---\n\n# Demo\n"
}

// snapshot records every entry under root without following links: file
// bytes and modification times, link targets, directories.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case isLink(fi):
			target, _ := readLink(p)
			out[rel] = "link " + target
		case fi.IsDir():
			out[rel] = "dir"
		default:
			b, _ := os.ReadFile(p)
			out[rel] = fmt.Sprintf("file %q %s", b, fi.ModTime().UTC().Format(time.RFC3339Nano))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func find(t *testing.T, st *Status, name string) Skill {
	t.Helper()
	for _, s := range st.Skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("skill %s not in scan", name)
	return Skill{}
}

func copyIn(sk Skill, loc string) Copy {
	if c := copyAt(sk, loc); c != nil {
		return *c
	}
	return Copy{}
}

func actions(r *Result) map[string]string {
	out := map[string]string{}
	for _, a := range r.Actions {
		v := a.Do
		if a.Error != "" {
			v += " error: " + a.Error
		}
		out[a.Skill+"@"+a.Location] = v
	}
	return out
}

func TestScanStates(t *testing.T) {
	m, user := fixture(t, ".claude", ".agents", ".codex", ".gemini")
	same := map[string]string{"SKILL.md": skillMD("Shared helper"), "scripts/run.py": "print(1)\n"}
	mkSkill(t, m.dir(t, "claude"), "shared", old, same)
	mkSkill(t, m.dir(t, "agents"), "shared", old, same)
	mkSkill(t, m.dir(t, "codex"), "shared", old.Add(time.Hour), map[string]string{"SKILL.md": skillMD("Shared helper v2"), "scripts/run.py": "print(1)\n"})
	mkSkill(t, m.dir(t, "claude"), "solo", old, map[string]string{"SKILL.md": skillMD("Only here")})
	mkSkill(t, m.dir(t, "claude"), "notes", old, map[string]string{"README.md": "not a skill"})
	mkSkill(t, m.dir(t, "codex"), ".system", old, map[string]string{"SKILL.md": "x"})
	other := mkSkill(t, filepath.Join(user, ".othertool", "skills"), "vendor", old, map[string]string{"SKILL.md": skillMD("Vendor")})
	for _, id := range []string{"claude", "gemini"} {
		_ = os.MkdirAll(m.dir(t, id), 0o755)
		if err := makeJunction(other, filepath.Join(m.dir(t, id), "vendor")); err != nil {
			t.Fatal(err)
		}
	}

	st, err := m.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range st.Skills {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"shared", "solo", "vendor"}) {
		t.Fatalf("skills %v", names)
	}
	sh := find(t, st, "shared")
	if sh.State != "differs" || sh.Variants != 2 || sh.Reference != "codex" || sh.Description != "Shared helper" && sh.Description != "Shared helper v2" {
		t.Fatalf("shared %+v", sh)
	}
	if c := copyIn(sh, "claude"); c.Match != "older" || c.Variant != "B" || c.Diff == nil || !reflect.DeepEqual(c.Diff.Changed, []string{"SKILL.md"}) {
		t.Fatalf("claude copy %+v diff %+v", c, c.Diff)
	}
	if c := copyIn(sh, "codex"); c.Match != "same" || c.Variant != "A" {
		t.Fatalf("codex copy %+v", c)
	}
	if s := find(t, st, "solo"); s.State != "single" || s.Description != "Only here" {
		t.Fatalf("solo %+v", s)
	}
	v := find(t, st, "vendor")
	if v.State != "external" || copyIn(v, "gemini").Kind != "link" || !samePath(copyIn(v, "gemini").Target, other) {
		t.Fatalf("vendor %+v", v)
	}
}

func TestSyncIdenticalCopiesThenRestoreByteForByte(t *testing.T) {
	m, _ := fixture(t, ".claude", ".agents")
	files := map[string]string{"SKILL.md": skillMD("Demo"), "scripts/a.sh": "echo a\n", "ref/notes.md": "n\n"}
	claude := mkSkill(t, m.dir(t, "claude"), "demo", old, files)
	agents := mkSkill(t, m.dir(t, "agents"), "demo", old.Add(time.Minute), files)
	before := map[string]map[string]string{"claude": snapshot(t, m.dir(t, "claude")), "agents": snapshot(t, m.dir(t, "agents"))}

	res, err := m.Sync(SyncOptions{Names: []string{"demo"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"demo@mcpcell": "import", "demo@claude": "replace", "demo@agents": "replace"}
	if got := actions(res); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions %v", got)
	}
	src := filepath.Join(m.SourceDir(), "demo")
	for _, p := range []string{claude, agents} {
		target, err := readLink(p)
		if err != nil || !samePath(target, src) {
			t.Fatalf("%s -> %s %v", p, target, err)
		}
		if b, err := os.ReadFile(filepath.Join(p, "scripts", "a.sh")); err != nil || string(b) != "echo a\n" {
			t.Fatalf("read through link: %q %v", b, err)
		}
	}
	st, _ := m.Scan()
	if s := find(t, st, "demo"); s.State != "synced" || s.From == "" {
		t.Fatalf("after sync %+v", s)
	}
	// Editing through one agent's link edits the one copy everyone sees.
	if err := os.WriteFile(filepath.Join(claude, "SKILL.md"), []byte(skillMD("Edited")), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(agents, "SKILL.md")); !strings.Contains(string(b), "Edited") {
		t.Fatal("edit not visible through the other link")
	}
	// Syncing again changes nothing.
	res, err = m.Sync(SyncOptions{Names: []string{"demo"}})
	if err != nil || res.Changes() != 0 {
		t.Fatalf("second sync %v %v", actions(res), err)
	}

	res, err = m.Restore(RestoreOptions{Names: []string{"demo"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(res); !reflect.DeepEqual(got, map[string]string{"demo@claude": "restore", "demo@agents": "restore"}) {
		t.Fatalf("restore %v", got)
	}
	for id, snap := range before {
		if after := snapshot(t, m.dir(t, id)); !reflect.DeepEqual(after, snap) {
			t.Fatalf("%s not restored byte for byte:\n%v\n%v", id, snap, after)
		}
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("restore should keep the source copy")
	}
	if st, _ := m.load(); len(st.Records) != 0 {
		t.Fatalf("records left: %+v", st.Records)
	}
}

func TestSyncDifferentCopiesNeedsAChoice(t *testing.T) {
	m, _ := fixture(t, ".claude", ".codex")
	mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("Old"), "a.txt": "a"})
	mkSkill(t, m.dir(t, "codex"), "demo", old.Add(time.Hour), map[string]string{"SKILL.md": skillMD("New"), "b.txt": "b"})
	before := map[string]map[string]string{"claude": snapshot(t, m.dir(t, "claude")), "codex": snapshot(t, m.dir(t, "codex"))}

	res, err := m.Sync(SyncOptions{Names: []string{"demo"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(res); !reflect.DeepEqual(got, map[string]string{"demo@claude": "conflict", "demo@codex": "conflict"}) {
		t.Fatalf("actions %v", got)
	}
	for _, a := range res.Actions {
		if a.Location == "claude" && (a.Match != "older" || a.Diff == nil || len(a.Diff.Changed) != 1 || len(a.Diff.Added) != 1 || len(a.Diff.Removed) != 1) {
			t.Fatalf("claude conflict %+v %+v", a, a.Diff)
		}
	}
	for id, snap := range before {
		if after := snapshot(t, m.dir(t, id)); !reflect.DeepEqual(after, snap) {
			t.Fatalf("%s changed by a refused sync", id)
		}
	}
	if _, err := os.Stat(m.SourceDir()); err == nil {
		if entries, _ := os.ReadDir(m.SourceDir()); len(entries) != 0 {
			t.Fatal("a refused sync imported something")
		}
	}

	// Choosing Codex's version replaces Claude's copy, after backing it up.
	res, err = m.Sync(SyncOptions{Names: []string{"demo"}, From: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(res); !reflect.DeepEqual(got, map[string]string{"demo@mcpcell": "import", "demo@claude": "replace", "demo@codex": "replace"}) {
		t.Fatalf("actions %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(m.dir(t, "claude"), "demo", "SKILL.md")); !strings.Contains(string(b), "New") {
		t.Fatalf("claude sees %q", b)
	}
	if _, err := m.Restore(RestoreOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	for id, snap := range before {
		if after := snapshot(t, m.dir(t, id)); !reflect.DeepEqual(after, snap) {
			t.Fatalf("%s not restored:\n%v\n%v", id, snap, after)
		}
	}
}

func TestSourceVersionLeavesDifferingCopyAlone(t *testing.T) {
	m, _ := fixture(t, ".claude", ".agents", ".gemini")
	mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("V1")})
	if _, err := m.Sync(SyncOptions{Names: []string{"demo"}}); err != nil {
		t.Fatal(err)
	}
	mkSkill(t, m.dir(t, "agents"), "demo", old, map[string]string{"SKILL.md": skillMD("V1")})
	mkSkill(t, m.dir(t, "gemini"), "demo", old.Add(time.Hour), map[string]string{"SKILL.md": skillMD("Local edit")})
	gemini := snapshot(t, m.dir(t, "gemini"))

	res, err := m.Sync(SyncOptions{Names: []string{"demo"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"demo@claude": "ok", "demo@agents": "replace", "demo@gemini": "conflict"}
	if got := actions(res); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions %v", got)
	}
	for _, a := range res.Actions {
		if a.Location == "gemini" && (a.Match != "newer" || a.Diff.Summary() != "改动 SKILL.md") {
			t.Fatalf("gemini %+v %q", a, a.Diff.Summary())
		}
	}
	if !reflect.DeepEqual(snapshot(t, m.dir(t, "gemini")), gemini) {
		t.Fatal("the differing copy was touched")
	}

	// Taking Gemini's edit into the source keeps the old source as a backup.
	res, err = m.Sync(SyncOptions{Names: []string{"demo"}, From: "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(res); got["demo@mcpcell"] != "update-source" || got["demo@gemini"] != "replace" {
		t.Fatalf("actions %v", got)
	}
	var backup string
	for _, a := range res.Actions {
		if a.Location == SourceID {
			backup = a.Backup
		}
	}
	if b, err := os.ReadFile(filepath.Join(backup, "SKILL.md")); err != nil || !strings.Contains(string(b), "V1") {
		t.Fatalf("old source backup %q %v", b, err)
	}
	if b, _ := os.ReadFile(filepath.Join(m.dir(t, "claude"), "demo", "SKILL.md")); !strings.Contains(string(b), "Local edit") {
		t.Fatalf("claude should see the new source: %q", b)
	}
}

func TestOtherToolsLinkNeedsExplicitTo(t *testing.T) {
	m, user := fixture(t, ".claude", ".gemini")
	other := mkSkill(t, filepath.Join(user, ".othertool", "skills"), "demo", old, map[string]string{"SKILL.md": skillMD("Vendor")})
	mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("Vendor")})
	if err := os.MkdirAll(m.dir(t, "gemini"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(m.dir(t, "gemini"), "demo")
	if err := makeJunction(other, link); err != nil {
		t.Fatal(err)
	}

	res, err := m.Sync(SyncOptions{Names: []string{"demo"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(res); got["demo@gemini"] != "skip" || got["demo@claude"] != "replace" {
		t.Fatalf("actions %v", got)
	}
	if target, _ := readLink(link); !samePath(target, other) {
		t.Fatal("another tool's link was replaced without being asked")
	}

	res, err = m.Sync(SyncOptions{Names: []string{"demo"}, To: []string{"gemini"}})
	if err != nil || actions(res)["demo@gemini"] != "replace" {
		t.Fatalf("explicit --to: %v %v", actions(res), err)
	}
	if target, _ := readLink(link); !samePath(target, filepath.Join(m.SourceDir(), "demo")) {
		t.Fatalf("gemini -> %s", target)
	}

	if _, err := m.Restore(RestoreOptions{Names: []string{"demo"}, Locations: []string{"gemini"}}); err != nil {
		t.Fatal(err)
	}
	if target, err := readLink(link); err != nil || !samePath(target, other) {
		t.Fatalf("original link not recreated: %s %v", target, err)
	}
	if st, _ := m.load(); len(st.Records) != 1 || st.Records[0].Location != "claude" {
		t.Fatalf("only gemini should be restored: %+v", st.Records)
	}
}

func TestChangedAfterSyncIsLeftAlone(t *testing.T) {
	m, _ := fixture(t, ".claude")
	p := mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("Demo")})
	if _, err := m.Sync(SyncOptions{Names: []string{"demo"}, To: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	// The user (or an installer) puts a real directory back in its place.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("Mine now")})
	mine := snapshot(t, m.dir(t, "claude"))

	st, _ := m.Scan("demo")
	if c := copyIn(find(t, st, "demo"), "claude"); !c.Managed || !c.Changed {
		t.Fatalf("copy %+v", c)
	}
	res, err := m.Sync(SyncOptions{Names: []string{"demo"}, From: SourceID})
	if err != nil || actions(res)["demo@claude"] != "skip" {
		t.Fatalf("sync %v %v", actions(res), err)
	}
	res, err = m.Restore(RestoreOptions{All: true})
	if err != nil || actions(res)["demo@claude"] != "conflict" {
		t.Fatalf("restore %v %v", actions(res), err)
	}
	if !reflect.DeepEqual(snapshot(t, m.dir(t, "claude")), mine) {
		t.Fatal("a changed entry was touched")
	}
}

func TestCopyMode(t *testing.T) {
	m, _ := fixture(t, ".claude", ".agents")
	mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("V1")})
	if _, err := m.Sync(SyncOptions{Names: []string{"demo"}, To: []string{"claude", "agents"}, Copy: true}); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(m.dir(t, "agents"), "demo")
	if fi, err := os.Lstat(agents); err != nil || isLink(fi) || !fi.IsDir() {
		t.Fatalf("want a copy: %v %v", fi, err)
	}
	st, _ := m.Scan("demo")
	if s := find(t, st, "demo"); s.State != "synced" {
		t.Fatalf("copies of the source count as synced: %+v", s)
	}
	// A new source version refreshes MCPcell's copies on the next sync.
	if err := os.WriteFile(filepath.Join(m.SourceDir(), "demo", "SKILL.md"), []byte(skillMD("V2")), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := m.Sync(SyncOptions{Names: []string{"demo"}})
	if err != nil || actions(res)["demo@agents"] != "update" {
		t.Fatalf("refresh %v %v", actions(res), err)
	}
	if b, _ := os.ReadFile(filepath.Join(agents, "SKILL.md")); !strings.Contains(string(b), "V2") {
		t.Fatalf("copy not refreshed: %q", b)
	}
	// Once the user edits a copy, MCPcell stops touching it.
	if err := os.WriteFile(filepath.Join(agents, "SKILL.md"), []byte(skillMD("hand edit")), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ = m.Sync(SyncOptions{Names: []string{"demo"}})
	if actions(res)["demo@agents"] != "skip" {
		t.Fatalf("edited copy: %v", actions(res))
	}
	res, _ = m.Restore(RestoreOptions{Names: []string{"demo"}})
	if got := actions(res); got["demo@agents"] != "conflict" || got["demo@claude"] != "restore" {
		t.Fatalf("restore %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(agents, "SKILL.md")); !strings.Contains(string(b), "hand edit") {
		t.Fatal("edited copy was removed")
	}
	if b, _ := os.ReadFile(filepath.Join(m.dir(t, "claude"), "demo", "SKILL.md")); !strings.Contains(string(b), "V1") {
		t.Fatalf("claude original not back: %q", b)
	}
}

func TestToNewLocation(t *testing.T) {
	m, _ := fixture(t, ".claude", ".cursor")
	mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("Demo")})
	res, err := m.Sync(SyncOptions{Names: []string{"demo"}, To: []string{"cursor", "copilot"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"demo@mcpcell": "import", "demo@cursor": "link", "demo@copilot": "skip"}
	if got := actions(res); !reflect.DeepEqual(got, want) {
		t.Fatalf("plan %v", got)
	}
	if _, err := os.Stat(m.SourceDir()); err == nil {
		t.Fatal("a dry run wrote something")
	}
	if _, err := m.Sync(SyncOptions{Names: []string{"demo"}, To: []string{"cursor"}}); err != nil {
		t.Fatal(err)
	}
	cursor := m.dir(t, "cursor")
	if target, err := readLink(filepath.Join(cursor, "demo")); err != nil || !samePath(target, filepath.Join(m.SourceDir(), "demo")) {
		t.Fatalf("cursor link %s %v", target, err)
	}
	// Claude's copy was not named, so it stays a separate (identical) copy.
	if fi, _ := os.Lstat(filepath.Join(m.dir(t, "claude"), "demo")); isLink(fi) {
		t.Fatal("an unnamed location was changed")
	}
	if _, err := m.Restore(RestoreOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(cursor); !os.IsNotExist(err) {
		t.Fatalf("the skills directory the sync created should be gone: %v", err)
	}
}

func TestTreeHashAndDescription(t *testing.T) {
	dir := t.TempDir()
	a := mkSkill(t, dir, "a", old, map[string]string{"SKILL.md": "---\nname: a\ndescription: >\n  Folded\n  text\n---\n", "x.py": "1"})
	b := mkSkill(t, dir, "b", old.Add(time.Hour), map[string]string{"SKILL.md": "---\nname: a\ndescription: >\n  Folded\n  text\n---\n", "x.py": "1",
		"__pycache__/x.cpython-312.pyc": "junk", "Thumbs.db": "junk"})
	ta, err := readTree(a)
	if err != nil {
		t.Fatal(err)
	}
	tb, _ := readTree(b)
	if ta.Hash != tb.Hash || len(tb.Files) != 2 {
		t.Fatalf("caches should not count: %v %v", ta.Files, tb.Files)
	}
	if d := description(a); d != "Folded text" {
		t.Fatalf("description %q", d)
	}
	if err := copyTree(b, filepath.Join(dir, "c")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot(t, b), snapshot(t, filepath.Join(dir, "c"))) {
		t.Fatal("copyTree should keep bytes and times")
	}
}

func TestRejectsBadNames(t *testing.T) {
	m, _ := fixture(t, ".claude")
	for _, n := range []string{"../x", `a\b`, ".hidden", "", "c:x"} {
		if _, err := m.Sync(SyncOptions{Names: []string{n}}); err == nil {
			t.Fatalf("%q accepted", n)
		}
	}
	if _, err := m.Sync(SyncOptions{Names: []string{"x"}, To: []string{"nowhere"}}); err == nil {
		t.Fatal("unknown location accepted")
	}
	if _, err := m.Sync(SyncOptions{}); err == nil {
		t.Fatal("sync without names accepted")
	}
}

func TestRelinkKeepsTheFirstOriginal(t *testing.T) {
	m, _ := fixture(t, ".claude")
	mkSkill(t, m.dir(t, "claude"), "demo", old, map[string]string{"SKILL.md": skillMD("Demo"), "a.txt": "a"})
	before := snapshot(t, m.dir(t, "claude"))
	if _, err := m.Sync(SyncOptions{Names: []string{"demo"}}); err != nil {
		t.Fatal(err)
	}
	// Someone deletes MCPcell's link; syncing again relinks it.
	if err := os.Remove(filepath.Join(m.dir(t, "claude"), "demo")); err != nil {
		t.Fatal(err)
	}
	res, err := m.Sync(SyncOptions{Names: []string{"demo"}, To: []string{"claude"}})
	if err != nil || actions(res)["demo@claude"] != "link" {
		t.Fatalf("relink %v %v", actions(res), err)
	}
	if _, err := m.Restore(RestoreOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, m.dir(t, "claude")); !reflect.DeepEqual(after, before) {
		t.Fatalf("the first original is lost:\n%v\n%v", before, after)
	}
}

func TestSyncAllTakesSkillsInSeveralPlaces(t *testing.T) {
	m, _ := fixture(t, ".claude", ".agents")
	same := map[string]string{"SKILL.md": skillMD("Same")}
	mkSkill(t, m.dir(t, "claude"), "same", old, same)
	mkSkill(t, m.dir(t, "agents"), "same", old, same)
	mkSkill(t, m.dir(t, "claude"), "differs", old, map[string]string{"SKILL.md": skillMD("One")})
	mkSkill(t, m.dir(t, "agents"), "differs", old, map[string]string{"SKILL.md": skillMD("Two")})
	mkSkill(t, m.dir(t, "claude"), "single", old, same)
	res, err := m.Sync(SyncOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"same@mcpcell": "import", "same@claude": "replace", "same@agents": "replace",
		"differs@claude": "conflict", "differs@agents": "conflict"}
	if got := actions(res); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions %v", got)
	}
}

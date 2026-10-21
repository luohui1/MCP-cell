package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/skills"
)

// skillHome is a fake user with Claude Code and Codex installed; nothing
// outside the test's temp dir is read or written.
func skillHome(t *testing.T) (*skills.Manager, string) {
	t.Helper()
	user, home := t.TempDir(), t.TempDir()
	for _, p := range []string{user, home} {
		if !strings.HasPrefix(p, os.TempDir()) {
			t.Fatalf("refusing to use %s outside the temp dir", p)
		}
	}
	m := skills.New(home, user)
	for _, l := range m.Locations {
		if !strings.HasPrefix(l.Path, user) {
			t.Fatalf("location %s outside the fake home: %s", l.ID, l.Path)
		}
	}
	return m, user
}

func writeSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\ndescription: "+body+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsCommand(t *testing.T) {
	m, user := skillHome(t)
	writeSkill(t, filepath.Join(user, ".claude", "skills", "demo"), "Demo")
	writeSkill(t, filepath.Join(user, ".codex", "skills", "demo"), "Demo")
	writeSkill(t, filepath.Join(user, ".codex", "skills", "other"), "Other")

	var out bytes.Buffer
	if err := skillsWith(&out, nil, m, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"demo", "相同", "other", "单处", "2 个 Skill"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("overview lacks %q:\n%s", want, out.String())
		}
	}

	// Without -y the plan is shown and nothing happens unless confirmed.
	out.Reset()
	if err := skillsWith(&out, strings.NewReader("n\n"), m, []string{"sync", "demo"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "备份后改为链接") || !strings.Contains(out.String(), "已取消") {
		t.Fatalf("plan:\n%s", out.String())
	}
	if _, err := os.Stat(m.SourceDir()); err == nil {
		t.Fatal("a cancelled sync wrote the source")
	}

	out.Reset()
	if err := skillsWith(&out, nil, m, []string{"sync", "demo", "-y"}); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	out.Reset()
	if err := skillsWith(&out, nil, m, []string{"demo"}); err != nil || !strings.Contains(out.String(), "已统一") {
		t.Fatalf("detail %v:\n%s", err, out.String())
	}

	out.Reset()
	if err := skillsWith(&out, nil, m, []string{"restore", "--all", "-y"}); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if fi, err := os.Lstat(filepath.Join(user, ".claude", "skills", "demo")); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("not restored: %v %v", fi, err)
	}
}

func TestSkillsSyncReportsConflicts(t *testing.T) {
	m, user := skillHome(t)
	writeSkill(t, filepath.Join(user, ".claude", "skills", "demo"), "One")
	writeSkill(t, filepath.Join(user, ".codex", "skills", "demo"), "Two")
	var out bytes.Buffer
	err := skillsWith(&out, nil, m, []string{"sync", "demo", "-y"})
	if err == nil || !strings.Contains(out.String(), "--from") || !strings.Contains(out.String(), "改动 SKILL.md") {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if _, err := parseSkillFlags("sync", nil); err == nil {
		t.Fatal("sync without names accepted")
	}
	o, err := parseSkillFlags("sync", []string{"a", "--to", "claude,agents", "b", "--from", "codex", "--copy"})
	if err != nil || strings.Join(o.names, ",") != "a,b" || strings.Join(o.to, ",") != "claude,agents" || o.from != "codex" || !o.copy {
		t.Fatalf("parse %+v %v", o, err)
	}
}

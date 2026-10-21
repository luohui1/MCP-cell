package httpd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/skills"
)

func TestSkillsAPI(t *testing.T) {
	// Skill locations follow these; keep them off the real directories.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	user, home := t.TempDir(), t.TempDir()
	h := start(t)
	h.srv.opts.Skills = skills.New(home, user)
	for _, l := range h.srv.opts.Skills.Locations {
		if !strings.HasPrefix(l.Path, user) {
			t.Fatalf("location %s outside the temp home: %s", l.ID, l.Path)
		}
	}
	for _, agent := range []string{".claude", ".agents"} {
		dir := filepath.Join(user, agent, "skills", "demo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: Demo\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	code, body := h.apiCall(t, "GET", "/api/skills", "")
	var st skills.Status
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil || len(st.Skills) != 1 || st.Skills[0].State != "same" {
		t.Fatalf("status %d %s", code, body)
	}

	sync := func(body string) skills.Result {
		t.Helper()
		code, out := h.apiCall(t, "POST", "/api/skills/sync", body)
		var res skills.Result
		if code != 200 || json.Unmarshal([]byte(out), &res) != nil {
			t.Fatalf("sync %d %s", code, out)
		}
		return res
	}
	if res := sync(`{"names":["demo"],"dryRun":true}`); res.Changes() != 3 || !res.DryRun {
		t.Fatalf("plan %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "skills")); err == nil {
		t.Fatal("a dry run wrote the source")
	}
	if res := sync(`{"names":["demo"]}`); res.Changes() != 3 {
		t.Fatalf("sync %+v", res)
	}
	if code, body := h.apiCall(t, "POST", "/api/skills/sync", `{"names":["../x"]}`); code != 400 {
		t.Fatalf("bad name: %d %s", code, body)
	}

	code, body = h.apiCall(t, "POST", "/api/skills/restore", `{"all":true}`)
	var res skills.Result
	if code != 200 || json.Unmarshal([]byte(body), &res) != nil || res.Changes() != 2 {
		t.Fatalf("restore %d %s", code, body)
	}
	for _, a := range res.Actions {
		if !a.Done {
			t.Fatalf("not restored: %+v", a)
		}
	}
}

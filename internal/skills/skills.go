// Package skills keeps one copy of each agent skill (a folder with SKILL.md)
// in MCPcell's source directory and points the agents' skill directories at
// it, usually with a directory link. It reports where each skill is and
// whether the copies agree, backs up whatever a sync replaces, and restores
// it byte for byte. A copy that differs from the chosen version is never
// overwritten unless the user picks which version wins; an entry changed
// after a sync is never touched.
package skills

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Location is one directory agents read user-level skills from.
type Location struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	// Agents are the agents that read this directory.
	Agents []string `json:"agents"`
	// Note says how sure we are, or what else to know.
	Note string `json:"note,omitempty"`
	// Exists: the directory is there. Installed: the agent's own directory
	// is there, so a sync may create the skills directory.
	Exists    bool `json:"exists"`
	Installed bool `json:"installed"`
	root      string
}

// Locations lists the user-level skill directories of the agents MCPcell
// knows, for the user whose home directory is home. Claude Code follows
// CLAUDE_CONFIG_DIR and Codex follows CODEX_HOME, as for their MCP configs.
func Locations(home string) []Location {
	claude := filepath.Join(home, ".claude")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		claude = d
	}
	codex := filepath.Join(home, ".codex")
	if d := os.Getenv("CODEX_HOME"); d != "" {
		codex = d
	}
	gemini := filepath.Join(home, ".gemini")
	locs := []Location{
		{ID: "claude", Name: "Claude Code", root: claude, Path: filepath.Join(claude, "skills"),
			Agents: []string{"Claude Code", "Cursor", "OpenCode"}},
		{ID: "agents", Name: "通用 .agents", root: filepath.Join(home, ".agents"), Path: filepath.Join(home, ".agents", "skills"),
			Agents: []string{"Codex", "Gemini CLI", "Cursor", "OpenCode", "GitHub Copilot"},
			Note:   "多个 Agent 共同读取的用户级位置"},
		{ID: "codex", Name: "Codex", root: codex, Path: filepath.Join(codex, "skills"),
			Agents: []string{"Codex", "Cursor"}, Note: "Codex 的旧位置，仍会读取；.system 是 Codex 自带的"},
		{ID: "gemini", Name: "Gemini CLI", root: gemini, Path: filepath.Join(gemini, "skills"),
			Agents: []string{"Gemini CLI"}, Note: "同名时 ~/.agents/skills 优先"},
		{ID: "antigravity", Name: "Antigravity", root: filepath.Join(gemini, "config"), Path: filepath.Join(gemini, "config", "skills"),
			Agents: []string{"Antigravity"}},
		{ID: "antigravity-ide", Name: "Antigravity IDE", root: filepath.Join(gemini, "antigravity"), Path: filepath.Join(gemini, "antigravity", "skills"),
			Agents: []string{"Antigravity IDE"}},
		{ID: "antigravity-cli", Name: "Antigravity CLI", root: filepath.Join(gemini, "antigravity-cli"), Path: filepath.Join(gemini, "antigravity-cli", "skills"),
			Agents: []string{"Antigravity CLI"}, Note: "未在官方文档中核实"},
		{ID: "cursor", Name: "Cursor", root: filepath.Join(home, ".cursor"), Path: filepath.Join(home, ".cursor", "skills"),
			Agents: []string{"Cursor"}},
		{ID: "opencode", Name: "OpenCode", root: filepath.Join(home, ".config", "opencode"), Path: filepath.Join(home, ".config", "opencode", "skills"),
			Agents: []string{"OpenCode"}},
		{ID: "copilot", Name: "GitHub Copilot", root: filepath.Join(home, ".copilot"), Path: filepath.Join(home, ".copilot", "skills"),
			Agents: []string{"GitHub Copilot"}},
	}
	refresh(locs)
	return locs
}

// refresh checks which locations exist now.
func refresh(locs []Location) {
	for i := range locs {
		locs[i].Exists = isDir(locs[i].Path)
		locs[i].Installed = locs[i].Exists || isDir(locs[i].root)
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// Manager works on one MCPcell home (the source directory, skills.json and
// skill-backups live there) and a set of agent skill locations.
type Manager struct {
	Home      string
	Locations []Location
}

// New returns a Manager for MCPcell home mcpcellHome and the user whose home
// directory is userHome.
func New(mcpcellHome, userHome string) *Manager {
	return &Manager{Home: mcpcellHome, Locations: Locations(userHome)}
}

// Dirs reports the locations as they are now.
func (m *Manager) Dirs() []Location {
	refresh(m.Locations)
	return append([]Location(nil), m.Locations...)
}

// SourceDir holds the one copy of every skill MCPcell manages.
func (m *Manager) SourceDir() string { return filepath.Join(m.Home, "skills") }

func (m *Manager) backupDir() string { return filepath.Join(m.Home, "skill-backups") }

func (m *Manager) location(id string) (Location, bool) {
	for _, l := range m.Locations {
		if l.ID == id {
			return l, true
		}
	}
	return Location{}, false
}

// SourceID names the source directory where a location ID is expected
// (sync --from mcpcell).
const SourceID = "mcpcell"

var errBadName = errors.New("Skill 名称无效")

// checkName rejects names that are not a single plain directory name.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") ||
		strings.ContainsAny(name, `/\:`) || filepath.Base(name) != name {
		return errBadName
	}
	return nil
}

// samePath compares paths the way the file system does.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// pathKey is a map key that equal paths share.
func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

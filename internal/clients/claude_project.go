package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Claude Code keeps servers per project too, in two places:
//
//   - the local scope, private to the user: projects["<dir>"].mcpServers in
//     ~/.claude.json, which `claude mcp add` writes by default. Claude Code
//     keys it by the directory it runs in (a git repository's root), so the
//     CLI changes it when run from that directory.
//   - the project scope: <dir>/.mcp.json, checked into the repository and
//     shared with everyone who clones it. Claude Code only starts these once
//     the user approves them (enabledMcpjsonServers).
//
// Each such project is a client of its own, "claude-code@<dir>", so its
// entries, takeovers and automatic takeover are kept apart from the user's
// and from other projects', exactly like another agent's.
//
// MCPcell never edits .mcp.json: it is shared, and an entry pointing at this
// machine's mcpcell-run would break it for everyone else. A project-scope
// server is taken over by adding a local-scope entry of the same name, which
// Claude Code prefers over the project's; restoring removes that entry and
// the shared one applies again.

const claudeProjectPrefix = "claude-code@"

// ProjectDir returns the project directory of a Claude Code project client
// id, or false for any other id.
func ProjectDir(id string) (string, bool) {
	return strings.CutPrefix(id, claudeProjectPrefix)
}

// ClaudeProject is one Claude Code project's servers.
type ClaudeProject struct {
	c   *ClaudeCode
	dir string // the key in ~/.claude.json, with forward slashes
}

// Project returns the client for the project at dir.
func (c *ClaudeCode) Project(dir string) *ClaudeProject {
	return &ClaudeProject{c: c, dir: filepath.ToSlash(dir)}
}

// Projects returns a client for every project that has servers of its own:
// local-scope entries, or a .mcp.json that lists some. Older Claude Code
// versions keyed projects with backslashes, so one directory can appear
// twice; it is one project, keyed with forward slashes as Claude Code now
// does (and as its CLI writes the local scope).
func (c *ClaudeCode) Projects() []Client {
	doc, err := c.read()
	if err != nil || doc == nil {
		return nil
	}
	seen := map[string]bool{}
	var dirs []string
	for key := range doc.Projects {
		dir := filepath.ToSlash(key)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if len(doc.Projects[dir].MCPServers) > 0 || listsServers(filepath.Join(filepath.FromSlash(dir), ".mcp.json")) {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	out := make([]Client, len(dirs))
	for i, dir := range dirs {
		out[i] = c.Project(dir)
	}
	return out
}

// listsServers reports whether a .mcp.json has servers, or cannot be read
// (the error is then shown with the project).
func listsServers(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	servers, err := mcpServers(path)
	return err != nil || len(servers) > 0
}

func (p *ClaudeProject) ID() string { return claudeProjectPrefix + p.dir }

func (p *ClaudeProject) Name() string {
	return "Claude Code · " + filepath.Base(filepath.FromSlash(p.dir))
}

// Path is the file MCPcell edits for the project: ~/.claude.json, where its
// local scope lives.
func (p *ClaudeProject) Path() string { return p.c.path }

// Dir is the project directory.
func (p *ClaudeProject) Dir() string { return filepath.FromSlash(p.dir) }

func (p *ClaudeProject) local() claudeScope {
	return claudeScope{c: p.c, id: p.ID(), scope: "local", dir: p.Dir()}
}

func (p *ClaudeProject) state() (claudeProjectDoc, error) {
	doc, err := p.c.read()
	if err != nil || doc == nil {
		return claudeProjectDoc{}, err
	}
	return doc.Projects[p.dir], nil
}

// shared returns the project's .mcp.json servers; nil when it has none.
func (p *ClaudeProject) shared() (map[string]json.RawMessage, error) {
	servers, err := mcpServers(filepath.Join(p.Dir(), ".mcp.json"))
	if errors.Is(err, errSyntax) {
		return nil, fmt.Errorf("%s 的 .mcp.json 格式不对", filepath.Base(p.Dir()))
	}
	return servers, err
}

// placeholder finds ${VAR} and ${VAR:-default}, which Claude Code expands in
// .mcp.json from its own environment.
var placeholder = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*(:-[^}]*)?\}`)

// List returns the local-scope entries, then the .mcp.json entries that no
// local entry of the same name overrides.
func (p *ClaudeProject) List() ([]Entry, error) {
	st, err := p.state()
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for name, raw := range st.MCPServers {
		e := entryFromJSON(p.ID(), name, raw)
		e.Enabled = !contains(st.Disabled, name)
		entries = append(entries, e)
	}
	shared, err := p.shared()
	if err != nil {
		return entries, err
	}
	answers := p.answers(st)
	for name, raw := range shared {
		if _, overridden := st.MCPServers[name]; overridden {
			continue
		}
		e := entryFromJSON(p.ID(), name, raw)
		e.Shared = true
		e.Enabled = answers.approved(name)
		switch {
		case !e.Enabled:
			e.Hold = "项目 .mcp.json 里的，还没在 Claude Code 里同意启用"
		case placeholder.Match(raw):
			e.Hold = "用了 ${…} 变量，由 Claude Code 自己展开，MCPcell 拿不到这些值"
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// mcpjsonAnswers are the user's answers about a project's .mcp.json servers.
type mcpjsonAnswers struct {
	All      bool     `json:"enableAllProjectMcpServers"`
	Approved []string `json:"enabledMcpjsonServers"`
	Rejected []string `json:"disabledMcpjsonServers"`
}

func (a mcpjsonAnswers) approved(name string) bool {
	return (a.All || contains(a.Approved, name)) && !contains(a.Rejected, name)
}

// answers gathers them from everywhere Claude Code keeps them: the project's
// .claude/settings.local.json and .claude/settings.json, the user's
// settings.json, and the project's part of ~/.claude.json, where older
// versions kept them (Claude Code moves them to settings.local.json the next
// time it writes the project's local scope).
func (p *ClaudeProject) answers(st claudeProjectDoc) mcpjsonAnswers {
	out := mcpjsonAnswers{Approved: st.Approved, Rejected: st.Rejected}
	for _, path := range []string{
		filepath.Join(p.Dir(), ".claude", "settings.local.json"),
		filepath.Join(p.Dir(), ".claude", "settings.json"),
		p.c.settings,
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var a mcpjsonAnswers
		if json.Unmarshal(stripJSONC(b), &a) != nil {
			continue
		}
		out.All = out.All || a.All
		out.Approved = append(out.Approved, a.Approved...)
		out.Rejected = append(out.Rejected, a.Rejected...)
	}
	return out
}

// Attach takes over a local-scope entry in place, or a .mcp.json entry by
// overriding it with a local-scope one (see the package comment above).
func (p *ClaudeProject) Attach(name string, t Target) (Record, error) {
	if !hasDir(p.Dir()) {
		return Record{Client: p.ID(), Name: name}, fmt.Errorf("项目目录 %s 不在了", p.Dir())
	}
	s := p.local()
	if _, err := s.raw(name); err == nil {
		return s.attach(name, t)
	}
	rec := Record{Client: p.ID(), Name: name, Override: true, At: time.Now()}
	shared, err := p.shared()
	if err != nil {
		return rec, err
	}
	raw, ok := shared[name]
	if !ok {
		return rec, ErrNotFound
	}
	entries, _ := p.List()
	for _, e := range entries {
		if e.Name == name && e.Hold != "" {
			return rec, errors.New(e.Hold)
		}
	}
	if rec.Backup, err = s.backup(); err != nil {
		return rec, err
	}
	original, _ := json.Marshal(json.RawMessage(stripJSONC(raw)))
	rec.Original = string(original)
	if rec.Written, err = s.written(name, raw, t); err != nil {
		return rec, err
	}
	if err := s.add(name, rec.Written); err != nil {
		return rec, err
	}
	return s.keep(rec, t)
}

// ErrProjectAdd: MCPcell adds servers to the user scope, not to a project.
var ErrProjectAdd = errors.New("项目里的 MCP 只能托管或改回，不在这里新加")

func (p *ClaudeProject) Add(name string, t Target) (Record, error) {
	return Record{Client: p.ID(), Name: name}, ErrProjectAdd
}

func (p *ClaudeProject) Restore(rec Record) error {
	if !hasDir(p.Dir()) {
		return fmt.Errorf("项目目录 %s 不在了，可以在 Claude Code 里删掉这一项，或者忘记这条记录", p.Dir())
	}
	return p.local().restore(rec)
}

func hasDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// Expand returns cs with each client's projects after it.
func Expand(cs []Client) []Client {
	out := make([]Client, 0, len(cs))
	for _, c := range cs {
		out = append(out, c)
		if p, ok := c.(interface{ Projects() []Client }); ok {
			out = append(out, p.Projects()...)
		}
	}
	return out
}

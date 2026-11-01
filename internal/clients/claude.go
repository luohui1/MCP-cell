package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/proc"
)

// Runner runs the client's own CLI with args, in dir.
type Runner func(dir string, args ...string) ([]byte, error)

// ClaudeCode changes user-scope servers through `claude mcp`, because running
// Claude Code sessions keep rewriting ~/.claude.json themselves. Projects
// with servers of their own are clients of their own (see Projects).
type ClaudeCode struct {
	path, home, backups string
	run                 Runner
	settings            string // the user's settings.json

	mu    sync.Mutex
	stamp fileStamp
	doc   *claudeDoc
}

// claudeDoc is what MCPcell reads from ~/.claude.json.
type claudeDoc struct {
	MCPServers map[string]json.RawMessage  `json:"mcpServers"`
	Disabled   []string                    `json:"disabledMcpServers"`
	Projects   map[string]claudeProjectDoc `json:"projects"`
}

// claudeProjectDoc is one project's part: its local-scope servers, the ones
// turned off in it, and its answers about the project's .mcp.json servers.
type claudeProjectDoc struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
	Disabled   []string                   `json:"disabledMcpServers"`
	Approved   []string                   `json:"enabledMcpjsonServers"`
	Rejected   []string                   `json:"disabledMcpjsonServers"`
}

type fileStamp struct {
	mod  time.Time
	size int64
}

func NewClaudeCode(home, backups string, run Runner) *ClaudeCode {
	path := filepath.Join(home, ".claude.json")
	settings := filepath.Join(home, ".claude", "settings.json")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		path = filepath.Join(d, ".claude.json")
		settings = filepath.Join(d, "settings.json")
	}
	c := &ClaudeCode{path: path, home: home, backups: backups, run: run, settings: settings}
	if c.run == nil {
		c.run = cli
	}
	return c
}

func (c *ClaudeCode) ID() string   { return "claude-code" }
func (c *ClaudeCode) Name() string { return "Claude Code" }
func (c *ClaudeCode) Path() string { return c.path }

// read parses ~/.claude.json again only when it changed: the page asks for
// every client's entries every few seconds, and the file holds every project
// Claude Code was ever opened in. A missing file is (nil, nil).
func (c *ClaudeCode) read() (*claudeDoc, error) {
	st, err := os.Stat(c.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stamp := fileStamp{st.ModTime(), st.Size()}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.doc != nil && c.stamp == stamp {
		return c.doc, nil
	}
	b, err := os.ReadFile(c.path)
	if err != nil {
		return nil, err
	}
	var doc claudeDoc
	if err := json.Unmarshal(stripJSONC(b), &doc); err != nil {
		return nil, errSyntax
	}
	c.doc, c.stamp = &doc, stamp
	return &doc, nil
}

// forget drops the parsed file after the CLI changed it, in case the change
// kept its size and fell within the clock's resolution.
func (c *ClaudeCode) forget() {
	c.mu.Lock()
	c.doc = nil
	c.mu.Unlock()
}

// List reports enablement the way Claude Code decides it: user-scope servers
// named in the top-level disabledMcpServers are off. A "disabled" key inside
// an entry is ignored by Claude Code, so it is ignored here too.
func (c *ClaudeCode) List() ([]Entry, error) {
	doc, err := c.read()
	if err != nil || doc == nil {
		return nil, err
	}
	entries := []Entry{}
	for name, raw := range doc.MCPServers {
		e := entryFromJSON(c.ID(), name, raw)
		e.Enabled = !contains(doc.Disabled, name)
		entries = append(entries, e)
	}
	return entries, nil
}

func cli(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := proc.Run(ctx, append([]string{"claude"}, args...), os.Environ(), dir)
	if err != nil {
		return out, fmt.Errorf("claude %s 失败：%v %s", strings.Join(args[:min(3, len(args))], " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// scope is the place `claude mcp ... -s <scope>` edits: the user scope, run
// from the user's directory, or a project's local scope, run from the
// project (Claude Code keys it by the directory it runs in).
type claudeScope struct {
	c     *ClaudeCode
	id    string
	scope string // user | local
	dir   string
}

func (c *ClaudeCode) user() claudeScope {
	return claudeScope{c: c, id: c.ID(), scope: "user", dir: c.home}
}

// servers returns the scope's entries as Claude Code stored them.
func (s claudeScope) servers() (map[string]json.RawMessage, error) {
	doc, err := s.c.read()
	if err != nil || doc == nil {
		return nil, err
	}
	if s.scope == "user" {
		return doc.MCPServers, nil
	}
	return doc.Projects[s.key()].MCPServers, nil
}

// key is the project's key in ~/.claude.json: its path with forward slashes.
func (s claudeScope) key() string { return filepath.ToSlash(s.dir) }

func (s claudeScope) raw(name string) (json.RawMessage, error) {
	servers, err := s.servers()
	if err != nil {
		return nil, err
	}
	raw, ok := servers[name]
	if !ok {
		return nil, ErrNotFound
	}
	return raw, nil
}

func (s claudeScope) add(name, value string) error {
	defer s.c.forget()
	_, err := s.c.run(s.dir, "mcp", "add-json", "-s", s.scope, name, value)
	return err
}

func (s claudeScope) remove(name string) error {
	defer s.c.forget()
	_, err := s.c.run(s.dir, "mcp", "remove", "-s", s.scope, name)
	return err
}

// replace swaps entry name for value via the CLI, putting the old one back
// if the add half fails after the remove.
func (s claudeScope) replace(name, value, old string) error {
	if err := s.remove(name); err != nil {
		return err
	}
	if err := s.add(name, value); err != nil {
		if _, missing := s.raw(name); missing != nil && old != "" {
			_ = s.add(name, old)
		}
		return err
	}
	return nil
}

func (s claudeScope) backup() (string, error) {
	data, err := os.ReadFile(s.c.path)
	if err != nil {
		return "", err
	}
	return keepBackup(s.c.backups, s.c.ID(), s.c.path, data)
}

// written is what a takeover writes for raw: the wrapped entry, or an
// endpoint entry when mcpcell-run is not installed.
func (s claudeScope) written(name string, raw json.RawMessage, t Target) (string, error) {
	written, _ := json.Marshal(httpEntry(true, t.URL, t.Token))
	if t.wrapped() {
		o, err := wrapJSON(raw, t.Wrapper, entryFromJSON(s.id, name, raw))
		if err != nil {
			return "", err
		}
		written, _ = marshalPlain(o)
	}
	return string(written), nil
}

func (s claudeScope) attach(name string, t Target) (Record, error) {
	rec := Record{Client: s.id, Name: name, At: time.Now()}
	raw, err := s.raw(name)
	if err != nil {
		return rec, err
	}
	if rec.Backup, err = s.backup(); err != nil {
		return rec, err
	}
	original, _ := json.Marshal(json.RawMessage(stripJSONC(raw))) // compact
	rec.Original = string(original)
	if rec.Written, err = s.written(name, raw, t); err != nil {
		return rec, err
	}
	if err := s.replace(name, rec.Written, rec.Original); err != nil {
		return rec, err
	}
	return s.keep(rec, t)
}

// saved confirms Claude Code saved what MCPcell asked for, and returns it as
// stored (the CLI may add defaults such as "type"): that is what a restore
// later expects to find.
func (s claudeScope) saved(name string, t Target) (string, error) {
	now, err := s.raw(name)
	if err != nil || !t.applied(entryFromJSON(s.id, name, now)) {
		return "", fmt.Errorf("Claude Code 没有保存新的 %s 配置", name)
	}
	b, _ := json.Marshal(json.RawMessage(stripJSONC(now)))
	return string(b), nil
}

// keep records what Claude Code saved for the entry.
func (s claudeScope) keep(rec Record, t Target) (Record, error) {
	saved, err := s.saved(rec.Name, t)
	if err == nil {
		rec.Written = saved
	}
	return rec, err
}

func (s claudeScope) restore(rec Record) error {
	cur, err := s.raw(rec.Name)
	if err != nil {
		return err
	}
	if !sameJSON(cur, []byte(rec.Written)) {
		return ErrConflict
	}
	if rec.Added || rec.Override {
		if err := s.remove(rec.Name); err != nil {
			return err
		}
		if _, err := s.raw(rec.Name); err == nil {
			return fmt.Errorf("Claude Code 没有移除 %s", rec.Name)
		}
		return nil
	}
	if err := s.replace(rec.Name, rec.Original, ""); err != nil {
		return err
	}
	now, err := s.raw(rec.Name)
	if err != nil || !sameEntry(entryFromJSON(s.id, rec.Name, now), entryFromJSON(s.id, rec.Name, []byte(rec.Original))) {
		return fmt.Errorf("Claude Code 没有恢复原来的 %s 配置，原始内容在 %s", rec.Name, rec.Backup)
	}
	return nil
}

func (c *ClaudeCode) Attach(name string, t Target) (Record, error) { return c.user().attach(name, t) }

func (c *ClaudeCode) Add(name string, t Target) (Record, error) {
	s := c.user()
	rec := Record{Client: c.ID(), Name: name, Added: true, At: time.Now()}
	if _, err := s.raw(name); err == nil {
		return rec, ErrExists
	}
	if _, err := os.Stat(c.path); err == nil {
		var err error
		if rec.Backup, err = s.backup(); err != nil {
			return rec, err
		}
	}
	written, _ := json.Marshal(httpEntry(true, t.URL, t.Token))
	if t.wrapped() {
		written, _ = marshalPlain(newJSON(styleTyped, t.Wrapper, t.Server))
	}
	rec.Written = string(written)
	if err := s.add(name, rec.Written); err != nil {
		return rec, err
	}
	return s.keep(rec, t)
}

func (c *ClaudeCode) Restore(rec Record) error { return c.user().restore(rec) }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// sameEntry compares what matters for launching; the CLI may add defaults
// such as "type": "stdio".
func sameEntry(a, b Entry) bool {
	return a.Command == b.Command && reflect.DeepEqual(nilIfEmpty(a.Args), nilIfEmpty(b.Args)) &&
		reflect.DeepEqual(nilIfEmptyMap(a.Env), nilIfEmptyMap(b.Env)) && a.URL == b.URL && a.Cwd == b.Cwd
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func nilIfEmptyMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/luohui1/MCP-cell/internal/proc"
)

// Runner runs the client's own CLI with args.
type Runner func(args ...string) ([]byte, error)

// ClaudeCode changes user-scope servers through `claude mcp`, because running
// Claude Code sessions keep rewriting ~/.claude.json themselves.
type ClaudeCode struct {
	path, home, backups string
	run                 Runner
}

func NewClaudeCode(home, backups string, run Runner) *ClaudeCode {
	path := filepath.Join(home, ".claude.json")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		path = filepath.Join(d, ".claude.json")
	}
	c := &ClaudeCode{path: path, home: home, backups: backups, run: run}
	if c.run == nil {
		c.run = c.cli
	}
	return c
}

func (c *ClaudeCode) ID() string   { return "claude-code" }
func (c *ClaudeCode) Name() string { return "Claude Code" }
func (c *ClaudeCode) Path() string { return c.path }

// List reports enablement the way Claude Code decides it: user-scope servers
// named in the top-level disabledMcpServers are off. A "disabled" key inside
// an entry is ignored by Claude Code, so it is ignored here too.
func (c *ClaudeCode) List() ([]Entry, error) {
	entries, err := listJSON(c.ID(), c.path)
	if err != nil || entries == nil {
		return entries, err
	}
	var doc struct {
		Disabled []string `json:"disabledMcpServers"`
	}
	if b, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(stripJSONC(b), &doc)
	}
	off := map[string]bool{}
	for _, n := range doc.Disabled {
		off[n] = true
	}
	for i := range entries {
		entries[i].Enabled = !off[entries[i].Name]
	}
	return entries, nil
}

func (c *ClaudeCode) cli(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := proc.Run(ctx, append([]string{"claude"}, args...), os.Environ(), c.home)
	if err != nil {
		return out, fmt.Errorf("claude %s 失败：%v %s", strings.Join(args[:min(3, len(args))], " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (c *ClaudeCode) raw(name string) (json.RawMessage, error) {
	servers, err := mcpServers(c.path)
	if err != nil {
		return nil, err
	}
	raw, ok := servers[name]
	if !ok {
		return nil, ErrNotFound
	}
	return raw, nil
}

// replace swaps the user-scope entry name for value via the CLI.
func (c *ClaudeCode) replace(name string, value string) error {
	if _, err := c.run("mcp", "remove", "-s", "user", name); err != nil {
		return err
	}
	_, err := c.run("mcp", "add-json", "-s", "user", name, value)
	return err
}

func (c *ClaudeCode) Attach(name string, t Target) (Record, error) {
	rec := Record{Client: c.ID(), Name: name, At: time.Now()}
	raw, err := c.raw(name)
	if err != nil {
		return rec, err
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return rec, err
	}
	if rec.Backup, err = keepBackup(c.backups, c.ID(), c.path, data); err != nil {
		return rec, err
	}
	original, _ := json.Marshal(json.RawMessage(stripJSONC(raw))) // compact
	written, _ := json.Marshal(httpEntry(true, t.URL, t.Token))
	if t.wrapped() {
		o, err := wrapJSON(raw, t.Wrapper, entryFromJSON(c.ID(), name, raw))
		if err != nil {
			return rec, err
		}
		written, _ = marshalPlain(o)
	}
	rec.Original, rec.Written = string(original), string(written)
	if err := c.replace(name, rec.Written); err != nil {
		// Put the original back if the add half failed after the remove.
		if _, missing := c.raw(name); missing != nil {
			_, _ = c.run("mcp", "add-json", "-s", "user", name, rec.Original)
		}
		return rec, err
	}
	if now, err := c.raw(name); err != nil || !t.applied(entryFromJSON(c.ID(), name, now)) {
		return rec, fmt.Errorf("Claude Code 没有保存新的 %s 配置", name)
	}
	return rec, nil
}

func (c *ClaudeCode) Add(name string, t Target) (Record, error) {
	rec := Record{Client: c.ID(), Name: name, Added: true, At: time.Now()}
	if _, err := c.raw(name); err == nil {
		return rec, ErrExists
	}
	if data, err := os.ReadFile(c.path); err == nil {
		if rec.Backup, err = keepBackup(c.backups, c.ID(), c.path, data); err != nil {
			return rec, err
		}
	}
	written, _ := json.Marshal(httpEntry(true, t.URL, t.Token))
	if t.wrapped() {
		written, _ = marshalPlain(newJSON(true, t.Wrapper, t.Server))
	}
	rec.Written = string(written)
	if _, err := c.run("mcp", "add-json", "-s", "user", name, rec.Written); err != nil {
		return rec, err
	}
	if now, err := c.raw(name); err != nil || !t.applied(entryFromJSON(c.ID(), name, now)) {
		return rec, fmt.Errorf("Claude Code 没有保存新的 %s 配置", name)
	}
	return rec, nil
}

func (c *ClaudeCode) Restore(rec Record) error {
	cur, err := c.raw(rec.Name)
	if err != nil {
		return err
	}
	if !sameJSON(cur, []byte(rec.Written)) {
		return ErrConflict
	}
	if rec.Added {
		if _, err := c.run("mcp", "remove", "-s", "user", rec.Name); err != nil {
			return err
		}
		if _, err := c.raw(rec.Name); err == nil {
			return fmt.Errorf("Claude Code 没有移除 %s", rec.Name)
		}
		return nil
	}
	if err := c.replace(rec.Name, rec.Original); err != nil {
		return err
	}
	now, err := c.raw(rec.Name)
	if err != nil || !sameEntry(entryFromJSON(c.ID(), rec.Name, now), entryFromJSON(c.ID(), rec.Name, []byte(rec.Original))) {
		return fmt.Errorf("Claude Code 没有恢复原来的 %s 配置，原始内容在 %s", rec.Name, rec.Backup)
	}
	return nil
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

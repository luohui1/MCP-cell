package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// jsonEntry is the shape most clients use under mcpServers.
type jsonEntry struct {
	Type     string            `json:"type"`
	Command  any               `json:"command"`
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	Cwd      string            `json:"cwd"`
	URL      string            `json:"url"`
	Disabled bool              `json:"disabled"`
}

func entryFromJSON(client, name string, raw []byte) Entry {
	var j jsonEntry
	_ = json.Unmarshal(stripJSONC(raw), &j)
	e := Entry{Client: client, Name: name, Args: j.Args, Cwd: j.Cwd, URL: j.URL, Enabled: !j.Disabled}
	switch c := j.Command.(type) {
	case string:
		e.Command = c
	case []any: // OpenCode-style command array
		for i, v := range c {
			if s, ok := v.(string); ok {
				if i == 0 {
					e.Command = s
				} else {
					e.Args = append(e.Args, s)
				}
			}
		}
	}
	e.setEnv(j.Env)
	switch {
	case e.URL != "":
		e.Kind = "http"
	case e.Command != "":
		e.Kind = "stdio"
	default:
		e.Kind = "unknown"
	}
	return e
}

// mcpServers returns the raw entries of a JSON config's mcpServers object.
func mcpServers(path string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(stripJSONC(b), &doc); err != nil {
		return nil, errSyntax
	}
	return doc.MCPServers, nil
}

func listJSON(client, path string) ([]Entry, error) {
	servers, err := mcpServers(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(servers))
	for name, raw := range servers {
		out = append(out, entryFromJSON(client, name, raw))
	}
	return out, nil
}

func httpEntry(withType bool, url, token string) map[string]any {
	e := map[string]any{"url": url, "headers": map[string]string{"Authorization": "Bearer " + token}}
	if withType {
		e["type"] = "http"
	}
	return e
}

// indentedValue renders v to replace the value starting at start, matching
// the indentation of the line it sits on.
func indentedValue(src []byte, start int, v any) string {
	lineStart := bytes.LastIndexByte(src[:start], '\n') + 1
	indent := ""
	for _, c := range src[lineStart:start] {
		if c != ' ' && c != '\t' {
			break
		}
		indent += string(c)
	}
	unit := "  "
	if strings.HasPrefix(indent, "\t") {
		unit = "\t"
	}
	b, _ := json.MarshalIndent(v, indent, unit)
	return strings.ReplaceAll(string(b), "\n", newline(src))
}

// Cursor edits ~/.cursor/mcp.json in place.
type Cursor struct{ path, backups string }

func NewCursor(home, backups string) *Cursor {
	return &Cursor{path: filepath.Join(home, ".cursor", "mcp.json"), backups: backups}
}

func (c *Cursor) ID() string             { return "cursor" }
func (c *Cursor) Name() string           { return "Cursor" }
func (c *Cursor) Path() string           { return c.path }
func (c *Cursor) List() ([]Entry, error) { return listJSON(c.ID(), c.path) }

func (c *Cursor) Attach(name, url, token string) (Record, error) {
	rec := Record{Client: c.ID(), Name: name}
	backup, err := rewrite(c.path, c.backups, c.ID(), func(old []byte) ([]byte, error) {
		start, end, err := valueSpan(old, "mcpServers", name)
		if err != nil {
			return nil, err
		}
		rec.Original = string(old[start:end])
		rec.Written = indentedValue(old, start, httpEntry(false, url, token))
		return splice(old, start, end, rec.Written), nil
	})
	rec.Backup, rec.At = backup, time.Now()
	return rec, err
}

func (c *Cursor) Restore(rec Record) error {
	_, err := rewrite(c.path, c.backups, c.ID(), func(old []byte) ([]byte, error) {
		start, end, err := valueSpan(old, "mcpServers", rec.Name)
		if err != nil {
			return nil, err
		}
		if !sameJSON(old[start:end], []byte(rec.Written)) {
			return nil, ErrConflict
		}
		return splice(old, start, end, rec.Original), nil
	})
	return err
}

func splice(src []byte, start, end int, with string) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(with))
	out = append(out, src[:start]...)
	out = append(out, with...)
	return append(out, src[end:]...)
}

package clients

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// jsonEntry is the shape most clients use for one server, with the members
// some of them name differently.
type jsonEntry struct {
	Type        string            `json:"type"`
	Command     any               `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Environment map[string]string `json:"environment"` // OpenCode
	Cwd         string            `json:"cwd"`
	URL         string            `json:"url"`
	ServerURL   string            `json:"serverUrl"` // Windsurf, Antigravity
	HTTPURL     string            `json:"httpUrl"`   // Gemini CLI
	Disabled    bool              `json:"disabled"`
	Enabled     *bool             `json:"enabled"` // OpenCode
}

func entryFromJSON(client, name string, raw []byte) Entry {
	var j jsonEntry
	_ = json.Unmarshal(stripJSONC(raw), &j)
	e := Entry{Client: client, Name: name, Args: j.Args, Cwd: j.Cwd, URL: cmp.Or(j.URL, j.ServerURL, j.HTTPURL),
		Enabled: !j.Disabled && (j.Enabled == nil || *j.Enabled)}
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
	env := j.Env
	if env == nil {
		env = j.Environment
	}
	e.setEnv(env)
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

// jsonServers returns the raw entries under key in a JSON config: nil when
// the file does not exist, empty when it has none.
func jsonServers(path, key string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(stripJSONC(b), &doc); err != nil {
		return nil, errSyntax
	}
	servers := map[string]json.RawMessage{}
	if raw := doc[key]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, errSyntax
		}
	}
	return servers, nil
}

// mcpServers returns the raw entries of a JSON config's mcpServers object.
func mcpServers(path string) (map[string]json.RawMessage, error) {
	return jsonServers(path, "mcpServers")
}

// heldJSON says why an entry cannot run as written outside its app: the app
// fills in ${…} variables (and VS Code's envFile) itself when it starts it.
func heldJSON(raw []byte) string {
	switch {
	case bytes.Contains(raw, []byte("${")):
		return "用了 ${…} 变量，由应用自己展开，MCPcell 拿不到这些值"
	case bytes.Contains(raw, []byte(`"envFile"`)):
		return "用 envFile 读取环境变量，MCPcell 拿不到这些值"
	}
	return ""
}

func listJSON(client, path, key string) ([]Entry, error) {
	servers, err := jsonServers(path, key)
	if err != nil || servers == nil {
		return nil, err
	}
	out := make([]Entry, 0, len(servers))
	for name, raw := range servers {
		e := entryFromJSON(client, name, raw)
		if e.Kind == "stdio" {
			e.Hold = heldJSON(raw)
		}
		out = append(out, e)
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

// jsonStyle is how an agent writes one server.
type jsonStyle int

const (
	styleCommand jsonStyle = iota // "command": "x", "args": [...]
	styleTyped                    // the same, and "type": "stdio" or "http" (VS Code)
	styleArray                    // "type": "local", "command": ["x", ...], "environment" (OpenCode)
)

// jsonAgent is one agent whose servers are an object in a JSON file.
type jsonAgent struct {
	id, name string
	path     func(home string) string // under the user's home
	key      string                   // the object holding the servers
	style    jsonStyle
}

// appDir is where desktop apps keep their settings, under home, so a test
// or a trial daemon with another home never reaches the real ones.
func appDir(home string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "Roaming")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	}
	return filepath.Join(home, ".config")
}

func inHome(parts ...string) func(string) string {
	return func(home string) string { return filepath.Join(append([]string{home}, parts...)...) }
}

func inAppDir(parts ...string) func(string) string {
	return func(home string) string { return filepath.Join(append([]string{appDir(home)}, parts...)...) }
}

// openCodeConfig is OpenCode's config file. OpenCode reads both opencode.json
// and opencode.jsonc; edit whichever exists, and prefer opencode.json when
// both do (the one MCPcell has always written). JSONClient.Path calls it on
// every use, so a file created while the daemon runs is found.
func openCodeConfig(home string) string {
	dir := filepath.Join(home, ".config", "opencode")
	if p := filepath.Join(dir, "opencode.json"); exists(p) {
		return p
	}
	if p := filepath.Join(dir, "opencode.jsonc"); exists(p) {
		return p
	}
	return filepath.Join(dir, "opencode.json")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// jsonAgents are the agents MCPcell edits as JSON, in display order. Paths
// follow each agent's documentation (docs/architecture.md §9.4); the VS Code
// extensions are found in VS Code's own storage.
var jsonAgents = []jsonAgent{
	{"cursor", "Cursor", inHome(".cursor", "mcp.json"), "mcpServers", styleCommand},
	{"claude-desktop", "Claude Desktop", inAppDir("Claude", "claude_desktop_config.json"), "mcpServers", styleCommand},
	{"vscode", "VS Code", inAppDir("Code", "User", "mcp.json"), "servers", styleTyped},
	{"copilot", "Copilot CLI", inHome(".copilot", "mcp-config.json"), "mcpServers", styleCommand},
	{"gemini", "Gemini CLI", inHome(".gemini", "settings.json"), "mcpServers", styleCommand},
	{"antigravity", "Antigravity", inHome(".gemini", "antigravity", "mcp_config.json"), "mcpServers", styleCommand},
	{"windsurf", "Windsurf", inHome(".codeium", "windsurf", "mcp_config.json"), "mcpServers", styleCommand},
	{"kiro", "Kiro", inHome(".kiro", "settings", "mcp.json"), "mcpServers", styleCommand},
	{"cline", "Cline", inAppDir("Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json"), "mcpServers", styleCommand},
	{"roo", "Roo Code", inAppDir("Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "mcp_settings.json"), "mcpServers", styleCommand},
	{"opencode", "OpenCode", openCodeConfig, "mcp", styleArray},
}

// JSONClient edits one JSON agent's servers in place: only the entry
// changes, the rest of the file (other settings, comments, key order) stays
// as it was.
type JSONClient struct {
	jsonAgent
	home, backups string
}

func newJSONClient(a jsonAgent, home, backups string) *JSONClient {
	return &JSONClient{jsonAgent: a, home: home, backups: backups}
}

// NewCursor is Cursor's ~/.cursor/mcp.json.
func NewCursor(home, backups string) *JSONClient { return newJSONClient(jsonAgents[0], home, backups) }

// jsonClients returns a client for every JSON agent.
func jsonClients(home, backups string) []Client {
	out := make([]Client, len(jsonAgents))
	for i, a := range jsonAgents {
		out[i] = newJSONClient(a, home, backups)
	}
	return out
}

func (c *JSONClient) ID() string             { return c.id }
func (c *JSONClient) Name() string           { return c.name }
func (c *JSONClient) Path() string           { return c.jsonAgent.path(c.home) }
func (c *JSONClient) List() ([]Entry, error) { return listJSON(c.id, c.Path(), c.key) }

func (c *JSONClient) httpValue(t Target) map[string]any {
	v := httpEntry(c.style == styleTyped, t.URL, t.Token)
	if c.style == styleArray {
		v["type"] = "remote"
	}
	return v
}

func (c *JSONClient) Attach(name string, t Target) (Record, error) {
	rec := Record{Client: c.id, Name: name}
	backup, err := rewrite(c.Path(), c.backups, c.id, func(old []byte) ([]byte, error) {
		start, end, err := valueSpan(old, c.key, name)
		if err != nil {
			return nil, err
		}
		rec.Original = string(old[start:end])
		var value any = c.httpValue(t)
		if t.wrapped() {
			if value, err = wrapJSON(old[start:end], t.Wrapper, entryFromJSON(c.id, name, old[start:end])); err != nil {
				return nil, err
			}
		}
		rec.Written = indentedValue(old, start, value)
		return splice(old, start, end, rec.Written), nil
	})
	rec.Backup, rec.At = backup, time.Now()
	return rec, err
}

func (c *JSONClient) Restore(rec Record) error {
	_, err := rewrite(c.Path(), c.backups, c.id, func(old []byte) ([]byte, error) {
		start, end, err := valueSpan(old, c.key, rec.Name)
		if err != nil {
			return nil, err
		}
		if !sameJSON(old[start:end], []byte(rec.Written)) {
			return nil, ErrConflict
		}
		if rec.Added {
			return removeMember(old, []string{c.key}, rec.Name)
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

func (c *JSONClient) Add(name string, t Target) (Record, error) {
	rec := Record{Client: c.id, Name: name, Added: true}
	if err := ensureFile(c.Path(), "{\n  \""+c.key+"\": {}\n}\n"); err != nil {
		return rec, err
	}
	backup, err := rewrite(c.Path(), c.backups, c.id, func(old []byte) ([]byte, error) {
		if _, _, err := valueSpan(old, c.key, name); err == nil {
			return nil, ErrExists
		}
		if _, _, err := valueSpan(old, c.key); errors.Is(err, ErrNotFound) {
			if old, _, err = insertMember(old, nil, c.key, map[string]any{}); err != nil {
				return nil, err
			}
		}
		var value any = c.httpValue(t)
		if t.wrapped() {
			value = newJSON(c.style, t.Wrapper, t.Server)
		}
		out, written, err := insertMember(old, []string{c.key}, name, value)
		rec.Written = written
		return out, err
	})
	rec.Backup, rec.At = backup, time.Now()
	return rec, err
}

// ensureFile creates path with initial content if it does not exist yet.
func ensureFile(path, initial string) error {
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(initial), 0o600)
}

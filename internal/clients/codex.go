package clients

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Codex edits ~/.codex/config.toml (shared by the CLI, IDE extension and
// desktop app). The entry's table and its sub-tables are replaced as one
// block; comments and everything else in the file stay as they were.
type Codex struct{ path, backups string }

func NewCodex(home, backups string) *Codex {
	dir := filepath.Join(home, ".codex")
	if d := os.Getenv("CODEX_HOME"); d != "" {
		dir = d
	}
	return &Codex{path: filepath.Join(dir, "config.toml"), backups: backups}
}

func (c *Codex) ID() string   { return "codex" }
func (c *Codex) Name() string { return "Codex" }
func (c *Codex) Path() string { return c.path }

func (c *Codex) List() ([]Entry, error) {
	b, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		MCPServers map[string]map[string]any `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(string(b), &doc); err != nil {
		return nil, fmt.Errorf("config.toml 解析失败：%w", err)
	}
	out := make([]Entry, 0, len(doc.MCPServers))
	for name, t := range doc.MCPServers {
		e := Entry{Client: c.ID(), Name: name, Enabled: true}
		e.Command, _ = t["command"].(string)
		e.Cwd, _ = t["cwd"].(string)
		e.URL, _ = t["url"].(string)
		if v, ok := t["enabled"].(bool); ok {
			e.Enabled = v
		}
		e.Args = strings_(t["args"])
		e.EnvVars = strings_(t["env_vars"])
		env := map[string]string{}
		if m, ok := t["env"].(map[string]any); ok {
			for k, v := range m {
				env[k] = fmt.Sprint(v)
			}
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
		out = append(out, e)
	}
	return out, nil
}

func strings_(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (c *Codex) Attach(name string, t Target) (Record, error) {
	rec := Record{Client: c.ID(), Name: name}
	var current Entry
	if t.wrapped() {
		entries, err := c.List()
		if err != nil {
			return rec, err
		}
		for _, e := range entries {
			if e.Name == name {
				current = e
			}
		}
		if current.Kind != "stdio" {
			return rec, errors.New("只有用命令启动的条目才能包装")
		}
	}
	backup, err := rewrite(c.path, c.backups, c.ID(), func(old []byte) ([]byte, error) {
		start, end, err := tomlBlock(old, name)
		if err != nil {
			return nil, err
		}
		rec.Original = string(old[start:end])
		if t.wrapped() {
			rec.Written = codexWrapBlock(rec.Original, t.Wrapper, current, newline(old))
		} else {
			rec.Written = codexHTTPBlock(rec.Original, t.URL, t.Token, newline(old))
		}
		out := splice(old, start, end, rec.Written)
		if _, err := toml.Decode(string(out), new(map[string]any)); err != nil {
			return nil, fmt.Errorf("改写后的 config.toml 无法解析，未写入：%w", err)
		}
		return out, nil
	})
	rec.Backup, rec.At = backup, time.Now()
	return rec, err
}

func (c *Codex) Add(name string, t Target) (Record, error) {
	rec := Record{Client: c.ID(), Name: name, Added: true}
	if err := ensureFile(c.path, ""); err != nil {
		return rec, err
	}
	backup, err := rewrite(c.path, c.backups, c.ID(), func(old []byte) ([]byte, error) {
		switch _, _, err := tomlBlock(old, name); {
		case err == nil:
			return nil, ErrExists
		case !errors.Is(err, ErrNotFound):
			return nil, err
		}
		nl := newline(old)
		header := fmt.Sprintf("[mcp_servers.%s]", tomlKeyText(name))
		if t.wrapped() {
			rec.Written = codexNewBlock(name, t.Wrapper, t.Server, nl)
		} else {
			rec.Written = codexHTTPBlock(header+nl, t.URL, t.Token, nl)
		}
		out := append([]byte(nil), old...)
		if len(out) > 0 && !strings.HasSuffix(string(out), nl) {
			out = append(out, nl...)
		}
		if len(out) > 0 {
			out = append(out, nl...) // blank line before the new table
		}
		out = append(out, rec.Written...)
		if _, err := toml.Decode(string(out), new(map[string]any)); err != nil {
			return nil, fmt.Errorf("改写后的 config.toml 无法解析，未写入：%w", err)
		}
		return out, nil
	})
	rec.Backup, rec.At = backup, time.Now()
	return rec, err
}

func tomlKeyText(name string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(name) {
		return name
	}
	return fmt.Sprintf("%q", name)
}

func (c *Codex) Restore(rec Record) error {
	_, err := rewrite(c.path, c.backups, c.ID(), func(old []byte) ([]byte, error) {
		start, end, err := tomlBlock(old, rec.Name)
		if err != nil {
			return nil, err
		}
		if normalizeBlock(string(old[start:end])) != normalizeBlock(rec.Written) {
			return nil, ErrConflict
		}
		if rec.Added {
			// Also take the blank line Add put before the table.
			nl := newline(old)
			if start >= 2*len(nl) && string(old[start-2*len(nl):start]) == nl+nl {
				start -= len(nl)
			}
			return splice(old, start, end, ""), nil
		}
		return splice(old, start, end, rec.Original), nil
	})
	return err
}

func normalizeBlock(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

var tomlHeader = regexp.MustCompile(`^\s*\[\s*([^\[\]#]+?)\s*\]\s*(#.*)?$`)

// tomlPath splits a dotted TOML key, honoring quotes.
func tomlPath(key string) []string {
	var parts []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(key); i++ {
		ch := key[i]
		switch {
		case quote != 0 && ch == quote:
			quote = 0
		case quote != 0:
			cur.WriteByte(ch)
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return append(parts, strings.TrimSpace(cur.String()))
}

type line struct {
	start, end int // end includes the newline
	text       string
}

func splitLines(src []byte) []line {
	var out []line
	for start := 0; start < len(src); {
		end := start
		for end < len(src) && src[end] != '\n' {
			end++
		}
		text := strings.TrimRight(string(src[start:end]), "\r")
		if end < len(src) {
			end++
		}
		out = append(out, line{start, end, text})
		start = end
	}
	return out
}

func headerPath(text string) ([]string, bool) {
	if strings.HasPrefix(strings.TrimSpace(text), "[[") {
		return nil, true // array of tables: a header, but never ours
	}
	m := tomlHeader.FindStringSubmatch(text)
	if m == nil {
		return nil, false
	}
	return tomlPath(m[1]), true
}

func isOurs(path []string, name string) bool {
	return len(path) >= 2 && path[0] == "mcp_servers" && path[1] == name
}

// tomlBlock finds [mcp_servers.<name>] and its sub-tables. The block ends
// after its last key line, so blank lines and comments that introduce the
// next table stay outside it.
func tomlBlock(src []byte, name string) (int, int, error) {
	lines := splitLines(src)
	first, last := -1, -1
	for i, l := range lines {
		path, header := headerPath(l.text)
		if first < 0 {
			if header && len(path) == 2 && isOurs(path, name) {
				first, last = i, i
			}
			continue
		}
		if header && !isOurs(path, name) {
			break
		}
		if t := strings.TrimSpace(l.text); t != "" && !strings.HasPrefix(t, "#") {
			last = i
		}
	}
	if first < 0 {
		if strings.Contains(string(src), "[mcp_servers]") {
			return 0, 0, errors.New("这个条目写在 [mcp_servers] 的内联表里，暂不支持自动接管，请手动修改")
		}
		return 0, 0, ErrNotFound
	}
	return lines[first].start, lines[last].end, nil
}

// stdio-only keys dropped when an entry becomes an HTTP endpoint. Everything
// else (enabled, timeouts, tool filters) is client policy and is kept.
var codexDrop = map[string]bool{"command": true, "args": true, "env": true, "cwd": true, "env_vars": true,
	"url": true, "http_headers": true, "env_http_headers": true, "bearer_token_env_var": true}

var tomlKey = regexp.MustCompile(`^\s*("[^"]*"|'[^']*'|[A-Za-z0-9_-]+)\s*=`)

// codexHTTPBlock rewrites one entry's block to point at url.
func codexHTTPBlock(block, url, token, nl string) string {
	lines := splitLines([]byte(block))
	out := []string{lines[0].text, fmt.Sprintf("url = %q", url), fmt.Sprintf("http_headers = { Authorization = %q }", "Bearer "+token)}
	dropping, depth, inSub, dropSub := false, 0, false, false
	for _, l := range lines[1:] {
		if path, header := headerPath(l.text); header && depth == 0 {
			inSub, dropSub = true, len(path) >= 3 && path[2] == "env"
			if !dropSub {
				out = append(out, l.text)
			}
			continue
		}
		if inSub {
			if !dropSub {
				out = append(out, l.text)
			}
			continue
		}
		if depth == 0 {
			if m := tomlKey.FindStringSubmatch(l.text); m != nil {
				dropping = codexDrop[strings.Trim(m[1], `"'`)]
			} else {
				dropping = false // blank line or comment
			}
		}
		depth += bracketDelta(l.text)
		if depth < 0 {
			depth = 0
		}
		if !dropping {
			out = append(out, l.text)
		}
	}
	// End at the last key line, as tomlBlock does, so finding this block again
	// at restore time covers exactly what was written.
	for len(out) > 1 {
		if t := strings.TrimSpace(out[len(out)-1]); t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		out = out[:len(out)-1]
	}
	return strings.Join(out, nl) + nl
}

// bracketDelta counts [ and { minus ] and } outside strings and comments, to
// follow multi-line arrays and inline tables.
func bracketDelta(s string) int {
	d := 0
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			if ch == '\\' && quote == '"' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '#':
			return d
		case ch == '[' || ch == '{':
			d++
		case ch == ']' || ch == '}':
			d--
		}
	}
	return d
}

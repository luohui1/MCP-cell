package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Target is what a taken-over entry becomes.
type Target struct {
	// Wrapper keeps the entry's own command and runs it through mcpcell-run
	// at this path: the entry still says which server runs, and removing the
	// prefix is the native setup again.
	Wrapper string
	// URL and Token point the entry at the daemon's HTTP endpoint instead,
	// when no wrapper is installed.
	URL, Token string
	// Server is what a new entry (Add) runs; Attach wraps the entry's own.
	Server Entry
}

func (t Target) wrapped() bool { return t.Wrapper != "" }

// applied reports whether e now is what t writes.
func (t Target) applied(e Entry) bool {
	if t.wrapped() {
		return e.Command == t.Wrapper
	}
	return e.URL == t.URL
}

// WrapperName is the wrapper's executable name without extension.
const WrapperName = "mcpcell-run"

// Unwrap returns the entry a wrapped entry runs, and whether e is wrapped:
// `mcpcell-run [flags] -- command args...`.
func Unwrap(e Entry) (Entry, bool) {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(e.Command), filepath.Ext(e.Command)))
	if e.Kind != "stdio" || base != WrapperName {
		return e, false
	}
	for i, a := range e.Args {
		if a == "--" || !strings.HasPrefix(a, "--") {
			rest := e.Args[i:]
			if a == "--" {
				rest = rest[1:]
			}
			if len(rest) == 0 {
				return e, false
			}
			inner := e
			inner.Command, inner.Args = rest[0], append([]string(nil), rest[1:]...)
			return inner, true
		}
	}
	return e, false
}

// wrapArgs is the wrapper's argument list for running command args.
func wrapArgs(command string, args []string) []string {
	return append([]string{"--", command}, args...)
}

// member and object keep a JSON object's key order, so a rewritten entry
// still reads like the one the user wrote.
type member struct {
	key   string
	value json.RawMessage
}

type object []member

func parseObject(raw []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(stripJSONC(raw)))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("条目不是 JSON 对象")
	}
	var o object
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o = append(o, member{t.(string), v})
	}
	return o, nil
}

// set replaces key's value, or inserts it after the member named after (at
// the end when there is none).
func (o object) set(key string, v any, after string) object {
	b, _ := marshalPlain(v)
	for i := range o {
		if o[i].key == key {
			o[i].value = b
			return o
		}
	}
	at := len(o)
	for i := range o {
		if o[i].key == after {
			at = i + 1
		}
	}
	return append(o[:at], append([]member{{key, b}}, o[at:]...)...)
}

func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := marshalPlain(m.key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalPlain is json.Marshal without HTML escaping, so paths and flags read
// as written.
func marshalPlain(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// wrapJSON rewrites a JSON entry to run its command through the wrapper,
// keeping every other member as it was.
func wrapJSON(raw []byte, wrapper string, e Entry) (object, error) {
	o, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	for _, m := range o {
		if m.key == "command" && !bytes.HasPrefix(bytes.TrimSpace(m.value), []byte(`"`)) {
			return nil, errors.New("command 不是字符串，暂不支持包装")
		}
	}
	o = o.set("command", wrapper, "type")
	return o.set("args", wrapArgs(e.Command, e.Args), "command"), nil
}

// newJSON is a new wrapped entry for server.
func newJSON(withType bool, wrapper string, server Entry) object {
	var o object
	if withType {
		o = o.set("type", "stdio", "")
	}
	o = o.set("command", wrapper, "type")
	o = o.set("args", wrapArgs(server.Command, server.Args), "command")
	if len(server.Env) > 0 {
		o = o.set("env", server.Env, "args")
	}
	if server.Cwd != "" {
		o = o.set("cwd", server.Cwd, "env")
	}
	return o
}

// tomlString is a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlArray(list []string) string {
	parts := make([]string, len(list))
	for i, s := range list {
		parts[i] = tomlString(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// codexWrapBlock rewrites one entry's table so its command runs through the
// wrapper. Only the command and args lines change; env, enabled, timeouts,
// comments and layout stay as they were.
func codexWrapBlock(block, wrapper string, e Entry, nl string) string {
	lines := splitLines([]byte(block))
	out := []string{lines[0].text}
	cmdAt, hasArgs := -1, false
	skipping, depth, inSub := false, 0, false
	for _, l := range lines[1:] {
		if _, header := headerPath(l.text); header && depth == 0 {
			inSub = true // a sub-table such as [mcp_servers.x.env]
		}
		if inSub {
			out = append(out, l.text)
			continue
		}
		if depth == 0 {
			skipping = false
			if m := tomlKey.FindStringSubmatch(l.text); m != nil {
				switch strings.Trim(m[1], `"'`) {
				case "command":
					out = append(out, "command = "+tomlString(wrapper))
					cmdAt, skipping = len(out)-1, true
				case "args":
					out = append(out, "args = "+tomlArray(wrapArgs(e.Command, e.Args)))
					hasArgs, skipping = true, true
				}
			}
		}
		depth += bracketDelta(l.text)
		if depth < 0 {
			depth = 0
		}
		if !skipping {
			out = append(out, l.text)
		}
	}
	if !hasArgs && cmdAt >= 0 {
		out = append(out[:cmdAt+1], append([]string{"args = " + tomlArray(wrapArgs(e.Command, e.Args))}, out[cmdAt+1:]...)...)
	}
	return strings.Join(out, nl) + nl
}

// codexNewBlock is a new wrapped entry's table for server.
func codexNewBlock(name, wrapper string, server Entry, nl string) string {
	out := []string{
		fmt.Sprintf("[mcp_servers.%s]", tomlKeyText(name)),
		"command = " + tomlString(wrapper),
		"args = " + tomlArray(wrapArgs(server.Command, server.Args)),
	}
	if server.Cwd != "" {
		out = append(out, "cwd = "+tomlString(server.Cwd))
	}
	if len(server.Env) > 0 {
		out = append(out, "", fmt.Sprintf("[mcp_servers.%s.env]", tomlKeyText(name)))
		for _, k := range sortedKeys(server.Env) {
			out = append(out, fmt.Sprintf("%s = %s", tomlKeyText(k), tomlString(server.Env[k])))
		}
	}
	return strings.Join(out, nl) + nl
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

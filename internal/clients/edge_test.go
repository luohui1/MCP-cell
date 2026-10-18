package clients

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// Config files as people write them: every case is wrapped, checked to mean
// the same thing apart from command and args, and restored byte for byte.

var codexEdges = []struct{ name, entry, src string }{
	{"no newline at end", "ctx", "[mcp_servers.ctx]\ncommand = \"npx\"\nargs = [\"-y\", \"x\"]"},
	{"header comment", "ctx", "[mcp_servers.ctx] # docs: https://x.dev/#mcp\ncommand = \"npx\"\nargs = [\"-y\", \"x\"]\n"},
	{"quoted name with dot", "a.b", "[mcp_servers.\"a.b\"]\ncommand = \"npx\"\nargs = [\"-y\", \"x\"]\n"},
	{"spacing and tabs", "ctx", "[mcp_servers.ctx]\n\tcommand=\"npx\"\n  args   =   [ \"-y\" , \"x\" ]\n"},
	{"brackets and hashes in strings", "ctx",
		"[mcp_servers.ctx]\ncommand = \"npx\"\nargs = [\n  \"-y\", # comment with ] and [\n  \"pkg[extra]\",\n  \"--x=#1\", \"a]b\",\n]\nstartup_timeout_sec = 20\n"},
	{"literal strings", "ctx", "[mcp_servers.ctx]\ncommand = 'C:\\tools\\npx.cmd'\nargs = ['-y', 'C:\\a\\b']\n"},
	{"unicode and escapes", "ctx", "[mcp_servers.ctx]\ncommand = \"D:\\\\工具\\\\run.exe\"\nargs = [\"\\u00e9\", \"tab\\there\", \"q\\\"uote\"]\n"},
	{"inline env with brackets", "ctx", "[mcp_servers.ctx]\ncommand = \"npx\"\nargs = [\"x\"]\nenv = { A = \"1\", B = \"x]y\" }\n"},
	{"dotted env keys", "ctx", "[mcp_servers.ctx]\ncommand = \"npx\"\nenv.A = \"1\"\nargs = [\"x\"]\n"},
	{"args before command", "ctx", "[mcp_servers.ctx]\nargs = [\n  \"-y\",\n  \"x\",\n]\ncommand = \"npx\"\nenabled = false\n"},
	{"no args", "ctx", "[mcp_servers.ctx]\ncommand = \"server.exe\"\ncwd = \"D:\\\\w\"\n"},
	{"quoted keys", "ctx", "[mcp_servers.ctx]\n\"command\" = \"npx\"\n'args' = [\"x\"]\n"},
	{"deeper sub-tables", "ctx",
		"[mcp_servers.ctx]\ncommand = \"npx\"\nargs = [\"x\"]\n\n[mcp_servers.ctx.env]\nK = \"v\"\n\n[mcp_servers.ctx.tools.search]\napproval_mode = \"approve\"\n\n[mcp_servers.other]\ncommand = \"y\"\n"},
	{"tool lists after args", "ctx",
		"[mcp_servers.ctx]\ncommand = \"npx\"\nargs = [\"x\"]\nenabled_tools = [\n  \"a\",\n  \"b\",\n]\ntool_timeout_sec = 60\n"},
	{"BOM and CRLF", "ctx", "\ufeffmodel = \"m\"\r\n\r\n[mcp_servers.ctx]\r\ncommand = \"npx\"\r\nargs = [\r\n  \"-y\",\r\n  \"x\",\r\n]\r\n"},
	{"multi-line string elsewhere", "ctx",
		"[mcp_servers.ctx]\ncommand = \"npx\"\nargs = [\"x\"]\n\n[notes]\ntext = \"\"\"\n[mcp_servers.ctx]\ncommand = \"evil\"\n\"\"\"\n"},
}

func TestCodexWrapEdgeCases(t *testing.T) {
	for _, c := range codexEdges {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".codex", "config.toml")
			write(t, path, c.src)
			cx := NewCodex(home, t.TempDir())
			inTemp(t, cx.Path())
			before := decodeTOML(t, strings.TrimPrefix(c.src, "\ufeff"))
			rec, err := cx.Attach(c.entry, Target{Wrapper: wrapper})
			if err != nil {
				t.Fatalf("attach: %v", err)
			}
			after := decodeTOML(t, strings.TrimPrefix(read(t, path), "\ufeff"))
			checkWrapped(t, entryOf(before, c.entry), entryOf(after, c.entry))
			for name := range before["mcp_servers"].(map[string]any) {
				if name != c.entry && !reflect.DeepEqual(entryOf(before, name), entryOf(after, name)) {
					t.Fatalf("entry %s changed", name)
				}
			}
			delete(before, "mcp_servers")
			delete(after, "mcp_servers")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rest of the file changed:\n%s", read(t, path))
			}
			if err := cx.Restore(rec); err != nil {
				t.Fatalf("restore: %v", err)
			}
			if got := read(t, path); got != c.src {
				t.Fatalf("restore not byte-exact:\n%q\nwant\n%q", got, c.src)
			}
		})
	}
}

var cursorEdges = []struct{ name, src string }{
	{"tabs", "{\n\t\"mcpServers\": {\n\t\t\"ctx\": {\n\t\t\t\"command\": \"npx\",\n\t\t\t\"args\": [\"-y\", \"x\"]\n\t\t}\n\t}\n}\n"},
	{"one line", `{"mcpServers":{"ctx":{"command":"npx","args":["-y","x"],"env":{"K":"v"}}}}`},
	{"four spaces CRLF", "{\r\n    \"mcpServers\": {\r\n        \"ctx\": {\r\n            \"command\": \"npx\",\r\n            \"args\": [\r\n                \"x\"\r\n            ]\r\n        }\r\n    }\r\n}\r\n"},
	{"escapes and unicode", `{"mcpServers": {"ctx": {"command": "D:\\\u5de5\u5177\\run.exe", "args": ["\/x", "<&>"], "cwd": "D:\\w"}}}`},
	{"comments and trailing commas", "{\n  // servers\n  \"mcpServers\": {\n    \"ctx\": {\n      \"command\": \"npx\", // runner\n      /* the package */\n      \"args\": [\"x\",],\n    },\n  },\n}\n"},
	{"no args", `{"mcpServers": {"ctx": {"type": "stdio", "command": "server.exe", "env": {}}}}`},
	{"BOM", "\ufeff{\n  \"mcpServers\": {\n    \"ctx\": { \"command\": \"npx\", \"args\": [\"x\"] }\n  }\n}\n"},
	{"other keys around", `{"a": [1, {"b": "}"}], "mcpServers": {"before": {"url": "http://x/"}, "ctx": {"disabled": true, "command": "npx", "args": ["x"], "autoApprove": ["t"]}, "after": {"command": "y"}}, "z": "{\"mcpServers\": 1}"}`},
}

func TestCursorWrapEdgeCases(t *testing.T) {
	for _, c := range cursorEdges {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".cursor", "mcp.json")
			write(t, path, c.src)
			cu := NewCursor(home, t.TempDir())
			inTemp(t, cu.Path())
			before := decodeJSONC(t, c.src)
			rec, err := cu.Attach("ctx", Target{Wrapper: wrapper})
			if err != nil {
				t.Fatalf("attach: %v", err)
			}
			after := decodeJSONC(t, read(t, path))
			checkWrapped(t, entryOf(before, "ctx"), entryOf(after, "ctx"))
			for name := range before["mcpServers"].(map[string]any) {
				if name != "ctx" && !reflect.DeepEqual(entryOf(before, name), entryOf(after, name)) {
					t.Fatalf("entry %s changed", name)
				}
			}
			delete(before, "mcpServers")
			delete(after, "mcpServers")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rest of the file changed:\n%s", read(t, path))
			}
			if err := cu.Restore(rec); err != nil {
				t.Fatalf("restore: %v", err)
			}
			if got := read(t, path); got != c.src {
				t.Fatalf("restore not byte-exact:\n%q\nwant\n%q", got, c.src)
			}
		})
	}
}

func decodeTOML(t *testing.T, src string) map[string]any {
	t.Helper()
	var doc map[string]any
	if _, err := toml.Decode(src, &doc); err != nil {
		t.Fatalf("does not parse: %v\n%s", err, src)
	}
	return doc
}

func decodeJSONC(t *testing.T, src string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(stripJSONC([]byte(strings.TrimPrefix(src, "\ufeff"))), &doc); err != nil {
		t.Fatalf("does not parse: %v\n%s", err, src)
	}
	return doc
}

func entryOf(doc map[string]any, name string) map[string]any {
	for _, key := range []string{"mcp_servers", "mcpServers"} {
		if servers, ok := doc[key].(map[string]any); ok {
			e, _ := servers[name].(map[string]any)
			return e
		}
	}
	return nil
}

// checkWrapped: command is the wrapper, args are "--", the old command and
// the old args, and nothing else in the entry changed.
func checkWrapped(t *testing.T, before, after map[string]any) {
	t.Helper()
	if before == nil || after == nil {
		t.Fatalf("entry missing: before %v after %v", before != nil, after != nil)
	}
	want := []any{"--", before["command"]}
	if args, ok := before["args"].([]any); ok {
		want = append(want, args...)
	}
	if after["command"] != wrapper || !reflect.DeepEqual(after["args"], want) {
		t.Fatalf("wrapped to %v %v, want %v", after["command"], after["args"], want)
	}
	b, a := map[string]any{}, map[string]any{}
	for k, v := range before {
		b[k] = v
	}
	for k, v := range after {
		a[k] = v
	}
	for _, k := range []string{"command", "args"} {
		delete(b, k)
		delete(a, k)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("other members changed:\n%v\n%v", b, a)
	}
}

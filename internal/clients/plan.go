package clients

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// ServiceKey identifies "the same server" across clients: same package or
// command line, same arguments, same environment values and directory.
// Differing credentials therefore never share an instance.
func ServiceKey(command string, args []string, env map[string]string, cwd string) string {
	if len(env) == 0 {
		env = nil // an empty env table and no env table mean the same
	}
	argv := proc.UnwrapCmd(append([]string{command}, args...))
	var k any
	if spec, rest, ok := npmcache.Detect(argv); ok {
		name, ver := npmcache.SplitSpec(spec)
		if ver == "latest" {
			ver = ""
		}
		if len(rest) == 0 {
			rest = nil
		}
		k = []any{"npm", name, ver, rest, env, cwd}
	} else {
		k = []any{"argv", argv, env, cwd}
	}
	b, _ := json.Marshal(k)
	return string(b)
}

// EntryKey is the entry's ServiceKey; a wrapped entry has its server's key,
// so it matches the same server configured natively elsewhere.
func EntryKey(e Entry) string {
	e, _ = Unwrap(e)
	return ServiceKey(e.Command, e.Args, e.Env, e.Cwd)
}

func DefinitionKey(s *registry.Service) string { return ServiceKey(s.Command, s.Args, s.Env, s.Cwd) }

// Definition is the MCPcell service that runs what the entry ran.
func Definition(e Entry, mode string) *registry.Service {
	return &registry.Service{Command: e.Command, Args: e.Args, Env: e.Env, Cwd: e.Cwd, Mode: mode}
}

// ModeFor is how a taken-over entry runs: one shared instance when the server
// is known to keep no per-client state, otherwise one instance per session,
// like the client ran it, unless the user says an unknown one can be shared.
func ModeFor(class Class, share bool) string {
	if class == ClassShared || class == ClassUnknown && share {
		return registry.ModeShared
	}
	return registry.ModePerSession
}

// Takeable reports whether MCPcell can run the entry: a local stdio server
// whose environment MCPcell can reproduce.
func Takeable(v EntryView) bool {
	switch v.Class {
	case ClassShared, ClassStateful, ClassUnknown:
		return v.Kind == "stdio" && len(v.EnvVars) == 0
	}
	return false
}

type EntryView struct {
	Entry
	Class         Class      `json:"class"`
	Reason        string     `json:"reason"`
	Package       string     `json:"package,omitempty"`
	Service       string     `json:"service,omitempty"` // MCPcell service it maps (or would map) to
	ServiceExists bool       `json:"serviceExists"`
	TakenOver     bool       `json:"takenOver"`
	TakenAt       *time.Time `json:"takenAt,omitempty"`
	Changed       bool       `json:"changed"` // taken over, but edited since
}

type ClientView struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	Found   bool        `json:"found"`
	Error   string      `json:"error,omitempty"`
	Entries []EntryView `json:"entries"`
}

// Overview lists every client's entries with what MCPcell would do with
// them. services is config.json's current definitions.
func Overview(cs []Client, store *Store, services map[string]*registry.Service, baseURL string) []ClientView {
	byKey := map[string]string{}
	for name, s := range services {
		byKey[DefinitionKey(s)] = name
	}
	taken := map[string]string{} // name -> key, for names proposed in this overview
	var out []ClientView
	for _, c := range cs {
		v := ClientView{ID: c.ID(), Name: c.Name(), Path: c.Path(), Entries: []EntryView{}}
		entries, err := c.List()
		switch {
		case err != nil:
			v.Error = err.Error()
		case entries != nil:
			v.Found = true
		}
		sort.Slice(entries, func(a, b int) bool { return entries[a].Name < entries[b].Name })
		for _, e := range entries {
			ev := EntryView{Entry: e}
			ev.Class, ev.Reason = Classify(e, baseURL)
			if e.Kind == "stdio" {
				ev.Package = LaunchPackage(e)
			}
			if rec, ok := store.Get(c.ID(), e.Name); ok {
				at := rec.At
				ev.TakenAt, ev.Service = &at, rec.Service
				_, wrapped := Unwrap(e)
				ev.TakenOver = wrapped || e.URL == baseURL+"/s/"+rec.Service
				ev.Changed = !ev.TakenOver
				if ev.TakenOver {
					ev.Class, ev.Reason = ClassManaged, "已由 MCPcell 接管"
				}
			} else if _, wrapped := Unwrap(e); wrapped {
				ev.Service = byKey[EntryKey(e)] // wrapped by hand: the service it maps to, if configured
			} else if ev.Class == ClassManaged {
				ev.Service = strings.TrimPrefix(e.URL, baseURL+"/s/")
			} else if e.Kind == "stdio" && ev.Class != ClassInternal {
				ev.Service = proposeName(e, byKey, services, taken)
			}
			_, ev.ServiceExists = services[ev.Service]
			v.Entries = append(v.Entries, ev)
		}
		out = append(out, v)
	}
	return out
}

// proposeName reuses an existing service with the same key, otherwise the
// entry's own name, suffixed with the client when that name means something
// else.
func proposeName(e Entry, byKey map[string]string, services map[string]*registry.Service, taken map[string]string) string {
	key := EntryKey(e)
	if name, ok := byKey[key]; ok {
		return name
	}
	for _, candidate := range []string{e.Name, e.Name + "-" + e.Client} {
		if !validName(candidate) {
			continue
		}
		if prev, ok := taken[candidate]; ok && prev != key {
			continue
		}
		if _, used := services[candidate]; used {
			continue
		}
		taken[candidate] = key
		return candidate
	}
	return ""
}

func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// FindClient returns the client with id.
func FindClient(cs []Client, id string) Client {
	for _, c := range cs {
		if c.ID() == id {
			return c
		}
	}
	return nil
}

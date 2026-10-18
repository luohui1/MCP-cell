package httpd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// One row per MCP server, merged across MCPcell's services and every agent's
// configuration: the same server configured in three agents is one row with
// three agent cells.

type agentCell struct {
	State   string        `json:"state"` // managed | native | changed | remote | internal
	Entry   string        `json:"entry"`
	Enabled bool          `json:"enabled"`
	Added   bool          `json:"added,omitempty"` // created by MCPcell; undo removes it
	Class   clients.Class `json:"class"`
	Reason  string        `json:"reason,omitempty"`
	// Takeable: native, and MCPcell can run it (local stdio, reproducible env).
	Takeable bool `json:"takeable,omitempty"`
}

type mcpRow struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Kind   string `json:"kind"` // stdio | http
	Launch string `json:"launch"`
	// Mode is the service's mode, or the one a takeover would choose.
	Mode     string               `json:"mode"`
	Detail   string               `json:"detail,omitempty"` // args, set when several rows share a name
	Class    clients.Class        `json:"class"`
	Reason   string               `json:"reason"`
	Service  string               `json:"service,omitempty"`  // existing MCPcell service
	Proposed string               `json:"proposed,omitempty"` // service a takeover or add would create
	Agents   map[string]agentCell `json:"agents"`
	CanAdd   bool                 `json:"canAdd"` // can be added to agents that lack it

	source *clients.Entry // a native entry to create the service from
	args   []string
}

type agentSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Found     bool   `json:"found"`
	Error     string `json:"error,omitempty"`
	Total     int    `json:"total"`
	Managed   int    `json:"managed"`
	Shareable int    `json:"shareable"` // native entries that can be taken over now
}

type unifiedResponse struct {
	Agents    []agentSummary  `json:"agents"`
	Rows      []*mcpRow       `json:"rows"`
	Autostart autostartStatus `json:"autostart"`
}

var rowOrder = map[string]int{"svc": 0, "key": 1, "url": 2, "internal": 3}

func (s *Server) unified() (unifiedResponse, error) {
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return unifiedResponse{}, err
	}
	base := s.baseURL()
	rows := map[string]*mcpRow{}
	row := func(key string, init func() *mcpRow) *mcpRow {
		if r := rows[key]; r != nil {
			return r
		}
		r := init()
		r.Key, r.Agents = key, map[string]agentCell{}
		rows[key] = r
		return r
	}
	for name, def := range cfg.Services {
		row("svc:"+name, func() *mcpRow { return serviceRow(name, def, base) })
	}
	var agents []agentSummary
	for _, v := range clients.Overview(s.opts.Clients, s.opts.Takeovers, cfg.Services, base) {
		sum := agentSummary{ID: v.ID, Name: v.Name, Path: v.Path, Found: v.Found, Error: v.Error, Total: len(v.Entries)}
		for _, ev := range v.Entries {
			cell := agentCell{Entry: ev.Name, Enabled: ev.Enabled, Class: ev.Class, Reason: ev.Reason}
			var key string
			switch {
			case ev.TakenOver || ev.Class == clients.ClassManaged:
				cell.State, key = "managed", "svc:"+ev.Service
				if ev.Service == "" { // wrapped by hand, with no configured service
					key = "key:" + shortHash(clients.EntryKey(ev.Entry))
				}
				if rec, ok := s.opts.Takeovers.Get(v.ID, ev.Name); ok {
					cell.Added = rec.Added
				}
				sum.Managed++
			case ev.Changed:
				cell.State, key = "changed", "svc:"+ev.Service
			case ev.Kind == "http":
				cell.State, key = "remote", "url:"+ev.URL
			case ev.Class == clients.ClassInternal:
				cell.State, key = "internal", "internal:"+v.ID+"/"+ev.Name
			case ev.ServiceExists:
				cell.State, key = "native", "svc:"+ev.Service
			default:
				cell.State, key = "native", "key:"+shortHash(clients.EntryKey(ev.Entry))
			}
			if cell.State == "native" && clients.Takeable(ev) {
				cell.Takeable = true
				sum.Shareable++
			}
			e := ev
			r := row(key, func() *mcpRow {
				launch := e.Package
				if e.Kind == "http" {
					launch = e.URL
				}
				return &mcpRow{Name: e.Name, Kind: e.Kind, Launch: launch, Mode: clients.ModeFor(e.Class, false), Class: e.Class,
					Reason: e.Reason, Proposed: e.Service, args: e.Args}
			})
			if r.Service == "" && key[:4] == "svc:" {
				r.Name, r.Reason = key[4:], "MCPcell 中已经没有这个服务"
			}
			if cell.State == "native" && r.source == nil {
				r.source = &e.Entry
			}
			if _, dup := r.Agents[v.ID]; !dup {
				r.Agents[v.ID] = cell
			}
		}
		agents = append(agents, sum)
	}
	named := map[string]int{}
	for _, r := range rows {
		named[r.Name]++
	}
	out := make([]*mcpRow, 0, len(rows))
	for _, r := range rows {
		if named[r.Name] > 1 && r.Kind == "stdio" {
			r.Detail = argsText(r.args)
		}
		switch {
		case r.Service != "":
			r.CanAdd = true
		case r.source != nil:
			r.CanAdd = r.Proposed != "" && clients.Takeable(clients.EntryView{Entry: *r.source, Class: r.Class})
		}
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool {
		ka, kb := rowOrder[prefix(out[a].Key)], rowOrder[prefix(out[b].Key)]
		if ka != kb {
			return ka < kb
		}
		return out[a].Name < out[b].Name
	})
	return unifiedResponse{Agents: agents, Rows: out, Autostart: s.autostart()}, nil
}

func serviceRow(name string, def *registry.Service, base string) *mcpRow {
	e := clients.Entry{Kind: "stdio", Command: def.Command, Args: def.Args, Env: def.Env, Cwd: def.Cwd}
	class, reason := clients.Classify(e, base)
	return &mcpRow{Name: name, Kind: "stdio", Launch: clients.LaunchPackage(e), Mode: def.Mode, Class: class, Reason: reason, Service: name, args: def.Args}
}

var secretFlag = regexp.MustCompile(`(?i)key|token|secret|passw|auth|cookie`)

// argsText shows how same-named entries differ, with values of
// secret-looking flags hidden.
func argsText(args []string) string {
	out := make([]string, len(args))
	hide := false
	for i, a := range args {
		switch {
		case hide:
			out[i], hide = "***", false
		case strings.HasPrefix(a, "-") && secretFlag.MatchString(a):
			if k, _, ok := strings.Cut(a, "="); ok {
				out[i] = k + "=***"
			} else {
				out[i], hide = a, true
			}
		default:
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

func prefix(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i]
		}
	}
	return key
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

func (s *Server) handleUnified(w http.ResponseWriter, r *http.Request) {
	u, err := s.unified()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// handleAddToAgent puts an MCP row into an agent that does not have it,
// creating the MCPcell service from another agent's entry first if needed.
func (s *Server) handleAddToAgent(w http.ResponseWriter, r *http.Request) {
	var req struct{ Row string }
	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<12))
	if json.Unmarshal(body, &req) != nil || req.Row == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	c := clients.FindClient(s.opts.Clients, r.PathValue("client"))
	if c == nil || s.opts.Takeovers == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "不支持这个 Agent"})
		return
	}
	s.takeMu.Lock()
	defer s.takeMu.Unlock()
	service, err := s.addToAgent(c, req.Row)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, clients.ErrExists) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"service": service})
}

func (s *Server) addToAgent(c clients.Client, key string) (string, error) {
	u, err := s.unified()
	if err != nil {
		return "", err
	}
	var row *mcpRow
	for _, r := range u.Rows {
		if r.Key == key {
			row = r
		}
	}
	switch {
	case row == nil:
		return "", errors.New("这个 MCP 已不存在，请刷新")
	case !row.CanAdd:
		return "", errors.New(row.Reason)
	}
	if _, has := row.Agents[c.ID()]; has {
		return "", errors.New("这个 Agent 已经配置了它")
	}
	service := row.Service
	if service == "" {
		service = row.Proposed
		def := clients.Definition(*row.source, clients.ModeFor(row.Class, false))
		err := s.editConfig(func(cfg *registry.Config) error {
			if _, exists := cfg.Services[service]; exists {
				return errors.New("同名服务刚被添加，请刷新后重试")
			}
			cfg.Services[service] = def
			return nil
		})
		if err != nil {
			return "", err
		}
		s.pool.Put(service, def)
		go s.warm(service)
	}
	rec, err := c.Add(service, s.target(service))
	if err != nil {
		return "", err
	}
	rec.Service = service
	if err := s.opts.Takeovers.Put(rec); err != nil {
		return "", errors.New("已写入 Agent 配置，但保存记录失败：" + err.Error())
	}
	s.opts.Log.Info("added to agent", "client", c.ID(), "service", service)
	return service, nil
}

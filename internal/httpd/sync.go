package httpd

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"

	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// Keeping every app on the same set of MCPs (config.json syncApps, default
// on): each MCPcell service an app uses is added to the apps that do not
// have it yet, so a server set up in one app shows up in all of them. It
// runs after 一键托管 (POST /api/sync, which the page and `mcpcell
// takeover` call) and after automatic takeover took something new; the
// page sees beforehand where each MCP would go (mcpRow.SyncTo).
//
// Left out: services that do not start or whose program is gone (they
// would only fail in every app), ones no app has turned on, ones only a
// Claude Code project uses (a project's servers are its own),
// remote servers (MCPcell does not run them), an app with another server
// of that name, and an app the user took the service back from: they
// restored it (an exclusion), or deleted the entry MCPcell had added (its
// record is still there).

type syncResult struct {
	Client  string `json:"client"`
	Service string `json:"service"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// handleSync is POST /api/sync, optionally {"clients": [...]} to add only
// to those apps.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if s.opts.Takeovers == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "没有接管记录"})
		return
	}
	var req struct{ Clients []string }
	if body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<12)); len(body) > 0 && json.Unmarshal(body, &req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	s.takeMu.Lock()
	defer s.takeMu.Unlock()
	results, err := s.syncApps(req.Clients)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// syncApps adds every row to the apps in its SyncTo (only those in only,
// when given); the caller holds takeMu.
func (s *Server) syncApps(only []string) ([]syncResult, error) {
	u, err := s.unified()
	if err != nil {
		return nil, err
	}
	results := []syncResult{}
	for _, row := range u.Rows {
		for _, id := range row.SyncTo {
			c := clients.FindClient(s.opts.Clients, id)
			if c == nil || len(only) > 0 && !slices.Contains(only, id) {
				continue
			}
			res := syncResult{Client: id, Service: row.Service}
			if _, err := s.addRow(c, row); err != nil {
				res.Error = err.Error()
			} else {
				res.OK = true
			}
			results = append(results, res)
		}
	}
	return results, nil
}

// markSync sets each row's SyncTo: the apps a sync would add it to.
func (s *Server) markSync(rows []*mcpRow, agents []agentSummary, services map[string]*registry.Service) {
	if s.opts.Takeovers == nil {
		return
	}
	failing := map[string]bool{}
	for _, st := range s.pool.Status() {
		if st.State == "failed" || st.Failures > 0 {
			failing[st.Name] = true
		}
	}
	found := map[string]bool{}
	for _, a := range agents {
		found[a.ID] = a.Found && a.Error == ""
	}
	taken := map[string]bool{} // agent and entry name, every entry
	for _, r := range rows {
		for id, cell := range r.Agents {
			taken[id+"\x00"+cell.Entry] = true
		}
	}
	for _, r := range rows {
		if r.Service == "" || r.Kind != "stdio" || failing[r.Service] || !usedByApp(r) {
			continue
		}
		if def := services[r.Service]; def != nil && clients.Missing(clients.Entry{Command: def.Command, Args: def.Args, Env: def.Env, Cwd: def.Cwd}) != "" {
			continue
		}
		for _, c := range s.opts.Clients { // the apps themselves, not Claude Code projects
			id := c.ID()
			if _, has := r.Agents[id]; has || !found[id] || taken[id+"\x00"+r.Service] {
				continue
			}
			if _, kept := s.opts.Takeovers.Get(id, r.Service); kept {
				continue // added once, then deleted in the app
			}
			if _, no := s.opts.Takeovers.Excluded(id, r.Service); no {
				continue
			}
			r.SyncTo = append(r.SyncTo, id)
		}
	}
}

// usedByApp reports whether an app, outside a Claude Code project, runs the
// row through MCPcell and has it turned on.
func usedByApp(row *mcpRow) bool {
	for id, cell := range row.Agents {
		if _, project := clients.ProjectDir(id); !project && cell.State == "managed" && cell.Enabled {
			return true
		}
	}
	return false
}

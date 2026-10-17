package httpd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/registry"
)

type clientsResponse struct {
	Clients   []clients.ClientView `json:"clients"`
	Autostart autostartStatus      `json:"autostart"`
}

func (s *Server) baseURL() string { return "http://127.0.0.1:" + itoa(s.opts.Port) }

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, clientsResponse{
		Clients:   clients.Overview(s.opts.Clients, s.opts.Takeovers, cfg.Services, s.baseURL()),
		Autostart: s.autostart(),
	})
}

type takeoverRequest struct {
	Names []string `json:"names"`
	// Force allows sharing a server MCPcell does not know to be stateless.
	Force bool `json:"force"`
}

type takeoverResult struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Service string `json:"service,omitempty"`
	Created bool   `json:"created,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) readTakeover(w http.ResponseWriter, r *http.Request) (clients.Client, takeoverRequest, bool) {
	var req takeoverRequest
	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err := json.Unmarshal(body, &req); err != nil || len(req.Names) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return nil, req, false
	}
	c := clients.FindClient(s.opts.Clients, r.PathValue("client"))
	if c == nil || s.opts.Takeovers == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "不支持这个客户端"})
		return nil, req, false
	}
	return c, req, true
}

func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	c, req, ok := s.readTakeover(w, r)
	if !ok {
		return
	}
	s.takeMu.Lock()
	defer s.takeMu.Unlock()
	results := make([]takeoverResult, 0, len(req.Names))
	for _, name := range req.Names {
		res := takeoverResult{Name: name}
		res.Service, res.Created, res.OK = s.attachOne(c, name, req.Force, &res.Error)
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// attachOne makes sure the target service exists, then rewrites the entry.
func (s *Server) attachOne(c clients.Client, name string, force bool, errText *string) (service string, created, ok bool) {
	fail := func(msg string) (string, bool, bool) { *errText = msg; return service, created, false }
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return fail(err.Error())
	}
	views := clients.Overview([]clients.Client{c}, s.opts.Takeovers, cfg.Services, s.baseURL())
	if views[0].Error != "" {
		return fail(views[0].Error)
	}
	var ev *clients.EntryView
	for i := range views[0].Entries {
		if views[0].Entries[i].Name == name {
			ev = &views[0].Entries[i]
		}
	}
	switch {
	case ev == nil:
		return fail("配置里没有这个条目")
	case ev.TakenOver:
		return ev.Service, false, true
	case ev.Class == clients.ClassUnknown && force && ev.Kind == "stdio" && len(ev.EnvVars) == 0:
	case ev.Class != clients.ClassShared:
		return fail(ev.Reason)
	}
	service = ev.Service
	if service == "" {
		return fail("无法为它生成服务名，请先在服务页手动添加")
	}
	if !ev.ServiceExists {
		def := clients.Definition(ev.Entry)
		err := s.editConfig(func(cfg *registry.Config) error {
			if _, exists := cfg.Services[service]; exists {
				return errors.New("同名服务刚被添加，请刷新后重试")
			}
			cfg.Services[service] = def
			return nil
		})
		if err != nil {
			return fail(err.Error())
		}
		s.pool.Put(service, def)
		created = true
	}
	rec, err := c.Attach(name, s.baseURL()+"/s/"+service, s.opts.Token)
	if err != nil {
		return fail(err.Error())
	}
	rec.Service = service
	if err := s.opts.Takeovers.Put(rec); err != nil {
		return fail("已改写客户端配置，但保存接管记录失败：" + err.Error())
	}
	s.opts.Log.Info("taken over", "client", c.ID(), "entry", name, "service", service)
	return service, created, true
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	c, req, ok := s.readTakeover(w, r)
	if !ok {
		return
	}
	s.takeMu.Lock()
	defer s.takeMu.Unlock()
	forget := r.URL.Query().Get("forget") == "1"
	results := make([]takeoverResult, 0, len(req.Names))
	for _, name := range req.Names {
		res := takeoverResult{Name: name}
		rec, found := s.opts.Takeovers.Get(c.ID(), name)
		switch {
		case !found:
			res.Error = "没有接管记录"
		case forget:
			// The user changed the entry after takeover; keep their edit and
			// just drop the record.
			res.OK = s.opts.Takeovers.Delete(c.ID(), name) == nil
		default:
			if err := c.Restore(rec); err != nil {
				res.Error = err.Error()
				if errors.Is(err, clients.ErrConflict) {
					res.Error = "这个条目在接管后被改过，没有覆盖；可以选择忘记记录"
				}
			} else {
				res.OK = s.opts.Takeovers.Delete(c.ID(), name) == nil
				s.opts.Log.Info("restored", "client", c.ID(), "entry", name)
			}
		}
		res.Service = rec.Service
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

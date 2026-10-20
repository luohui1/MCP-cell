package httpd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

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
	// Force shares a server MCPcell does not know to be stateless; without
	// it such a server gets one instance per session.
	Force bool `json:"force"`
	// Mode "per-session" runs every service the takeover creates one
	// instance per session, even one that could be shared; "shared" is Force.
	// Existing services keep their mode.
	Mode string `json:"mode,omitempty"`
}

// modeFor is how a service created for an entry of class runs. A server
// that needs session isolation never becomes shared.
func (req takeoverRequest) modeFor(class clients.Class) string {
	if req.Mode == registry.ModePerSession {
		return registry.ModePerSession
	}
	return clients.ModeFor(class, req.Force || req.Mode == registry.ModeShared)
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
	if err := json.Unmarshal(body, &req); err != nil || len(req.Names) == 0 ||
		req.Mode != "" && req.Mode != registry.ModeShared && req.Mode != registry.ModePerSession {
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

// target is what an entry taken over for service becomes: its own command
// behind the wrapper when one is installed, otherwise the HTTP endpoint.
func (s *Server) target(service string) clients.Target {
	t := clients.Target{URL: s.baseURL() + "/s/" + service, Token: s.opts.Token}
	if s.opts.Wrapper != "" {
		t.Wrapper = s.opts.Wrapper
		if def := s.pool.Definition(service); def != nil {
			t.Server = clients.Entry{Kind: "stdio", Command: def.Command, Args: def.Args, Env: def.Env, Cwd: def.Cwd}
		}
	}
	return t
}

func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	c, req, ok := s.readTakeover(w, r)
	if !ok {
		return
	}
	s.takeMu.Lock()
	defer s.takeMu.Unlock()
	tried := s.tryForTakeover(c, req)
	results := make([]takeoverResult, 0, len(req.Names))
	for _, name := range req.Names {
		res := takeoverResult{Name: name}
		res.Service, res.Created, res.OK = s.attachOne(c, name, req, tried, &res.Error)
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// tryTimeout bounds one start check; a first start may install an npm
// package.
const tryTimeout = 3 * time.Minute

// tryService makes sure MCPcell can start what an entry would be pointed
// at: def for a service about to be added, otherwise the configured service
// called name, unless it has started before (it has a catalog). An entry
// whose server does not start under MCPcell, often because it does not start
// at all, stays native: MCPcell would only try it again for every session.
func (s *Server) tryService(name string, def *registry.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
	defer cancel()
	if def != nil {
		return s.pool.Try(ctx, name, def)
	}
	if s.pool.Cataloged(name) {
		return nil
	}
	return s.pool.Warm(ctx, name)
}

// tryForTakeover runs the start checks of a takeover side by side, before
// any entry is rewritten. Results are by service name; attachOne checks a
// service missing here itself.
func (s *Server) tryForTakeover(c clients.Client, req takeoverRequest) map[string]error {
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return nil
	}
	views := clients.Overview([]clients.Client{c}, s.opts.Takeovers, cfg.Services, s.baseURL())
	if views[0].Error != "" {
		return nil
	}
	todo := map[string]*registry.Service{}
	for _, ev := range views[0].Entries {
		if !slices.Contains(req.Names, ev.Name) || ev.TakenOver || !clients.Takeable(ev) || ev.Service == "" {
			continue
		}
		if ev.ServiceExists {
			todo[ev.Service] = nil
		} else {
			todo[ev.Service] = clients.Definition(ev.Entry, req.modeFor(ev.Class))
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := map[string]error{}
	for name, def := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.tryService(name, def)
			mu.Lock()
			out[name] = err
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// startProblem says in one line why a server did not start, with the last
// thing it printed to stderr, which usually names the cause.
func startProblem(err error) string {
	head, tail, _ := strings.Cut(err.Error(), "\nstderr：")
	msg := firstLine(head)
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		msg += "；它的输出：" + last
	}
	return msg
}

// attachOne makes sure the target service exists and starts, then rewrites
// the entry. tried holds start checks already run, by service name.
func (s *Server) attachOne(c clients.Client, name string, req takeoverRequest, tried map[string]error, errText *string) (service string, created, ok bool) {
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
	case !clients.Takeable(*ev):
		return fail(ev.Reason)
	}
	service = ev.Service
	if service == "" {
		return fail("无法为它生成服务名，请先在服务页手动添加")
	}
	var def *registry.Service
	if !ev.ServiceExists {
		def = clients.Definition(ev.Entry, req.modeFor(ev.Class))
	}
	err, done := tried[service]
	if !done {
		err = s.tryService(service, def)
	}
	if err != nil {
		s.opts.Log.Warn("not taken over: the service does not start", "client", c.ID(), "entry", name, "service", service, "err", firstLine(err.Error()))
		return fail("MCPcell 试着启动它失败，没有接管，条目保持原样：" + startProblem(err))
	}
	if !ev.ServiceExists {
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
		s.pool.Put(service, def) // the check above cached its catalog
		created = true
	}
	rec, err := c.Attach(name, s.target(service))
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

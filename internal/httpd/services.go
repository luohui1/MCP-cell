package httpd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/luohui1/MCP-cell/internal/registry"
)

// serviceForm is a service definition as the status page edits it. Env values
// never travel to the browser: GET sends each key with null, and a null on
// save means "keep the stored value".
type serviceForm struct {
	Name        string             `json:"name"`
	Command     string             `json:"command"`
	Args        []string           `json:"args"`
	Env         map[string]*string `json:"env"`
	Cwd         string             `json:"cwd"`
	Mode        string             `json:"mode"`        // shared | per-session
	IdleTimeout string             `json:"idleTimeout"` // "" default, "0" never
	MemoryLimit string             `json:"memoryLimit"` // "" none
	Flatten     bool               `json:"flatten"`
	Catalog     bool               `json:"catalog"`
	Lean        bool               `json:"lean"`
}

func formOf(name string, s *registry.Service) serviceForm {
	f := serviceForm{Name: name, Command: s.Command, Args: s.Args, Env: map[string]*string{}, Cwd: s.Cwd, Mode: s.Mode,
		Flatten: s.FlattenEnabled(), Catalog: s.CatalogEnabled(), Lean: s.LeanEnabled()}
	if f.Args == nil {
		f.Args = []string{}
	}
	for k := range s.Env {
		f.Env[k] = nil
	}
	if s.IdleTimeout.Set {
		f.IdleTimeout = s.IdleTimeout.Text()
	}
	if s.MemoryLimit > 0 {
		f.MemoryLimit = s.MemoryLimit.Text()
	}
	return f
}

// service turns the form into a validated definition, filling kept env values
// from prev.
func (f serviceForm) service(prev *registry.Service) (*registry.Service, error) {
	env := map[string]string{}
	for k, v := range f.Env {
		switch {
		case v != nil:
			env[k] = *v
		case prev != nil && prev.Env != nil:
			old, ok := prev.Env[k]
			if !ok {
				return nil, errors.New("环境变量 " + k + " 没有填写值")
			}
			env[k] = old
		default:
			return nil, errors.New("环境变量 " + k + " 没有填写值")
		}
	}
	raw := map[string]any{"command": f.Command}
	if len(f.Args) > 0 {
		raw["args"] = f.Args
	}
	if len(env) > 0 {
		raw["env"] = env
	}
	if f.Cwd != "" {
		raw["cwd"] = f.Cwd
	}
	if f.Mode != "" {
		raw["mode"] = f.Mode
	}
	if f.IdleTimeout != "" {
		raw["idleTimeout"] = f.IdleTimeout
	}
	if f.MemoryLimit != "" {
		raw["memoryLimit"] = f.MemoryLimit
	}
	if !f.Flatten {
		raw["flatten"] = false
	}
	if !f.Catalog {
		raw["catalog"] = "off"
	}
	if !f.Lean {
		raw["lean"] = false
	}
	b, _ := json.Marshal(raw)
	return registry.ParseService(f.Name, b)
}

func (s *Server) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := []serviceForm{}
	for name, svc := range cfg.Services {
		out = append(out, formOf(name, svc))
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	writeJSON(w, http.StatusOK, out)
}

// editConfig re-reads config.json, applies change and saves, so hand edits to
// other entries are not overwritten.
func (s *Server) editConfig(change func(cfg *registry.Config) error) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return err
	}
	if err := change(cfg); err != nil {
		return err
	}
	return registry.Save(s.opts.Home, cfg)
}

var errExists = errors.New("已有同名服务")

func (s *Server) handleSaveService(w http.ResponseWriter, r *http.Request) {
	var f serviceForm
	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := json.Unmarshal(body, &f); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	creating := r.Method == http.MethodPost
	if !creating {
		f.Name = r.PathValue("name") // renaming is not supported; the client URL depends on it
	}
	var saved *registry.Service
	err := s.editConfig(func(cfg *registry.Config) error {
		prev, exists := cfg.Services[f.Name]
		switch {
		case creating && exists:
			return errExists
		case !creating && !exists:
			return errors.New("服务不存在")
		}
		svc, err := f.service(prev)
		if err != nil {
			return err
		}
		cfg.Services[f.Name] = svc
		saved = svc
		return nil
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errExists) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	s.pool.Put(f.Name, saved)
	go s.warm(f.Name)
	if !creating {
		// The edited definition may expose different tools.
		s.notifyListChanged(f.Name, "tools/list")
	}
	s.opts.Log.Info("service saved", "service", f.Name, "created", creating)
	writeJSON(w, http.StatusOK, formOf(f.Name, saved))
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.editConfig(func(cfg *registry.Config) error {
		if _, ok := cfg.Services[name]; !ok {
			return errors.New("服务不存在")
		}
		delete(cfg.Services, name)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	s.pool.Remove(name)
	s.mu.Lock()
	var gone []*session
	for _, ss := range s.sessions {
		if ss.service == name {
			gone = append(gone, ss)
		}
	}
	s.mu.Unlock()
	for _, ss := range gone {
		s.endSession(ss)
	}
	s.opts.Log.Info("service deleted", "service", name)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// warm starts a new or changed service once in the background: it checks that
// the service starts and caches its catalog, so the sessions that connect next
// are answered from the catalog instead of each starting an instance.
func (s *Server) warm(name string) {
	if s.pool.Cataloged(name) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := s.pool.Warm(ctx, name); err != nil {
		s.opts.Log.Warn("service does not start", "service", name, "err", firstLine(err.Error()))
		return
	}
	s.opts.Log.Info("service checked and catalog cached", "service", name)
}

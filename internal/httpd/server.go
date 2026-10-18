// Package httpd is the daemon's loopback HTTP surface: MCP endpoints at
// /s/{service}, the management API under /api, and the status page under /ui.
package httpd

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/autostart"
	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/pool"
	"github.com/luohui1/MCP-cell/internal/proc"
)

// The status page is built from web/ (npm run build) into ui/dist.
//
//go:embed all:ui/dist
var uiFiles embed.FS

type Options struct {
	Port       int
	Token      string
	Version    string
	ConfigPath string
	Home       string // config.json lives here; the page edits services through it
	// MaxInstances is reported on the settings page.
	MaxInstances int
	Log          *slog.Logger

	// Clients and Takeovers enable pointing client configs at MCPcell.
	Clients   []clients.Client
	Takeovers *clients.Store
	// Exe is the running binary, for registering autostart.
	Exe string
	// Shutdown stops the daemon (mcpcell stop).
	Shutdown func()
	// Desktop is the tray icon and app window, if this daemon has them.
	Desktop Desktop
	// Wrapper is the installed mcpcell-run. Takeovers keep the entry's
	// command and wrap it; without it they point entries at /s/<name>.
	Wrapper string
}

type Server struct {
	opts    Options
	pool    *pool.Pool
	started time.Time
	cookie  string // per-run UI session cookie value

	mu       sync.Mutex
	sessions map[string]*session
	codes    map[string]time.Time // one-time UI login codes

	cfgMu  sync.Mutex // serializes config.json edits
	takeMu sync.Mutex // serializes client config takeovers
}

func New(p *pool.Pool, opts Options) *Server {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	s := &Server{opts: opts, pool: p, started: time.Now(), cookie: randomHex(32),
		sessions: map[string]*session{}, codes: map[string]time.Time{}}
	p.OnListChange(s.notifyListChanged)
	return s
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/s/{name}", s.handleMCP)
	mux.HandleFunc("GET /run", s.handleRun)
	mux.HandleFunc("GET /api/status", s.api(s.handleStatus))
	mux.HandleFunc("POST /api/services/{name}/stop", s.api(s.handleStop))
	mux.HandleFunc("POST /api/services/{name}/start", s.api(s.handleStart))
	mux.HandleFunc("POST /api/services/{name}/update", s.api(s.handleUpdate))
	mux.HandleFunc("GET /api/configs", s.api(s.handleListConfigs))
	mux.HandleFunc("POST /api/services", s.api(s.handleSaveService))
	mux.HandleFunc("PUT /api/services/{name}", s.api(s.handleSaveService))
	mux.HandleFunc("DELETE /api/services/{name}", s.api(s.handleDeleteService))
	mux.HandleFunc("GET /api/clients", s.api(s.handleClients))
	mux.HandleFunc("GET /api/mcp", s.api(s.handleUnified))
	mux.HandleFunc("POST /api/agents/{client}/add", s.api(s.handleAddToAgent))
	mux.HandleFunc("POST /api/clients/{client}/attach", s.api(s.handleAttach))
	mux.HandleFunc("POST /api/clients/{client}/restore", s.api(s.handleRestore))
	mux.HandleFunc("GET /api/autostart", s.api(s.handleAutostart))
	mux.HandleFunc("POST /api/autostart", s.api(s.handleAutostart))
	mux.HandleFunc("POST /api/shutdown", s.api(s.handleShutdown))
	mux.HandleFunc("GET /api/desktop", s.api(s.handleDesktop))
	mux.HandleFunc("POST /api/desktop", s.api(s.handleDesktop))
	mux.HandleFunc("POST /api/window", s.api(s.handleWindow))
	mux.HandleFunc("POST /api/login-code", s.handleLoginCode)
	mux.HandleFunc("GET /ui/login", s.handleLogin)
	sub, _ := fs.Sub(uiFiles, "ui/dist")
	mux.Handle("GET /ui/", s.requireUI(http.StripPrefix("/ui/", http.FileServerFS(sub))))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	return s.guard(mux)
}

// Run expires idle sessions until ctx ends, then closes the rest.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.closeAllSessions()
			return
		case now := <-t.C:
			s.expireSessions(now)
		}
	}
}

// guard rejects DNS-rebinding and cross-site requests: the Host must be our
// loopback address and any Origin must be the status page itself.
func (s *Server) guard(next http.Handler) http.Handler {
	port := strconv.Itoa(s.opts.Port)
	hosts := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true, "[::1]:" + port: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[strings.ToLower(r.Host)] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !hosts[strings.TrimPrefix(strings.ToLower(o), "http://")] {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) bearerOK(r *http.Request) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(tok)), []byte(s.opts.Token)) == 1
}

func (s *Server) cookieOK(r *http.Request) bool {
	c, err := r.Cookie("mcpcell")
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.cookie)) == 1
}

// api accepts the bearer token (CLI) or the UI cookie. Cookie-authenticated
// writes also need a custom header, which a cross-site form cannot send.
func (s *Server) api(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case s.bearerOK(r):
		case s.cookieOK(r) && (r.Method == http.MethodGet || r.Header.Get("X-MCPcell") == "1"):
		default:
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未授权"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func (s *Server) requireUI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cookieOK(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(loginHint))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

const loginHint = `<!doctype html><meta charset="utf-8"><title>MCPcell</title>
<body style="font:15px system-ui;margin:15vh auto;max-width:28rem;padding:0 16px;color:#333">
<h1 style="font-size:20px">MCPcell</h1><p>请在终端运行 <code>mcpcell ui</code> 打开管理页。</p></body>`

func (s *Server) handleLoginCode(w http.ResponseWriter, r *http.Request) {
	if !s.bearerOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未授权"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": s.LoginURL()})
}

// LoginURL mints a one-time link that opens the status page (valid for a
// minute, usable once).
func (s *Server) LoginURL() string {
	code := randomHex(16)
	s.mu.Lock()
	now := time.Now()
	for c, exp := range s.codes {
		if now.After(exp) {
			delete(s.codes, c)
		}
	}
	s.codes[code] = now.Add(time.Minute)
	s.mu.Unlock()
	return "http://127.0.0.1:" + strconv.Itoa(s.opts.Port) + "/ui/login?code=" + code
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	s.mu.Lock()
	exp, ok := s.codes[code]
	delete(s.codes, code)
	s.mu.Unlock()
	if !ok || time.Now().After(exp) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(strings.Replace(loginHint, "打开管理页", "重新打开管理页（链接只能用一次，1 分钟内有效）", 1)))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "mcpcell", Value: s.cookie, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

type statusResponse struct {
	Version      string               `json:"version"`
	PID          int                  `json:"pid"`
	Port         int                  `json:"port"`
	StartedAt    time.Time            `json:"startedAt"`
	BaseURL      string               `json:"baseUrl"`
	Token        string               `json:"token"`
	ConfigPath   string               `json:"configPath"`
	MaxInstances int                  `json:"maxInstances"`
	Daemon       proc.Usage           `json:"daemon"`
	Services     []pool.ServiceStatus `json:"services"`
	Sessions     []sessionInfo        `json:"sessions"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{
		Version: s.opts.Version, PID: pid(), Port: s.opts.Port, StartedAt: s.started,
		BaseURL: "http://127.0.0.1:" + strconv.Itoa(s.opts.Port), Token: s.opts.Token, ConfigPath: s.opts.ConfigPath, MaxInstances: s.opts.MaxInstances,
		Daemon: proc.Self(), Services: s.pool.Status(), Sessions: s.sessionInfos(),
	})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !s.pool.Has(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未知服务"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": s.pool.Stop(name, "手动回收")})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	name := r.PathValue("name")
	var err error
	if s.pool.PerSession(name) {
		// A per-session service only runs for a session; starting it here
		// just checks that it starts and caches its catalog.
		err = s.pool.Warm(ctx, name)
	} else {
		_, err = s.pool.Acquire(ctx, name, nil)
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, pool.ErrUnknownService) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": firstLine(err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"started": true})
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	now, before, err := s.pool.Update(ctx, r.PathValue("name"))
	if err != nil && now == nil {
		status := http.StatusBadRequest
		if errors.Is(err, pool.ErrUnknownService) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": firstLine(err.Error())})
		return
	}
	out := map[string]string{"version": now.Version}
	if before != nil {
		out["previous"] = before.Version
	}
	if err != nil {
		out["error"] = firstLine(err.Error())
	}
	writeJSON(w, http.StatusOK, out)
}

type autostartStatus = autostart.Status

func (s *Server) autostart() autostartStatus { return autostart.Get() }

func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct{ Enabled bool }
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		var err error
		if req.Enabled {
			err = autostart.Enable(s.opts.Exe)
		} else {
			err = autostart.Disable()
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, s.autostart())
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if s.opts.Shutdown == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "不支持"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stopping": true})
	go s.opts.Shutdown()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// Listen binds the loopback port, explaining the usual cause of failure.
func Listen(port int) (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, errors.New("无法监听 127.0.0.1:" + strconv.Itoa(port) + "，端口可能已被占用（MCPcell 是否已在运行？可用 mcpcell ps 查看）")
	}
	return l, nil
}

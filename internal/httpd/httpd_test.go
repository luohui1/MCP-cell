package httpd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/pool"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
	"github.com/luohui1/MCP-cell/internal/testutil/fakemcp"
)

func TestMain(m *testing.M) {
	fakemcp.MaybeRun()
	os.Exit(m.Run())
}

var token = strings.Repeat("ab", 32)

type harness struct {
	srv  *Server
	base string // http://127.0.0.1:port
}

func start(t *testing.T) *harness { return startWith(t, nil) }

// startWithHome also gives the server a home with config.json, so the
// service editing API works.
func startWithHome(t *testing.T) (*harness, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"services":{"fake":{"command":"fake"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := startWith(t, nil)
	h.srv.opts.Home = home
	return h, home
}

// startWith runs the daemon's HTTP side; a non-nil store enables the catalog.
func startWith(t *testing.T, store *catalog.Store) *harness {
	t.Helper()
	cfg, err := registry.Parse([]byte(`{"services":{"fake":{"command":"fake"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	p := pool.New(cfg, pool.Options{Version: "test", Catalog: store,
		SpecFor: func(string, *registry.Service) proc.Spec { return fakemcp.Spec() }})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	srv := New(p, Options{Port: port, Token: token, Version: "test"})
	hs := &http.Server{Handler: srv.Handler()}
	go hs.Serve(l)
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Run(ctx)
	t.Cleanup(func() {
		cancel()
		hs.Close()
		p.Close()
	})
	return &harness{srv: srv, base: fmt.Sprintf("http://127.0.0.1:%d", port)}
}

type authRT struct{ base http.RoundTripper }

func (a authRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return a.base.RoundTrip(r)
}

func (h *harness) connect(t *testing.T, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "sdk-test", Version: "1.0"}, opts)
	tr := &mcp.StreamableClientTransport{Endpoint: h.base + "/s/fake",
		HTTPClient: &http.Client{Transport: authRT{http.DefaultTransport}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := client.Connect(ctx, tr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callText(t *testing.T, cs *mcp.ClientSession, params *mcp.CallToolParams) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("call %s: %v", params.Name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("empty result for %s", params.Name)
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func (h *harness) status(t *testing.T) statusResponse {
	t.Helper()
	req, _ := http.NewRequest("GET", h.base+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

// M1 acceptance: six concurrent clients, one upstream process. The SDK client
// tries the 2026-07-28 server/discover first and must fall back to initialize.
func TestSixClientsShareOneUpstream(t *testing.T) {
	h := start(t)
	sessions := make([]*mcp.ClientSession, 6)
	var wg sync.WaitGroup
	for n := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sessions[n] = h.connect(t, nil)
		}()
	}
	wg.Wait()
	pids := map[string]bool{}
	for _, cs := range sessions {
		if v := cs.InitializeResult().ProtocolVersion; v >= "2026" {
			t.Fatalf("client negotiated %s; the endpoint should be legacy-only", v)
		}
		pids[callText(t, cs, &mcp.CallToolParams{Name: "pid", Arguments: map[string]any{}})] = true
		tools, err := cs.ListTools(context.Background(), nil)
		if err != nil || len(tools.Tools) < 4 {
			t.Fatalf("list tools: %v %v", tools, err)
		}
	}
	if len(pids) != 1 {
		t.Fatalf("expected one upstream process, got pids %v", pids)
	}
	st := h.status(t)
	if len(st.Sessions) != 6 || len(st.Services) != 1 || st.Services[0].Instance == nil || st.Services[0].Instance.Sessions != 6 {
		t.Fatalf("status: %d sessions, services %+v", len(st.Sessions), st.Services)
	}
	if st.Services[0].Instance.Usage.PrivateBytes == 0 {
		t.Error("no memory reported for the instance")
	}
}

// Raw HTTP, JSON response mode: three sessions reuse ids 1..40 concurrently.
func TestRawSessionsReuseIDs(t *testing.T) {
	h := start(t)
	post := func(sid string, body string) (*http.Response, []byte) {
		req, _ := http.NewRequest("POST", h.base+"/s/fake", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, b
	}
	var sids []string
	for range 3 {
		resp, _ := post("", `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"raw","version":"1"}}}`)
		sid := resp.Header.Get("Mcp-Session-Id")
		if resp.StatusCode != 200 || sid == "" {
			t.Fatalf("initialize: %d sid=%q", resp.StatusCode, sid)
		}
		sids = append(sids, sid)
	}
	var wg sync.WaitGroup
	var bad atomic.Int32
	for s, sid := range sids {
		for n := 1; n <= 40; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				want := fmt.Sprintf("s%d-%d", s, n)
				_, b := post(sid, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"echo","arguments":{"text":%q}}}`, n, want))
				var m struct {
					ID     json.RawMessage
					Result struct{ Content []struct{ Text string } }
				}
				if json.Unmarshal(b, &m) != nil || string(m.ID) != fmt.Sprint(n) || len(m.Result.Content) == 0 || m.Result.Content[0].Text != want {
					bad.Add(1)
					t.Errorf("session %d id %d: %s", s, n, b)
				}
			}()
		}
	}
	wg.Wait()
	if bad.Load() > 0 {
		t.Fatalf("%d crossed or broken responses", bad.Load())
	}
}

func TestProgressOverSSE(t *testing.T) {
	h := start(t)
	var got atomic.Int32
	cs := h.connect(t, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			if req.Params.ProgressToken == "p1" {
				got.Add(1)
			}
		}})
	params := &mcp.CallToolParams{Name: "slow", Arguments: map[string]any{"ms": 200, "steps": 4}}
	params.SetProgressToken("p1")
	if txt := callText(t, cs, params); txt != "done" {
		t.Fatal(txt)
	}
	// The SDK client runs notification handlers concurrently with returning
	// the result, so the last one may land just after CallTool returns.
	for deadline := time.Now().Add(2 * time.Second); got.Load() < 4 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 4 {
		t.Fatalf("got %d progress notifications, want 4", got.Load())
	}
}

func TestListChangedReachesOtherClient(t *testing.T) {
	h := start(t)
	changed := make(chan struct{}, 4)
	b := h.connect(t, &mcp.ClientOptions{ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
		changed <- struct{}{}
	}})
	a := h.connect(t, nil)
	callText(t, b, &mcp.CallToolParams{Name: "pid", Arguments: map[string]any{}})
	time.Sleep(100 * time.Millisecond) // let b's GET stream open
	callText(t, a, &mcp.CallToolParams{Name: "add_tool", Arguments: map[string]any{"text": "x"}})
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("other client never saw tools/list_changed")
	}
}

func TestCloseEndsSession(t *testing.T) {
	h := start(t)
	cs := h.connect(t, nil)
	if n := len(h.status(t).Sessions); n != 1 {
		t.Fatalf("sessions = %d", n)
	}
	cs.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.status(t).Sessions) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("DELETE did not end the session")
}

func TestGuards(t *testing.T) {
	h := start(t)
	do := func(method, path, body string, hdr map[string]string) (*http.Response, string) {
		req, _ := http.NewRequest(method, h.base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range hdr {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(b)
	}
	auth := "Bearer " + token
	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		hdr    map[string]string
		want   int
		has    string
	}{
		{"no token", "POST", "/s/fake", discover, nil, 401, ""},
		{"wrong token", "POST", "/s/fake", discover, map[string]string{"Authorization": "Bearer nope"}, 401, ""},
		{"bad origin", "POST", "/s/fake", discover, map[string]string{"Authorization": auth, "Origin": "https://evil.example"}, 403, ""},
		{"rebinding host", "GET", "/api/status", "", map[string]string{"Authorization": auth, "Host": "evil.example:80"}, 403, ""},
		{"modern probe falls back", "POST", "/s/fake", discover, map[string]string{"Authorization": auth}, 400, `"code":-32600`},
		{"unknown session", "POST", "/s/fake", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Authorization": auth, "Mcp-Session-Id": "x"}, 404, ""},
		{"unknown service", "POST", "/s/nope", discover, map[string]string{"Authorization": auth}, 404, ""},
		{"api needs auth", "GET", "/api/status", "", nil, 401, ""},
		{"api with token", "GET", "/api/status", "", map[string]string{"Authorization": auth}, 200, `"services"`},
		{"ui without cookie", "GET", "/ui/", "", nil, 401, "mcpcell ui"},
	}
	for _, c := range cases {
		resp, body := do(c.method, c.path, c.body, c.hdr)
		if resp.StatusCode != c.want || !strings.Contains(body, c.has) {
			t.Errorf("%s: got %d %s", c.name, resp.StatusCode, body)
		}
	}
}

func TestUILogin(t *testing.T) {
	h := start(t)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("POST", h.base+"/api/login-code", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var lc struct{ URL string }
	_ = json.NewDecoder(resp.Body).Decode(&lc)
	resp.Body.Close()
	login := strings.Replace(lc.URL, "127.0.0.1:"+strings.Split(h.base, ":")[2], h.base[len("http://"):], 1)

	resp, err = noRedirect.Get(login)
	if err != nil || resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) == 0 {
		t.Fatalf("login: %v %v", resp.StatusCode, err)
	}
	cookie := resp.Cookies()[0]
	if again, _ := noRedirect.Get(login); again.StatusCode != http.StatusForbidden {
		t.Fatalf("login code reused: %d", again.StatusCode)
	}

	get := func(path string, hdr map[string]string, method string) int {
		req, _ := http.NewRequest(method, h.base+path, bytes.NewReader(nil))
		req.AddCookie(cookie)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get("/ui/", nil, "GET"); code != 200 {
		t.Fatalf("ui with cookie: %d", code)
	}
	index := func() string {
		req, _ := http.NewRequest("GET", h.base+"/ui/", nil)
		req.AddCookie(cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}()
	asset := regexp.MustCompile(`/ui/assets/[^"]+\.js`).FindString(index)
	if asset == "" {
		t.Fatalf("built index.html has no script asset: %s", index)
	}
	if code := get(asset, nil, "GET"); code != 200 {
		t.Fatalf("%s: %d", asset, code)
	}
	if code := get("/api/services/fake/stop", nil, "POST"); code != 401 {
		t.Fatalf("cookie write without X-MCPcell header: %d", code)
	}
	if code := get("/api/services/fake/start", map[string]string{"X-MCPcell": "1"}, "POST"); code != 200 {
		t.Fatalf("start via ui: %d", code)
	}
	if code := get("/api/services/fake/stop", map[string]string{"X-MCPcell": "1"}, "POST"); code != 200 {
		t.Fatalf("stop via ui: %d", code)
	}
}

func TestOrphanedSessionExpires(t *testing.T) {
	h := start(t)
	open := func(withStream bool) string {
		req, _ := http.NewRequest("POST", h.base+"/s/fake", strings.NewReader(
			`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw","version":"1"}}}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		sid := resp.Header.Get("Mcp-Session-Id")
		if withStream {
			ctx, cancel := context.WithCancel(context.Background())
			get, _ := http.NewRequestWithContext(ctx, "GET", h.base+"/s/fake", nil)
			get.Header.Set("Authorization", "Bearer "+token)
			get.Header.Set("Accept", "text/event-stream")
			get.Header.Set("Mcp-Session-Id", sid)
			resp, err := http.DefaultClient.Do(get)
			if err != nil {
				t.Fatal(err)
			}
			cancel() // the client goes away without DELETE
			resp.Body.Close()
			time.Sleep(100 * time.Millisecond)
		}
		return sid
	}
	exited, quiet := open(true), open(false)
	h.srv.expireSessions(time.Now().Add(11 * time.Minute))
	h.srv.mu.Lock()
	_, exitedLeft := h.srv.sessions[exited]
	_, quietLeft := h.srv.sessions[quiet]
	h.srv.mu.Unlock()
	if exitedLeft || !quietLeft {
		t.Fatalf("after 11 idle minutes: exited client kept=%v, stream-less client kept=%v", exitedLeft, quietLeft)
	}
}

func (h *harness) running(t *testing.T) bool {
	t.Helper()
	return h.status(t).Services[0].Instance != nil
}

func (h *harness) waitCatalog(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if c := h.status(t).Services[0].Catalog; c != nil && c.Tools > 0 {
			return
		}
	}
	t.Fatal("catalog never filled")
}

// M2 acceptance: with a catalog, connecting and listing start no process; the
// first tool call does.
func TestConnectAndListStartNothing(t *testing.T) {
	h := startWith(t, catalog.OpenStore(t.TempDir()))
	first := h.connect(t, nil) // no catalog yet: this one starts the upstream
	h.waitCatalog(t)
	first.Close()
	h.srv.pool.Stop("fake", "test")
	if h.running(t) {
		t.Fatal("instance still running after stop")
	}
	var clients []*mcp.ClientSession
	for range 3 {
		cs := h.connect(t, nil)
		tools, err := cs.ListTools(context.Background(), nil)
		if err != nil || len(tools.Tools) != 5 {
			t.Fatalf("list from catalog: %v %v", tools, err)
		}
		clients = append(clients, cs)
	}
	if h.running(t) {
		t.Fatal("connecting or listing started the upstream")
	}
	callText(t, clients[0], &mcp.CallToolParams{Name: "pid", Arguments: map[string]any{}})
	if !h.running(t) {
		t.Fatal("tool call did not start the upstream")
	}
}

func TestCatalogChangeReachesSessionThatNeverCalled(t *testing.T) {
	h := startWith(t, catalog.OpenStore(t.TempDir()))
	h.connect(t, nil)
	h.waitCatalog(t)
	changed := make(chan struct{}, 4)
	cold := h.connect(t, &mcp.ClientOptions{ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
		changed <- struct{}{}
	}})
	if _, err := cold.ListTools(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let its GET stream open
	other := h.connect(t, nil)
	callText(t, other, &mcp.CallToolParams{Name: "add_tool", Arguments: map[string]any{"text": "late"}})
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("a session served only from the catalog never heard list_changed")
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		tools, _ := cold.ListTools(context.Background(), nil)
		for _, tool := range tools.Tools {
			if tool.Name == "extra_late" {
				return
			}
		}
	}
	t.Fatal("re-listing never showed the new tool")
}

func (h *harness) apiCall(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServiceEditingAPI(t *testing.T) {
	h, home := startWithHome(t)
	form := `{"name":"added","command":"x","args":["a"],"env":{"API_KEY":"s3cret"},"idleTimeout":"5m","flatten":true,"catalog":true,"lean":true}`
	if code, body := h.apiCall(t, "POST", "/api/services", form); code != 200 {
		t.Fatalf("create: %d %s", code, body)
	}
	if code, _ := h.apiCall(t, "POST", "/api/services", form); code != http.StatusConflict {
		t.Fatalf("duplicate create: %d", code)
	}
	if n := len(h.status(t).Services); n != 2 {
		t.Fatalf("services after create = %d", n)
	}
	_, list := h.apiCall(t, "GET", "/api/configs", "")
	if strings.Contains(list, "s3cret") || !strings.Contains(list, `"API_KEY":null`) {
		t.Fatalf("env value leaked or missing: %s", list)
	}
	// Saving with a null value keeps the stored secret.
	edit := `{"command":"x","args":["a","b"],"env":{"API_KEY":null},"idleTimeout":"0","flatten":false,"catalog":true,"lean":true}`
	if code, body := h.apiCall(t, "PUT", "/api/services/added", edit); code != 200 {
		t.Fatalf("update: %d %s", code, body)
	}
	cfg, err := registry.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Services["added"]
	if got.Env["API_KEY"] != "s3cret" || len(got.Args) != 2 || got.FlattenEnabled() || got.Idle() != 0 {
		t.Fatalf("saved definition: %+v", got)
	}
	if code, body := h.apiCall(t, "PUT", "/api/services/added", `{"command":"x","env":{"NEW":null}}`); code != 400 {
		t.Fatalf("null for an unknown env key must fail: %d %s", code, body)
	}
	if code, _ := h.apiCall(t, "DELETE", "/api/services/added", ""); code != 200 {
		t.Fatal("delete")
	}
	if cfg, _ := registry.Load(home); cfg.Services["added"] != nil || cfg.Services["fake"] == nil {
		t.Fatalf("config after delete: %+v", cfg.Services)
	}
	if n := len(h.status(t).Services); n != 1 {
		t.Fatalf("services after delete = %d", n)
	}
}

func TestTakeoverAPI(t *testing.T) {
	h, home := startWithHome(t)
	userHome := t.TempDir()
	cursorFile := filepath.Join(userHome, ".cursor", "mcp.json")
	original := `{
  "mcpServers": {
    "context7": { "command": "npx", "args": ["-y", "@upstash/context7-mcp"] },
    "mine": { "command": "python", "args": ["my_server.py"] },
    "remote": { "url": "https://example.com/mcp" }
  }
}
`
	_ = os.MkdirAll(filepath.Dir(cursorFile), 0o700)
	_ = os.WriteFile(cursorFile, []byte(original), 0o600)
	store, _ := clients.OpenStore(home)
	h.srv.opts.Clients = []clients.Client{clients.NewCursor(userHome, filepath.Join(home, "backups"))}
	h.srv.opts.Takeovers = store

	_, body := h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["context7","mine","remote"]}`)
	var out struct {
		Results []struct {
			Name, Service, Error string
			OK, Created          bool
		}
	}
	_ = json.Unmarshal([]byte(body), &out)
	if len(out.Results) != 3 || !out.Results[0].OK || !out.Results[0].Created || out.Results[0].Service != "context7" {
		t.Fatalf("attach: %s", body)
	}
	if out.Results[1].OK || out.Results[2].OK {
		t.Fatalf("unknown and remote entries must not be taken over without force: %s", body)
	}
	cfg, _ := registry.Load(home)
	if cfg.Services["context7"] == nil || cfg.Services["context7"].Args[1] != "@upstash/context7-mcp" {
		t.Fatalf("service not created from the entry: %+v", cfg.Services)
	}
	if b, _ := os.ReadFile(cursorFile); !strings.Contains(string(b), h.base+"/s/context7") || !strings.Contains(string(b), `"remote": { "url"`) {
		t.Fatalf("cursor config after attach:\n%s", b)
	}
	if _, body := h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["mine"],"force":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("forced attach: %s", body)
	}
	_, list := h.apiCall(t, "GET", "/api/clients", "")
	if !strings.Contains(list, `"takenOver":true`) || strings.Contains(list, "my_server.py\",\"env") {
		t.Fatalf("overview: %s", list)
	}
	if _, body := h.apiCall(t, "POST", "/api/clients/cursor/restore", `{"names":["context7","mine"]}`); strings.Count(body, `"ok":true`) != 2 {
		t.Fatalf("restore: %s", body)
	}
	if b, _ := os.ReadFile(cursorFile); string(b) != original {
		t.Fatalf("restore not byte-exact:\n%s", b)
	}
	if len(store.All()) != 0 {
		t.Fatal("records kept after restore")
	}
}

func TestUnifiedViewAndAddToAgent(t *testing.T) {
	h, home := startWithHome(t)
	userHome := t.TempDir()
	codexHome := filepath.Join(userHome, ".codex")
	t.Setenv("CODEX_HOME", codexHome) // never the real one
	cursorFile := filepath.Join(userHome, ".cursor", "mcp.json")
	_ = os.MkdirAll(filepath.Dir(cursorFile), 0o700)
	_ = os.MkdirAll(codexHome, 0o700)
	_ = os.WriteFile(cursorFile, []byte(`{"mcpServers": {"context7": {"command": "npx", "args": ["-y", "@upstash/context7-mcp"]}, "web": {"url": "https://example.com/mcp"}}}`), 0o600)
	codexOriginal := "model = \"x\"\n"
	_ = os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(codexOriginal), 0o600)
	store, _ := clients.OpenStore(home)
	h.srv.opts.Clients = []clients.Client{clients.NewCursor(userHome, ""), clients.NewCodex(userHome, "")}
	h.srv.opts.Takeovers = store

	type view struct {
		Rows []struct {
			Key, Name, Service string
			CanAdd             bool
			Agents             map[string]struct{ State string }
		}
	}
	load := func() view {
		_, body := h.apiCall(t, "GET", "/api/mcp", "")
		var v view
		_ = json.Unmarshal([]byte(body), &v)
		return v
	}
	v := load()
	var key string
	for _, r := range v.Rows {
		if r.Name == "context7" {
			key = r.Key
			if r.Agents["cursor"].State != "native" || r.Agents["codex"].State != "" || !r.CanAdd {
				t.Fatalf("context7 row before add: %+v", r)
			}
		}
	}
	if key == "" || len(v.Rows) < 3 { // fake service + context7 + web
		t.Fatalf("rows: %+v", v.Rows)
	}
	if code, body := h.apiCall(t, "POST", "/api/agents/codex/add", `{"row":"`+key+`"}`); code != 200 || !strings.Contains(body, "context7") {
		t.Fatalf("add: %d %s", code, body)
	}
	v = load()
	for _, r := range v.Rows {
		if r.Name == "context7" && (r.Service != "context7" || r.Agents["codex"].State != "managed" || r.Agents["cursor"].State != "native") {
			t.Fatalf("context7 row after add: %+v", r)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(codexHome, "config.toml")); !strings.Contains(string(b), h.base+"/s/context7") {
		t.Fatalf("codex config after add:\n%s", b)
	}
	if _, body := h.apiCall(t, "POST", "/api/clients/codex/restore", `{"names":["context7"]}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("remove: %s", body)
	}
	if b, _ := os.ReadFile(filepath.Join(codexHome, "config.toml")); string(b) != codexOriginal {
		t.Fatalf("codex config after remove:\n%q", b)
	}
}

func TestArgsTextHidesSecrets(t *testing.T) {
	got := argsText([]string{"--app", "desktop", "--api-key", "sk-1", "--token=abc", "-y"})
	if want := "--app desktop --api-key *** --token=*** -y"; got != want {
		t.Fatalf("argsText = %q, want %q", got, want)
	}
}

type fakeDesktop struct {
	shown             int
	desktop, startMnu bool
	missing           error
}

func (f *fakeDesktop) Show() error                          { f.shown++; return f.missing }
func (f *fakeDesktop) Available() error                     { return f.missing }
func (f *fakeDesktop) Shortcuts() (desktop, startMenu bool) { return f.desktop, f.startMnu }
func (f *fakeDesktop) SetShortcuts(desktop, startMenu *bool) error {
	if desktop != nil {
		f.desktop = *desktop
	}
	if startMenu != nil {
		f.startMnu = *startMenu
	}
	return nil
}

func TestDesktopAPI(t *testing.T) {
	h := start(t)
	if code, body := h.apiCall(t, "GET", "/api/desktop", ""); code != 200 || !strings.Contains(body, `"supported":false`) {
		t.Fatalf("no desktop: %d %s", code, body)
	}
	if code, _ := h.apiCall(t, "POST", "/api/window", ""); code != 400 {
		t.Fatalf("window without desktop: %d", code)
	}

	fake := &fakeDesktop{}
	h.srv.opts.Desktop = fake
	code, body := h.apiCall(t, "GET", "/api/desktop", "")
	if want := `{"supported":true,"webview":true,"desktopShortcut":false,"startMenuShortcut":false}`; code != 200 || strings.TrimSpace(body) != want {
		t.Fatalf("GET /api/desktop = %d %s, want %s", code, body, want)
	}
	code, body = h.apiCall(t, "POST", "/api/desktop", `{"startMenuShortcut":true}`)
	if code != 200 || !strings.Contains(body, `"desktopShortcut":false,"startMenuShortcut":true`) || fake.desktop {
		t.Fatalf("POST /api/desktop = %d %s", code, body)
	}
	if code, body := h.apiCall(t, "POST", "/api/window", ""); code != 200 || fake.shown != 1 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("POST /api/window = %d %s (shown %d)", code, body, fake.shown)
	}

	fake.missing = errors.New("没有找到 WebView2 运行时")
	if _, body := h.apiCall(t, "GET", "/api/desktop", ""); !strings.Contains(body, `"webview":false,"webviewError":"没有找到 WebView2 运行时"`) {
		t.Fatalf("missing runtime not reported: %s", body)
	}
	if code, _ := h.apiCall(t, "POST", "/api/window", ""); code != 500 {
		t.Fatalf("window without runtime: %d", code)
	}

	// LoginURL mints the same one-time links as /api/login-code.
	if !strings.HasPrefix(h.srv.LoginURL(), "http://127.0.0.1:") {
		t.Fatalf("LoginURL = %s", h.srv.LoginURL())
	}
}

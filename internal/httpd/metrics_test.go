package httpd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/events"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *harness) getJSON(t *testing.T, path string, v any) int {
	t.Helper()
	req, _ := http.NewRequest("GET", h.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

// What the dashboard shows: memory now and without sharing, the
// conversations, and each service's typical memory once a run ended.
func TestSampleMeasuresAndEstimates(t *testing.T) {
	h := start(t)
	a := h.connect(t, nil)
	h.connect(t, nil)
	callText(t, a, &mcp.CallToolParams{Name: "pid"})

	m := h.srv.sample(time.Now())
	if m.Instances != 1 || m.RunningServices != 1 || m.Services != 1 {
		t.Fatalf("counts: %+v", m)
	}
	if m.InstanceBytes == 0 || m.DaemonBytes == 0 || m.TotalBytes != m.DaemonBytes+m.InstanceBytes+m.WrapperBytes {
		t.Fatalf("memory: %+v", m)
	}
	// Two conversations, the instance measured now (no run ended yet).
	if m.EstimateBytes != 2*m.InstanceBytes || len(m.Unmeasured) != 0 {
		t.Fatalf("estimate %d for two sessions of %d, unmeasured %v", m.EstimateBytes, m.InstanceBytes, m.Unmeasured)
	}
	if len(m.Conversations) != 1 || m.Conversations[0] != (agentCount{Agent: "sdk-test", Conversations: 2}) {
		t.Fatalf("conversations: %+v", m.Conversations)
	}

	// The run ends: its peak becomes the service's typical memory.
	peak := m.InstanceBytes
	h.srv.pool.Stop("fake", "手动回收")
	m = h.srv.sample(time.Now())
	st := h.service(t, "fake")
	if st.TypicalRuns != 1 || st.TypicalBytes != peak {
		t.Fatalf("typical %d over %d runs, want %d", st.TypicalBytes, st.TypicalRuns, peak)
	}
	if m.Instances != 0 || m.InstanceBytes != 0 || m.EstimateBytes != 2*peak {
		t.Fatalf("after stopping: %+v", m)
	}
}

func TestSampleLeavesUnmeasuredOut(t *testing.T) {
	h := start(t)
	h.connect(t, nil)
	h.srv.pool.Stop("fake", "手动回收") // before any sample: never measured
	m := h.srv.sample(time.Now())
	if m.EstimateBytes != 0 || len(m.Unmeasured) != 1 || m.Unmeasured[0] != "fake" {
		t.Fatalf("estimate %d, unmeasured %v", m.EstimateBytes, m.Unmeasured)
	}
}

// Sessions from one agent process are one conversation, however many MCPs.
func TestConversationsByAgentProcess(t *testing.T) {
	h := startConfig(t, nil, `{"services":{}}`)
	env := map[string]string{"MCPCELL_FAKE_MCP": "1"}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" && env[k] == "" {
			env[k] = v
		}
	}
	for _, arg := range []string{"one-mcp", "another-mcp"} {
		c := dialRun(t, h, map[string]any{"argv": []string{"npx", "-y", arg}, "cwd": t.TempDir(), "env": env, "mode": "shared"})
		c.init()
	}
	m := h.srv.sample(time.Now())
	if runtime.GOOS != "windows" {
		t.Skip("the agent process is only looked up on Windows")
	}
	if len(m.Conversations) != 1 || m.Conversations[0] != (agentCount{Agent: "run-test", Conversations: 1}) {
		t.Fatalf("conversations: %+v", m.Conversations)
	}
	if m.Wrappers != 1 || m.WrapperBytes == 0 {
		t.Fatalf("the wrapper (this test process) counted %d times, %d bytes", m.Wrappers, m.WrapperBytes)
	}
}

func TestMetricsAndEventsAPI(t *testing.T) {
	h := start(t)
	a := h.connect(t, nil)
	callText(t, a, &mcp.CallToolParams{Name: "pid"})
	h.srv.sample(time.Now().Add(-20 * time.Second))
	h.srv.sample(time.Now())

	var hour metricsResponse
	if code := h.getJSON(t, "/api/metrics?range=1h", &hour); code != 200 {
		t.Fatalf("1h: %d", code)
	}
	if hour.Step != 10 || len(hour.Points) != 2 || hour.Now == nil || hour.Points[1].Total == 0 || hour.Points[1].Running != 1 {
		t.Fatalf("1h: %+v", hour)
	}
	var day metricsResponse
	h.getJSON(t, "/api/metrics?range=24h", &day)
	if day.Step != 300 || len(day.Points) == 0 || len(day.Points) > 2 {
		t.Fatalf("24h: %+v", day)
	}
	var one metricsResponse
	h.getJSON(t, "/api/metrics?range=1h&service=fake", &one)
	if one.Service != "fake" || len(one.Points) != 2 || one.Points[1].Total == 0 || one.Now != nil {
		t.Fatalf("one service: %+v", one)
	}
	var now metricsResponse
	h.getJSON(t, "/api/metrics?range=now", &now)
	if now.Now == nil || now.Now.TotalBytes == 0 || len(now.Points) != 0 {
		t.Fatalf("now: %+v", now)
	}
	if code := h.getJSON(t, "/api/metrics?range=7d", nil); code != http.StatusBadRequest {
		t.Fatalf("bad range: %d", code)
	}

	var none struct{ Events []events.Event }
	h.getJSON(t, "/api/events", &none)
	if none.Events == nil || len(none.Events) != 0 {
		t.Fatalf("without a log: %+v", none)
	}
	log := events.Open(filepath.Join(t.TempDir(), "events.json"))
	log.Add(events.Event{Kind: "started", Service: "fake"})
	log.Add(events.Event{Kind: "stopped_idle", Service: "fake"})
	h.srv.opts.Events = log
	var got struct{ Events []events.Event }
	h.getJSON(t, "/api/events", &got)
	if len(got.Events) != 2 || got.Events[0].Kind != "stopped_idle" {
		t.Fatalf("events newest first: %+v", got.Events)
	}
}

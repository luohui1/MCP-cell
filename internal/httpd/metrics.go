package httpd

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/events"
	"github.com/luohui1/MCP-cell/internal/metrics"
	"github.com/luohui1/MCP-cell/internal/proc"
)

// The home page's dashboard: every 10 seconds the daemon measures what
// MCPs cost right now (itself, every instance, every mcpcell-run) and what
// they would cost if every conversation ran its own copy, and keeps a day
// of that in memory (internal/metrics). It also follows each instance's
// peak and hands it to the pool when the instance ends, which turns those
// into each service's typical memory.

const (
	sampleEvery = 10 * time.Second
	saveEvery   = 5 * time.Minute
)

type sampler struct {
	ring  *metrics.Ring
	path  string        // where the points are saved
	every time.Duration // between samples

	mu    sync.Mutex
	peaks map[string]*runPeak // by instKey
	now   metricsNow          // the last sample
	saved time.Time
}

type runPeak struct {
	service string
	bytes   uint64
}

func instKey(service string, pid int, started time.Time) string {
	return service + "\x00" + strconv.Itoa(pid) + "\x00" + strconv.FormatInt(started.UnixNano(), 10)
}

func newSampler(home string, every time.Duration) *sampler {
	if every <= 0 {
		every = sampleEvery
	}
	sp := &sampler{ring: metrics.NewRing(int(24 * time.Hour / every)), peaks: map[string]*runPeak{}, every: every}
	if home != "" {
		sp.path = filepath.Join(home, "metrics.bin")
		_ = sp.ring.Load(sp.path, 24*time.Hour)
	}
	return sp
}

// agentCount is how many conversations of one agent use MCPs right now.
type agentCount struct {
	Agent         string `json:"agent"` // claude-code | codex | cursor | the client's own name
	Conversations int    `json:"conversations"`
}

type metricsNow struct {
	At            time.Time `json:"at"`
	TotalBytes    uint64    `json:"totalBytes"` // the three below
	DaemonBytes   uint64    `json:"daemonBytes"`
	InstanceBytes uint64    `json:"instanceBytes"`
	WrapperBytes  uint64    `json:"wrapperBytes"` // mcpcell-run, one per conversation and MCP
	Wrappers      int       `json:"wrappers"`
	// EstimateBytes is what the MCPs in use would cost if every conversation
	// ran its own copy: per service, its conversations times its measured
	// memory. Services not measured yet are left out and listed.
	EstimateBytes   uint64       `json:"estimateBytes"`
	Unmeasured      []string     `json:"unmeasured"`
	Instances       int          `json:"instances"`
	RunningServices int          `json:"runningServices"`
	Services        int          `json:"services"` // configured
	Conversations   []agentCount `json:"conversations"`
}

// agentOf names the agent behind a session from its clientInfo, like the
// page's agentOfClient.
func agentOf(client string) string {
	c := strings.ToLower(client)
	switch {
	case strings.Contains(c, "claude"):
		return "claude-code"
	case strings.Contains(c, "codex"), strings.Contains(c, "openai"):
		return "codex"
	case strings.Contains(c, "cursor"):
		return "cursor"
	}
	if f := strings.Fields(c); len(f) > 0 {
		return f[0]
	}
	return "other"
}

// peerOf finds the mcpcell-run process behind a /run connection and the
// agent process that started it (one per conversation). Zero when unknown.
func peerOf(c net.Conn) (wrapper, agent int) {
	local, ok1 := c.LocalAddr().(*net.TCPAddr)
	remote, ok2 := c.RemoteAddr().(*net.TCPAddr)
	if !ok1 || !ok2 {
		return 0, 0
	}
	wrapper = proc.LoopbackPeer(uint16(remote.Port), uint16(local.Port))
	if wrapper == 0 {
		return 0, 0
	}
	agent = wrapper
	for range 3 { // through a cmd /c the agent may have wrapped it in
		ppid, exe, err := proc.Parent(agent)
		if err != nil || ppid == 0 {
			break
		}
		agent = ppid
		if exe != "cmd.exe" && exe != "conhost.exe" {
			break
		}
	}
	if agent == wrapper {
		agent = 0
	}
	return wrapper, agent
}

// conversations counts each agent's conversations: the distinct agent
// processes behind its sessions, plus, for sessions whose process is not
// known (HTTP clients), as many as the busiest MCP has.
func conversations(list []*session) []agentCount {
	type tally struct {
		procs   map[int]bool
		unknown map[string]int
	}
	by := map[string]*tally{}
	for _, ss := range list {
		a := agentOf(ss.client)
		t := by[a]
		if t == nil {
			t = &tally{procs: map[int]bool{}, unknown: map[string]int{}}
			by[a] = t
		}
		if ss.agent != 0 {
			t.procs[ss.agent] = true
		} else {
			t.unknown[ss.service]++
		}
	}
	out := []agentCount{}
	for a, t := range by {
		n := 0
		for _, c := range t.unknown {
			n = max(n, c)
		}
		out = append(out, agentCount{Agent: a, Conversations: len(t.procs) + n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Conversations != out[j].Conversations {
			return out[i].Conversations > out[j].Conversations
		}
		return out[i].Agent < out[j].Agent
	})
	return out
}

// runSampler samples every few seconds until ctx ends.
func (s *Server) runSampler(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	s.sample(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.sample(now)
		}
	}
}

// sample measures once and records the point.
func (s *Server) sample(now time.Time) metricsNow {
	sp := s.metrics
	running := s.pool.Running()

	s.mu.Lock()
	list := make([]*session, 0, len(s.sessions))
	for _, ss := range s.sessions {
		list = append(list, ss)
	}
	s.mu.Unlock()

	m := metricsNow{At: now, DaemonBytes: proc.Self().PrivateBytes, Unmeasured: []string{}, Instances: len(running)}
	m.Services = len(s.configuredNames())

	// Each wrapper process once, though it may have opened more sessions.
	wrappers := map[int]bool{}
	for _, ss := range list {
		if ss.wrapper != 0 && !wrappers[ss.wrapper] {
			wrappers[ss.wrapper] = true
			m.WrapperBytes += proc.Of(ss.wrapper).PrivateBytes
		}
	}
	m.Wrappers = len(wrappers)

	perService := map[string]uint32{} // KB, under the configured name
	current := map[string]uint64{}    // the largest instance of each service now
	runningBase := map[string]bool{}
	seen := map[string]bool{}
	sp.mu.Lock()
	for _, r := range running {
		m.InstanceBytes += r.Bytes
		base := s.baseName(r.Service)
		perService[base] += uint32(r.Bytes >> 10)
		runningBase[base] = true
		current[r.Service] = max(current[r.Service], r.Bytes)
		k := instKey(r.Service, r.PID, r.StartedAt)
		seen[k] = true
		if pk := sp.peaks[k]; pk != nil {
			pk.bytes = max(pk.bytes, r.Bytes)
		} else {
			sp.peaks[k] = &runPeak{service: r.Service, bytes: r.Bytes}
		}
	}
	var ended []*runPeak
	for k, pk := range sp.peaks {
		if !seen[k] {
			ended = append(ended, pk)
			delete(sp.peaks, k)
		}
	}
	sp.mu.Unlock()
	for _, pk := range ended {
		s.pool.RecordPeak(pk.service, pk.bytes)
	}
	typical := s.pool.Typical()
	m.RunningServices = len(runningBase)
	m.TotalBytes = m.DaemonBytes + m.InstanceBytes + m.WrapperBytes

	// Without MCPcell every conversation starts each of its MCPs itself.
	using := map[string]int{}
	for _, ss := range list {
		using[ss.service]++
	}
	unmeasured := map[string]bool{}
	for name, n := range using {
		each := typical[name]
		if each == 0 {
			each = current[name] // measured now, though the run has not ended
		}
		if each == 0 {
			unmeasured[s.baseName(name)] = true
			continue
		}
		m.EstimateBytes += uint64(n) * each
	}
	for name := range unmeasured {
		m.Unmeasured = append(m.Unmeasured, name)
	}
	sort.Strings(m.Unmeasured)
	m.Conversations = conversations(list)
	total := 0
	for _, c := range m.Conversations {
		total += c.Conversations
	}

	sp.ring.Add(metrics.Point{At: uint32(now.Unix()), Total: kb(m.TotalBytes), Estimate: kb(m.EstimateBytes),
		Running: uint16(min(len(running), 0xffff)), Sessions: uint16(min(total, 0xffff))}, perService)
	sp.mu.Lock()
	sp.now = m
	save := sp.path != "" && now.Sub(sp.saved) >= saveEvery
	if save {
		sp.saved = now
	}
	sp.mu.Unlock()
	if save {
		s.saveMetrics()
	}
	if s.opts.Events != nil {
		if err := s.opts.Events.Save(); err != nil {
			s.opts.Log.Warn("saving events", "err", err)
		}
	}
	return m
}

func kb(b uint64) uint32 { return uint32(min(b>>10, 0xffffffff)) }

// baseName is the configured service a run- variant stands for.
func (s *Server) baseName(name string) string {
	if base, ok := s.variantOf.Load(name); ok {
		return base.(string)
	}
	return name
}

// configuredNames lists the services in the pool that are not run- variants.
func (s *Server) configuredNames() []string {
	var out []string
	for name := range s.pool.Definitions() {
		if _, variant := s.variantOf.Load(name); !variant {
			out = append(out, name)
		}
	}
	return out
}

func (s *Server) saveMetrics() {
	if err := s.metrics.ring.Save(s.metrics.path); err != nil {
		s.opts.Log.Warn("saving memory samples", "err", err)
	}
}

// FlushMetrics hands the peaks of the instances still running to the pool
// and saves the samples; the daemon calls it on the way out.
func (s *Server) FlushMetrics() {
	sp := s.metrics
	sp.mu.Lock()
	peaks := sp.peaks
	sp.peaks = map[string]*runPeak{}
	sp.mu.Unlock()
	for _, pk := range peaks {
		s.pool.RecordPeak(pk.service, pk.bytes)
	}
	if sp.path != "" {
		s.saveMetrics()
	}
}

type metricPoint struct {
	T        int64  `json:"t"` // Unix seconds
	Total    uint64 `json:"total"`
	Estimate uint64 `json:"estimate"`
	Running  int    `json:"running"`
	// Conversations is the most conversations at once in the step.
	Conversations int `json:"conversations"`
}

type metricsResponse struct {
	Range  string        `json:"range"`
	Step   int           `json:"step"` // seconds per point
	Points []metricPoint `json:"points"`
	Now    *metricsNow   `json:"now,omitempty"`
	// Service is set when one service's memory was asked for (?service=).
	Service string `json:"service,omitempty"`
}

// handleMetrics serves the samples of the last hour (every point) or day
// (5-minute averages): GET /api/metrics?range=1h|24h[&service=name]; or
// only the latest sample, for the home page's one line: ?range=now.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	rng := r.URL.Query().Get("range")
	span, step, bucket := time.Hour, int(s.metrics.every/time.Second), uint32(0)
	switch rng {
	case "", "1h":
		rng = "1h"
	case "24h":
		span, step, bucket = 24*time.Hour, 300, 300
	case "now":
		span = 0
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "range 只能是 now、1h 或 24h"})
		return
	}
	from := time.Now().Add(-span)
	out := metricsResponse{Range: rng, Step: step, Points: []metricPoint{}}
	if name := r.URL.Query().Get("service"); name != "" {
		out.Service = name
		at, kbs := s.metrics.ring.ServiceSince(name, from)
		pts := make([]metrics.Point, len(at))
		for i := range at {
			pts[i] = metrics.Point{At: at[i], Total: kbs[i]}
		}
		for _, p := range metrics.Downsample(pts, bucket) {
			out.Points = append(out.Points, metricPoint{T: int64(p.At), Total: uint64(p.Total) << 10})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if span > 0 {
		for _, p := range metrics.Downsample(s.metrics.ring.Since(from), bucket) {
			out.Points = append(out.Points, metricPoint{T: int64(p.At), Total: uint64(p.Total) << 10,
				Estimate: uint64(p.Estimate) << 10, Running: int(p.Running), Conversations: int(p.Sessions)})
		}
	}
	s.metrics.mu.Lock()
	now := s.metrics.now
	s.metrics.mu.Unlock()
	if now.At.IsZero() {
		now = s.sample(time.Now()) // before the first tick
	}
	out.Now = &now
	writeJSON(w, http.StatusOK, out)
}

// handleEvents serves the recent events, newest first.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	list := []events.Event{}
	if s.opts.Events != nil {
		list = s.opts.Events.Recent()
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": list})
}

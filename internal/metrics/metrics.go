// Package metrics keeps the last 24 hours of memory samples for the page's
// chart, in fixed-size rings: one 16-byte point per sample (8640 at one
// every 10 seconds, 138 KB), and per service a ring of 2-byte values in
// 64 KB units that is only allocated once the service has run (17 KB each)
// and dropped once it has not run for a whole day.
package metrics

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// Point is one sample. Memory is in KB.
type Point struct {
	At    uint32 // Unix seconds
	Total uint32 // the daemon and every MCP instance
	// Once an estimate of every session running its own copy; kept blank
	// so the saved layout stays 16 bytes and older files still read.
	_        uint32
	Running  uint16 // instances
	Sessions uint16
}

// Ring holds the last N points, and each service's memory at those points.
type Ring struct {
	mu       sync.Mutex
	pts      []Point
	next     int // where the next point goes
	full     bool
	services map[string][]uint16 // in units of 64 KB, indexed like pts
}

// unit is the per-service resolution: 64 KB, up to 4 GB.
const unit = 64

func NewRing(n int) *Ring {
	return &Ring{pts: make([]Point, n), services: map[string][]uint16{}}
}

// Add records a point and the memory (KB) of the services running at it.
func (r *Ring) Add(p Point, services map[string]uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.next
	r.pts[i] = p
	for name, kb := range services {
		if _, ok := r.services[name]; !ok && kb > 0 {
			r.services[name] = make([]uint16, len(r.pts))
		}
	}
	for name, ring := range r.services {
		ring[i] = uint16(min((services[name]+unit-1)/unit, 0xffff))
	}
	r.next = (i + 1) % len(r.pts)
	if r.next == 0 {
		r.full = true
		// A day went by: let go of services that did not run in it.
		for name, ring := range r.services {
			if allZero(ring) {
				delete(r.services, name)
			}
		}
	}
}

func allZero(v []uint16) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// order returns the ring's indexes oldest first.
func (r *Ring) order() []int {
	n, start := r.next, 0
	if r.full {
		n, start = len(r.pts), r.next
	}
	idx := make([]int, n)
	for k := range idx {
		idx[k] = (start + k) % len(r.pts)
	}
	return idx
}

// Since returns the points from t on, oldest first.
func (r *Ring) Since(t time.Time) []Point {
	r.mu.Lock()
	defer r.mu.Unlock()
	from := uint32(max(t.Unix(), 0))
	var out []Point
	for _, i := range r.order() {
		if r.pts[i].At >= from {
			out = append(out, r.pts[i])
		}
	}
	return out
}

// ServiceSince returns one service's memory (KB) at the points from t on.
func (r *Ring) ServiceSince(name string, t time.Time) (at []uint32, kb []uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ring := r.services[name]
	from := uint32(max(t.Unix(), 0))
	for _, i := range r.order() {
		if r.pts[i].At >= from {
			at = append(at, r.pts[i].At)
			v := uint32(0)
			if ring != nil {
				v = uint32(ring[i]) * unit
			}
			kb = append(kb, v)
		}
	}
	return at, kb
}

// Downsample averages memory and takes the maximum of the counts over
// buckets of step seconds, so a day fits a chart.
func Downsample(pts []Point, step uint32) []Point {
	if step <= 1 || len(pts) == 0 {
		return pts
	}
	var out []Point
	var sum, n uint64
	var cur Point
	flush := func() {
		if n > 0 {
			cur.Total = uint32(sum / n)
			out = append(out, cur)
		}
	}
	for _, p := range pts {
		b := p.At - p.At%step
		if n > 0 && b != cur.At {
			flush()
			sum, n = 0, 0
		}
		if n == 0 {
			cur = Point{At: b}
		}
		sum += uint64(p.Total)
		n++
		cur.Running = max(cur.Running, p.Running)
		cur.Sessions = max(cur.Sessions, p.Sessions)
	}
	flush()
	return out
}

// Save writes the points (not the per-service rings) so a restart keeps
// the chart; Load reads them back, dropping what is older than keep.
func (r *Ring) Save(path string) error {
	pts := r.Since(time.Time{})
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	err = binary.Write(w, binary.LittleEndian, pts)
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (r *Ring) Load(path string, keep time.Duration) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	from := uint32(time.Now().Add(-keep).Unix())
	br := bufio.NewReader(f)
	for {
		var p Point
		if err := binary.Read(br, binary.LittleEndian, &p); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		if p.At >= from {
			r.Add(p, nil)
		}
	}
}

package metrics

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRingKeepsTheLastPointsInOrder(t *testing.T) {
	r := NewRing(4)
	for i := uint32(1); i <= 6; i++ {
		r.Add(Point{At: i, Total: i * 10}, nil)
	}
	got := r.Since(time.Unix(0, 0))
	if len(got) != 4 || got[0].At != 3 || got[3].At != 6 {
		t.Fatalf("points %+v, want 3..6", got)
	}
	if got := r.Since(time.Unix(5, 0)); len(got) != 2 || got[0].At != 5 {
		t.Fatalf("since 5: %+v", got)
	}
}

func TestServiceRings(t *testing.T) {
	r := NewRing(3)
	r.Add(Point{At: 1}, map[string]uint32{"a": 0}) // never ran: nothing kept
	if len(r.services) != 0 {
		t.Fatalf("a service that never ran got a ring")
	}
	r.Add(Point{At: 2}, map[string]uint32{"a": 1000})
	r.Add(Point{At: 3}, nil)
	at, kb := r.ServiceSince("a", time.Unix(0, 0))
	if len(at) != 3 || kb[0] != 0 || kb[1] != 1024 || kb[2] != 0 {
		t.Fatalf("a: at %v kb %v (1000 KB rounds up to 16 units of 64 KB)", at, kb)
	}
	// A whole round without running lets the ring go.
	for i := uint32(4); i <= 6; i++ {
		r.Add(Point{At: i}, nil)
	}
	if _, ok := r.services["a"]; ok {
		t.Fatalf("a's ring kept after a day without running")
	}
	if _, kb := r.ServiceSince("a", time.Unix(0, 0)); len(kb) != 3 || kb[0]+kb[1]+kb[2] != 0 {
		t.Fatalf("a after dropping: %v", kb)
	}
}

func TestDownsample(t *testing.T) {
	var pts []Point
	for i := range uint32(60) { // ten minutes at one every 10 s
		pts = append(pts, Point{At: 6000 + i*10, Total: i, Running: uint16(i % 7), Sessions: uint16(i % 5)})
	}
	got := Downsample(pts, 300)
	if len(got) != 2 {
		t.Fatalf("%d buckets, want 2: %+v", len(got), got)
	}
	// 6000..6290 → values 0..29, average 14; the next 30..59, average 44.
	if got[0].At != 6000 || got[0].Total != 14 || got[1].Total != 44 {
		t.Fatalf("averages %+v", got)
	}
	if got[0].Running != 6 || got[0].Sessions != 4 {
		t.Fatalf("counts keep the maximum: %+v", got[0])
	}
	if len(Downsample(pts, 0)) != 60 {
		t.Fatal("step 0 should keep every point")
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.bin")
	now := uint32(time.Now().Unix())
	r := NewRing(10)
	r.Add(Point{At: now - 7200, Total: 1}, nil) // older than an hour
	r.Add(Point{At: now - 60, Total: 2, Running: 4, Sessions: 5}, map[string]uint32{"a": 64})
	r.Add(Point{At: now, Total: 6}, nil)
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	back := NewRing(10)
	if err := back.Load(path, time.Hour); err != nil {
		t.Fatal(err)
	}
	got := back.Since(time.Unix(0, 0))
	if len(got) != 2 || got[0] != (Point{At: now - 60, Total: 2, Running: 4, Sessions: 5}) || got[1].Total != 6 {
		t.Fatalf("loaded %+v", got)
	}
}

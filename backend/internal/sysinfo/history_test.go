package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)

func withHistory(minutes ...int) *Sampler {
	s := &Sampler{Root: "/"}
	for _, m := range minutes {
		s.record(Snapshot{At: t0.Add(time.Duration(m) * time.Minute), CPU: float64(m), GPU: 50, Mem: 40})
	}
	return s
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "performance-history.json")
	src := withHistory(0, 1, 2, 3)
	if err := src.SaveHistory(path); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("history file must be private: %v %v", fi, err)
	}

	dst := &Sampler{Root: "/"}
	if err := dst.LoadHistory(path, t0.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, got := dst.Last()
	if len(got) != 4 || got[3].CPU != 3 || got[0].Mem != 40 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestLoadDropsOldAndFutureSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	withHistory(0, 10, 20, 40).SaveHistory(path)

	s := &Sampler{Root: "/"}
	// At 10:45 the 30-minute window starts at 10:15: minutes 0 and 10 are
	// too old, 20 is kept, 40 is kept. A sample "from the future" is dropped.
	if err := s.LoadHistory(path, t0.Add(45*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, got := s.Last()
	known := 0
	for _, h := range got {
		if h.CPU >= 0 {
			known++
			if h.At.Before(t0.Add(15 * time.Minute)) {
				t.Errorf("sample from %v should have expired", h.At)
			}
		}
	}
	if known != 2 {
		t.Fatalf("want 2 surviving samples, got %d: %+v", known, got)
	}

	future := &Sampler{Root: "/"}
	future.LoadHistory(path, t0.Add(30*time.Minute)) // minute 40 is in the future
	if _, h := future.Last(); h[len(h)-1].At.After(t0.Add(30 * time.Minute)) {
		t.Error("future sample kept")
	}
}

func TestLoadMissingAndCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	s := withHistory(0, 1)
	if err := s.LoadHistory(filepath.Join(dir, "nope.json"), t0); err != nil {
		t.Errorf("missing file must not be an error: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	if err := s.LoadHistory(bad, t0.Add(2*time.Minute)); err == nil {
		t.Error("corrupt file must be reported")
	}
	if _, h := s.Last(); len(h) != 2 {
		t.Errorf("corrupt file must leave history unchanged, got %d samples", len(h))
	}
}

func TestGapsAreMarkedUnknown(t *testing.T) {
	// Samples at minutes 0 and 4: minutes 1-3 are missing (e.g. restart).
	s := withHistory(0, 4)
	_, h := s.Last()
	if len(h) != 5 {
		t.Fatalf("want 5 entries (2 real + 3 gap), got %d", len(h))
	}
	for i := 1; i <= 3; i++ {
		if h[i].CPU != -1 || h[i].Mem != -1 {
			t.Errorf("minute %d should be unknown, got %+v", i, h[i])
		}
	}
	// A very long gap (machine asleep overnight) is capped to the window.
	s = withHistory(0, 600)
	if _, h := s.Last(); len(h) != historyLen || h[len(h)-1].CPU != 600 {
		t.Fatalf("long gap: %d entries, last %+v", len(h), h[len(h)-1])
	}
}

func TestRestartKeepsGraphContinuous(t *testing.T) {
	// Run for 10 minutes, save, "restart" 2 minutes later, keep sampling.
	path := filepath.Join(t.TempDir(), "h.json")
	before := withHistory(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	before.SaveHistory(path)

	after := &Sampler{Root: "/"}
	after.LoadHistory(path, t0.Add(11*time.Minute))
	after.record(Snapshot{At: t0.Add(11 * time.Minute), CPU: 11, GPU: 50, Mem: 40})
	_, h := after.Last()
	if len(h) != 12 || h[0].CPU != 0 || h[10].CPU != -1 || h[11].CPU != 11 {
		t.Fatalf("history should continue across the restart with a 1-minute gap: %+v", h)
	}
}

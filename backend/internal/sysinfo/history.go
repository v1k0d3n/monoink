package sysinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// savedSample is the on-disk form of one history entry; only what the
// graph needs is kept.
type savedSample struct {
	At  time.Time `json:"at"`
	CPU float64   `json:"cpu"`
	GPU float64   `json:"gpu"`
	Mem float64   `json:"mem"`
}

type savedHistory struct {
	Version int           `json:"version"`
	Samples []savedSample `json:"samples"`
}

// SaveHistory writes the per-minute history to path atomically, readable
// only by the current user.
func (s *Sampler) SaveHistory(path string) error {
	s.mu.Lock()
	out := savedHistory{Version: 1, Samples: make([]savedSample, 0, len(s.history))}
	for _, h := range s.history {
		out.Samples = append(out.Samples, savedSample{At: h.At, CPU: h.CPU, GPU: h.GPU, Mem: h.Mem})
	}
	s.mu.Unlock()

	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadHistory restores history saved by SaveHistory, keeping only samples
// from the last 30 minutes (and none from the future, in case the clock
// changed). A missing file is not an error; an unreadable or corrupt one
// returns an error and leaves the history unchanged.
func (s *Sampler) LoadHistory(path string, now time.Time) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var in savedHistory
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("performance history %s is corrupt: %w", path, err)
	}
	if in.Version != 1 {
		return fmt.Errorf("performance history %s has unknown version %d", path, in.Version)
	}
	sort.Slice(in.Samples, func(i, j int) bool { return in.Samples[i].At.Before(in.Samples[j].At) })

	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	cutoff := now.Add(-historyLen * time.Minute)
	for _, h := range in.Samples {
		if h.At.Before(cutoff) || h.At.After(now) {
			continue
		}
		snap := unknownAt(h.At)
		snap.CPU, snap.GPU, snap.Mem = h.CPU, h.GPU, h.Mem
		s.record(snap)
	}
	return nil
}

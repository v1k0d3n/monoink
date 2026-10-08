// Package sysinfo samples CPU, GPU, memory and temperatures from /proc and
// /sys. It reads nothing else and spawns no processes.
package sysinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Snapshot is one sample. Negative values mean "unavailable".
type Snapshot struct {
	At       time.Time
	CPU      float64 // percent busy since previous sample
	GPU      float64 // percent busy
	Mem      float64 // percent used
	MemUsed  uint64  // bytes
	MemTotal uint64  // bytes
	CPUTemp  float64 // °C
	GPUTemp  float64 // °C
	Uptime   time.Duration
	Battery  float64 // percent, system battery only (not peripherals)
	Charging bool
}

// Sampler keeps the previous CPU counters and a per-minute history.
type Sampler struct {
	Root string // filesystem root, "/" outside tests

	mu       sync.Mutex
	prevIdle uint64
	prevAll  uint64
	last     Snapshot
	history  []Snapshot // one entry per minute, oldest first
}

const historyLen = 30

func New() *Sampler { return &Sampler{Root: "/"} }

func (s *Sampler) path(p ...string) string {
	return filepath.Join(append([]string{s.Root}, p...)...)
}

func (s *Sampler) readInt(p string) (int64, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return v, err == nil
}

// Sample reads current values and records per-minute history.
func (s *Sampler) Sample() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{At: time.Now(), CPU: -1, GPU: -1, Mem: -1, CPUTemp: -1, GPUTemp: -1, Battery: -1}
	s.cpu(&snap)
	s.mem(&snap)
	s.hwmon(&snap)
	s.gpuBusy(&snap)
	s.battery(&snap)
	if b, err := os.ReadFile(s.path("proc", "uptime")); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				snap.Uptime = time.Duration(v) * time.Second
			}
		}
	}
	s.last = snap
	s.record(snap)
	return snap
}

// record appends a per-minute history entry. Minutes with no sample (the
// service was stopped, or the machine slept) are filled with unknown
// values so the graph shows a gap instead of joining distant points.
func (s *Sampler) record(snap Snapshot) {
	n := len(s.history)
	if n > 0 && snap.At.Sub(s.history[n-1].At) < time.Minute {
		return
	}
	if n > 0 {
		missing := int(snap.At.Sub(s.history[n-1].At)/time.Minute) - 1
		for i := 1; i <= min(missing, historyLen); i++ {
			s.history = append(s.history, unknownAt(s.history[n-1].At.Add(time.Duration(i)*time.Minute)))
		}
	}
	s.history = append(s.history, snap)
	if len(s.history) > historyLen {
		s.history = s.history[len(s.history)-historyLen:]
	}
}

func unknownAt(t time.Time) Snapshot {
	return Snapshot{At: t, CPU: -1, GPU: -1, Mem: -1, CPUTemp: -1, GPUTemp: -1, Battery: -1}
}

// Last returns the latest sample and a copy of the history.
func (s *Sampler) Last() (Snapshot, []Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, append([]Snapshot(nil), s.history...)
}

func (s *Sampler) cpu(snap *Snapshot) {
	f, err := os.Open(s.path("proc", "stat"))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return
	}
	var all, idle uint64
	for i, v := range fields[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		all += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	if s.prevAll > 0 && all > s.prevAll {
		snap.CPU = 100 * (1 - float64(idle-s.prevIdle)/float64(all-s.prevAll))
	}
	s.prevAll, s.prevIdle = all, idle
}

func (s *Sampler) mem(snap *Snapshot) {
	f, err := os.Open(s.path("proc", "meminfo"))
	if err != nil {
		return
	}
	defer f.Close()
	var total, avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = v * 1024
		case "MemAvailable:":
			avail = v * 1024
		}
	}
	if total > 0 {
		snap.MemTotal, snap.MemUsed = total, total-avail
		snap.Mem = 100 * float64(total-avail) / float64(total)
	}
}

func (s *Sampler) hwmon(snap *Snapshot) {
	dirs, _ := filepath.Glob(s.path("sys", "class", "hwmon", "hwmon*"))
	for _, d := range dirs {
		name, _ := os.ReadFile(filepath.Join(d, "name"))
		v, ok := s.readInt(filepath.Join(d, "temp1_input"))
		if !ok {
			continue
		}
		switch strings.TrimSpace(string(name)) {
		case "k10temp", "coretemp", "zenpower":
			if snap.CPUTemp < 0 {
				snap.CPUTemp = float64(v) / 1000
			}
		case "amdgpu":
			if snap.GPUTemp < 0 {
				snap.GPUTemp = float64(v) / 1000
			}
		}
	}
}

func (s *Sampler) gpuBusy(snap *Snapshot) {
	matches, _ := filepath.Glob(s.path("sys", "class", "drm", "card*", "device", "gpu_busy_percent"))
	for _, m := range matches {
		if v, ok := s.readInt(m); ok {
			snap.GPU = float64(v)
			return
		}
	}
}

func (s *Sampler) battery(snap *Snapshot) {
	dirs, _ := filepath.Glob(s.path("sys", "class", "power_supply", "*"))
	for _, d := range dirs {
		typ, _ := os.ReadFile(filepath.Join(d, "type"))
		scope, _ := os.ReadFile(filepath.Join(d, "scope"))
		// Skip mice, controllers and other peripherals (scope "Device").
		if strings.TrimSpace(string(typ)) != "Battery" || strings.TrimSpace(string(scope)) == "Device" {
			continue
		}
		if v, ok := s.readInt(filepath.Join(d, "capacity")); ok {
			snap.Battery = float64(v)
			status, _ := os.ReadFile(filepath.Join(d, "status"))
			snap.Charging = strings.TrimSpace(string(status)) == "Charging"
			return
		}
	}
}

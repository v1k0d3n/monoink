package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(content), 0o644)
}

func TestSample(t *testing.T) {
	root := t.TempDir()
	put(t, root, "proc/stat", "cpu  100 0 100 800 0 0 0 0 0 0\n")
	put(t, root, "proc/meminfo", "MemTotal: 1000 kB\nMemAvailable: 250 kB\n")
	put(t, root, "proc/uptime", "3600.5 100\n")
	put(t, root, "sys/class/hwmon/hwmon0/name", "k10temp\n")
	put(t, root, "sys/class/hwmon/hwmon0/temp1_input", "55000\n")
	put(t, root, "sys/class/hwmon/hwmon1/name", "amdgpu\n")
	put(t, root, "sys/class/hwmon/hwmon1/temp1_input", "61000\n")
	put(t, root, "sys/class/drm/card0/device/gpu_busy_percent", "42\n")
	put(t, root, "sys/class/power_supply/mouse/type", "Battery\n")
	put(t, root, "sys/class/power_supply/mouse/scope", "Device\n")
	put(t, root, "sys/class/power_supply/mouse/capacity", "10\n")

	s := &Sampler{Root: root}
	first := s.Sample()
	if first.CPU != -1 {
		t.Errorf("first CPU sample should be unknown, got %v", first.CPU)
	}
	put(t, root, "proc/stat", "cpu  150 0 150 900 0 0 0 0 0 0\n") // 100 busy of 200
	snap := s.Sample()
	if snap.CPU != 50 || snap.Mem != 75 || snap.CPUTemp != 55 || snap.GPUTemp != 61 || snap.GPU != 42 {
		t.Errorf("unexpected snapshot: %+v", snap)
	}
	if snap.Battery != -1 {
		t.Error("peripheral battery must be ignored")
	}
	if snap.Uptime.Hours() != 1 {
		t.Errorf("uptime %v", snap.Uptime)
	}
}

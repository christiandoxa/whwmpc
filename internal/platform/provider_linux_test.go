//go:build linux

package platform

import (
	"context"
	"math"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func TestLinuxOverviewUsesSamplesAndReportsMissingData(t *testing.T) {
	files := fstest.MapFS{
		"etc/os-release":            {Data: []byte("ID=ubuntu\nPRETTY_NAME=\"Fixture Ubuntu\"")},
		"proc/sys/kernel/osrelease": {Data: []byte("6.8-microsoft-standard-WSL2\n")},
		"proc/stat":                 {Data: []byte("cpu 100 0 0 200 0 0 0 0 17 0")},
		"proc/meminfo":              {Data: []byte("MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 512 kB\nSwapFree: 256 kB")},
		"proc/self/mountinfo":       {Data: []byte("12 1 8:1 / / rw - ext4 /dev/test rw\n13 1 0:2 / /remote rw - nfs server:/remote rw")},
	}
	t0 := time.Date(2026, 11, 22, 1, 0, 0, 0, time.UTC)
	samples := []string{"cpu 110 0 0 220 0 0 0 0", "cpu 120 0 0 240 0 0 0 0", "cpu 130 0 0 260 0 0 0 0"}
	p := linuxProvider{
		fs:  files,
		now: func() time.Time { return t0 },
		wait: func(_ context.Context, d time.Duration) error {
			files["proc/stat"].Data = []byte(samples[0])
			samples = samples[1:]
			t0 = t0.Add(d)
			return nil
		},
		statfs: func(path string, s *syscall.Statfs_t) error {
			if path != "/" {
				t.Fatalf("nonlocal statfs path: %q", path)
			}
			s.Blocks, s.Bfree, s.Bavail, s.Bsize = 100, 50, 40, 4096
			return nil
		},
	}
	got := p.collect(context.Background(), 4*time.Second)
	cpu, hasCPU := got.CPU.Latest()
	if got.Distro != "Fixture Ubuntu" || got.Environment != "wsl" || !hasCPU || math.Abs(cpu-100.0/3) > 0.0001 ||
		len(got.CPU.Samples) != 3 || got.CPU.CapturedAt.Sub(got.CPU.StartedAt) != 12*time.Second || len(got.Memory.Use.Samples) != 3 || len(got.Disks) != 1 ||
		got.Disks[0].FreeBytes != 40*4096 || got.Memory.SwapStatus != domain.Available || got.Memory.SwapUsed != 256*1024 {
		t.Fatalf("overview = %+v", got)
	}
	if got.Temperature.Availability != domain.Unknown || got.PowerStatus != domain.Unknown || got.Systemd {
		t.Fatalf("incorrectly claimed unavailable capability: %+v", got)
	}
}

func TestProcessMatchingAvoidsReusedPID(t *testing.T) {
	before := map[string]processStat{"15": {start: 2, ticks: 12}, "16": {start: 4, ticks: 24}}
	after := map[string]processStat{"15": {start: 3, ticks: 40, name: "reused"}, "16": {start: 4, ticks: 34, name: "stable"}}
	got := topProcesses(before, after, 50)
	if len(got) != 1 || got[0].Name != "stable" || got[0].CPU != 20 {
		t.Fatalf("matched processes = %+v", got)
	}
}

func TestMemorySeparatesUnreadableSwapFromNoSwap(t *testing.T) {
	p := linuxProvider{fs: fstest.MapFS{"proc/meminfo": {Data: []byte("MemTotal: 1000 kB\nMemAvailable: 250 kB")}}}
	if m := p.memory(); m.Availability != domain.Available || m.SwapStatus != domain.Unknown {
		t.Fatalf("incomplete sample = %+v", m)
	}
	p.fs = fstest.MapFS{"proc/meminfo": {Data: []byte("MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB")}}
	if m := p.memory(); m.SwapStatus != domain.Available || m.SwapBytes != 0 {
		t.Fatalf("no swap sample = %+v", m)
	}
}

func TestStressTemperatureKeepsInitialCPUInputs(t *testing.T) {
	const hwmon = "sys/class/hwmon/hwmon0/"
	const fallback = "sys/class/thermal/thermal_zone0/"
	files := fstest.MapFS{
		hwmon + "name":                       {Data: []byte("coretemp\n")},
		hwmon + "temp1_input":                {Data: []byte("65000\n")},
		hwmon + "temp2_input":                {Data: []byte("55000\n")},
		"sys/class/hwmon/hwmon1/name":        {Data: []byte("amdgpu\n")},
		"sys/class/hwmon/hwmon1/temp1_input": {Data: []byte("88000\n")},
		fallback + "type":                    {Data: []byte("x86_pkg_temp\n")},
		fallback + "temp":                    {Data: []byte("58000\n")},
	}
	p := linuxProvider{fs: files}
	probe := p.temperatureProbe()
	if got := probe(t.Context()); got == nil || *got != 65 {
		t.Fatalf("expected hottest CPU input: %v", got)
	}
	files[hwmon+"name"].Data = []byte("amdgpu\n")
	if got := probe(t.Context()); got != nil {
		t.Fatalf("accepted replaced CPU sensor: %v", *got)
	}
	files[hwmon+"name"].Data = []byte("coretemp\n")
	delete(files, hwmon+"temp1_input")
	if got := probe(t.Context()); got != nil {
		t.Fatalf("lost sensor was hidden by a lower reading: %v", *got)
	}
	delete(files, hwmon+"name")
	if got := p.temperatureProbe()(t.Context()); got == nil || *got != 58 {
		t.Fatalf("missing supported thermal zone: %v", got)
	}
}

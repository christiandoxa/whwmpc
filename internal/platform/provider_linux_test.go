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
	got := p.collect(context.Background())
	cpu, hasCPU := got.CPU.Latest()
	if got.Distro != "Fixture Ubuntu" || got.Environment != "wsl" || !hasCPU || math.Abs(cpu-100.0/3) > 0.0001 ||
		len(got.CPU.Samples) != 3 || len(got.Memory.Use.Samples) != 3 || len(got.Disks) != 1 ||
		got.Disks[0].FreeBytes != 40*4096 || got.Memory.SwapUsed != 256*1024 {
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

//go:build darwin || windows

package platform

import (
	"os"
	"runtime"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func TestNativeOverview(t *testing.T) {
	if os.Getenv("WHWMPC_TEST_NATIVE") != "1" {
		t.Skip("native CI explicitly requests live read-only telemetry")
	}
	o := (Native{}).Overview(t.Context())
	if o.OS != runtime.GOOS || o.Distro == "" || o.Kernel == "" || o.UptimeSeconds == nil || *o.UptimeSeconds <= 0 {
		t.Fatalf("OS identity or uptime unavailable (%s uptime=%t)", o.OS, o.UptimeSeconds != nil)
	}
	cpu, valid := o.CPU.Latest()
	if !valid || !goodPercent(cpu) || len(o.CPU.Samples) < 2 {
		t.Errorf("native CPU must include multiple valid samples: %v %+v", cpu, o.CPU)
	}
	if o.Memory.TotalBytes == 0 || o.Memory.FreeBytes > o.Memory.TotalBytes || o.Memory.Availability == domain.Unknown {
		t.Errorf("native memory unavailable: %+v", o.Memory)
	}
	root := false
	for _, d := range o.Disks {
		root = root || d.Root && d.Availability == domain.Available && d.TotalBytes > 0 && d.FreeBytes <= d.TotalBytes
	}
	if !root {
		t.Errorf("no measurable startup volume among %d disk entries (status %s)", len(o.Disks), o.DiskStatus)
	}
	if len(o.Processes) == 0 {
		t.Errorf("native process sample missing (status %s)", o.ProcessStatus)
	}
	t.Logf("%s/%s native CPU samples=%d disks=%d processes=%d power=%s battery=%t",
		o.OS, o.Arch, len(o.CPU.Samples), len(o.Disks), len(o.Processes), o.PowerStatus, o.Battery != nil)
}

//go:build darwin || windows

package platform

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func TestNativeObservationWindow(t *testing.T) {
	if os.Getenv("WHWMPC_TEST_NATIVE") != "1" {
		t.Skip("native CI explicitly requests live read-only telemetry")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	start := time.Now()
	o := (Native{}).Observe(ctx, 4*time.Second)
	elapsed := time.Since(start)
	if ctx.Err() != nil || elapsed < 11500*time.Millisecond || len(o.CPU.Samples) < 2 || len(o.Memory.Use.Samples) < 2 {
		t.Fatalf("native observation elapsed=%s CPU=%d RAM=%d cancellation=%v", elapsed, len(o.CPU.Samples), len(o.Memory.Use.Samples), ctx.Err())
	}
	t.Logf("%s/%s read-only observation elapsed=%s CPU=%d RAM=%d processes=%d power=%s",
		o.OS, o.Arch, elapsed.Round(time.Millisecond), len(o.CPU.Samples), len(o.Memory.Use.Samples), len(o.Processes), o.PowerStatus)
}

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

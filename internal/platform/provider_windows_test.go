package platform

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type cancelOnSampleWait struct {
	context.Context
	cancel context.CancelFunc
}

func (c cancelOnSampleWait) Done() <-chan struct{} {
	// Process enumeration checks Err. The sampling wait is the first caller of Done.
	c.cancel()
	return c.Context.Done()
}

func TestWindowsOverviewDoesNotReuseProcessSnapshotOnCancel(t *testing.T) {
	if _, ok := windowsProcessSnapshot(t.Context())[uint32(os.Getpid())]; !ok {
		t.Fatal("the native process snapshot could not read the test process")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	overview := Native{}.Overview(cancelOnSampleWait{Context: ctx, cancel: cancel})
	if ctx.Err() != context.Canceled {
		t.Fatal("overview did not reach the sampling wait")
	}
	if len(overview.Processes) != 0 || overview.ProcessStatus != domain.Unknown {
		t.Fatalf("published processes without a second snapshot: %+v (%s)", overview.Processes, overview.ProcessStatus)
	}
}

func TestWindowsTelemetryUnits(t *testing.T) {
	if pct, ok := windowsBusy(tickCounts{0, 100, 0}, tickCounts{70, 180, 20}); !ok || pct != 30 {
		t.Fatalf("kernel idle time counted incorrectly %v %t", pct, ok)
	}
	if _, ok := windowsBusy(tickCounts{10, 100, 0}, tickCounts{9, 180, 20}); ok {
		t.Fatal("accepted counters reset")
	}
	got := windowsPower(powerStatus{255, 255, 255, 0, 0, 0})
	if got.OnAC != nil || got.Percent != nil || got.Availability != domain.Unknown || got.BatteryStatus != "" {
		t.Fatal("unknown battery status invented")
	}
	got = windowsPower(powerStatus{1, 128, 100, 0, 0, 0})
	if got.OnAC == nil || !*got.OnAC || got.Percent != nil || got.Availability != domain.Available || got.BatteryStatus != "" {
		t.Fatal("desktop presented as battery-powered")
	}
	got = windowsPower(powerStatus{1, 8, 255, 0, 0, 0})
	if got.BatteryStatus != "Charging" || got.Percent != nil || got.Availability != domain.Available {
		t.Fatalf("indeterminate battery percent hid charging: %+v", got)
	}
	got = windowsPower(powerStatus{1, 255, 53, 0, 0, 0})
	if got.Percent == nil || *got.Percent != 53 || got.BatteryStatus != "" {
		t.Fatalf("unknown flags masked independent percentage or invented charging: %+v", got)
	}
	got = windowsPower(powerStatus{255, 0, 53, 0, 0, 0})
	if got.OnAC != nil || got.Availability != domain.Partial || got.Percent == nil || got.BatteryStatus != "Not charging" {
		t.Fatalf("known battery flags obscured by an unknown source: %+v", got)
	}
	pr := windowsProcesses(map[uint32]windowsProc{1: {10, 0, 0, "one"}, 2: {10, 0, 0, "old"}},
		map[uint32]windowsProc{1: {10, 5_000_000, 1024, "one"}, 2: {20, 5_000_000, 0, "reused"}}, time.Second, 2)
	if len(pr) != 1 || pr[0].CPU != 25 || pr[0].RSS != 1024 {
		t.Fatalf("windows process units or PID reuse incorrect: %+v", pr)
	}
}

func TestWindowsProcessorGroupTelemetry(t *testing.T) {
	if sampleAvailability(0, true) != domain.Unknown ||
		sampleAvailability(3, true) != domain.Partial ||
		sampleAvailability(3, false) != domain.Available {
		t.Fatal("sample completeness must include processor scope")
	}
	before := map[uint32]windowsProc{1: {created: 10, name: "one"}}
	after := map[uint32]windowsProc{1: {created: 10, name: "one", total: 640_000_000}}
	processes := windowsProcesses(before, after, time.Second, 128)
	if len(processes) != 1 || processes[0].CPU != 50 {
		t.Fatalf("process share must use the capacity of all 128 processors: %+v", processes)
	}
}

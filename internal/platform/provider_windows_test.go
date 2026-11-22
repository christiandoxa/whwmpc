package platform

import (
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

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

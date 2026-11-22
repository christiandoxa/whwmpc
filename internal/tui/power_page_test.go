package tui

import (
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
)

func TestNativePowerPoseRequiresReportedCharging(t *testing.T) {
	snapshot := power.Snapshot{OS: "windows", CPUFreqStatus: domain.Unsupported, PowerStatus: domain.Available,
		PowerSource: "ac", Batteries: []power.Battery{{Status: "Charging", Percent: new(uint64(78))}}}
	if got := powerPage(snapshot, "en"); got.Availability != domain.Available || got.Pose != mascot.Charging ||
		len(got.Found) == 0 || !strings.Contains(got.Found[0], "Power status") ||
		len(got.Evidence) != 1 || !strings.Contains(got.Evidence[0], "Battery · Charging · 78%") {
		t.Fatalf("lost native charging data: %+v", got)
	}
	snapshot.Batteries[0].Status = "Not charging"
	if got := powerPage(snapshot, "en"); got.Pose != mascot.Battery {
		t.Fatalf("charger implied charging: %+v", got)
	}
}

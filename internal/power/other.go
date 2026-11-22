//go:build !linux

package power

import (
	"context"
	"io/fs"
	"runtime"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func Scan(ctx context.Context, fsys fs.FS, runner sysrun.Runner) Snapshot {
	reading := platform.ReadPower(ctx, runner)
	snapshot := Snapshot{OS: runtime.GOOS, At: time.Now().UTC(), CPUFreqStatus: domain.Unsupported,
		PowerStatus: reading.Availability, PowerSource: "unknown", Profiles: InspectProfiles(ctx, fsys, runner)}
	if reading.OnAC != nil {
		if *reading.OnAC {
			snapshot.PowerSource = "ac"
		} else {
			snapshot.PowerSource = "battery"
		}
	}
	if reading.Percent != nil || reading.BatteryStatus != "" {
		battery := Battery{Status: reading.BatteryStatus}
		if reading.Percent != nil {
			battery.Percent = new(uint64(*reading.Percent))
		}
		snapshot.Batteries = []Battery{battery}
	}
	return snapshot
}

func InspectProfiles(context.Context, fs.FS, sysrun.Runner) Profiles {
	return Profiles{Status: domain.Unsupported, ManagerStatus: domain.Unsupported, Detail: "The Linux power-profile backend is unavailable on this OS."}
}

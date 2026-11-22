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
	ac, charge, status := platform.ReadPower(ctx, runner)
	snapshot := Snapshot{OS: runtime.GOOS, At: time.Now().UTC(), CPUFreqStatus: domain.Unsupported,
		PowerStatus: status, PowerSource: "unknown", Profiles: InspectProfiles(ctx, fsys, runner)}
	if ac != nil {
		if *ac {
			snapshot.PowerSource = "ac"
		} else {
			snapshot.PowerSource = "battery"
		}
	}
	if charge != nil {
		percent := uint64(*charge)
		snapshot.Batteries = []Battery{{Percent: &percent}}
	}
	return snapshot
}

func InspectProfiles(context.Context, fs.FS, sysrun.Runner) Profiles {
	return Profiles{Status: domain.Unsupported, ManagerStatus: domain.Unsupported, Detail: "The Linux power-profile backend is unavailable on this OS."}
}

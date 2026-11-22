//go:build !linux

package power

import (
	"context"
	"io/fs"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func Scan(ctx context.Context, fsys fs.FS, runner sysrun.Runner) Snapshot {
	return Snapshot{
		At: time.Now().UTC(), CPUFreqStatus: domain.Unsupported,
		PowerStatus: domain.Unsupported, PowerSource: "unknown", Profiles: InspectProfiles(ctx, fsys, runner),
	}
}

func InspectProfiles(context.Context, fs.FS, sysrun.Runner) Profiles {
	return Profiles{Status: domain.Unsupported, ManagerStatus: domain.Unsupported, Detail: "The Linux power-profile backend is unavailable on this OS."}
}

//go:build !linux && !darwin && !windows

package filesystem

import (
	"context"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func (Volume) CheckCandidate() bool { return false }

func discover(context.Context, sysrun.Runner) Snapshot {
	return Snapshot{Availability: domain.Unsupported}
}

func Check(_ context.Context, _ sysrun.Runner, target string) CheckResult {
	return CheckResult{Availability: domain.Unsupported, Device: target, State: "not_started", ExitCode: -1}
}

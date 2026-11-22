//go:build !linux && !darwin && !windows

package platform

import (
	"context"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type Native struct{}

func (Native) Overview(context.Context) domain.Overview {
	o := baseOverview(time.Now(), domain.Unsupported)
	o.Availability = domain.Partial
	return o
}

func ReadPower(context.Context, sysrun.Runner) PowerReading {
	return PowerReading{Availability: domain.Unsupported}
}

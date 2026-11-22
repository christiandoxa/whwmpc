//go:build !linux

package platform

import (
	"context"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Native struct{}

func (Native) Overview(context.Context) domain.Overview {
	o := baseOverview(time.Now(), domain.Unsupported)
	o.Availability = domain.Partial
	return o
}

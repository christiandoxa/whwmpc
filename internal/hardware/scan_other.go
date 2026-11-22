//go:build !linux

package hardware

import (
	"context"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func Scan(context.Context, sysrun.Runner) Snapshot {
	return Snapshot{At: time.Now().UTC(), Availability: domain.Unsupported,
		Detail: "Backend GPU ini tersedia pada Linux dengan driver yang sesuai."}
}

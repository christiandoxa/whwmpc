//go:build !linux

package storage

import (
	"context"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func (c Collector) InspectSMART(context.Context) SMARTAudit {
	return SMARTAudit{Availability: domain.Unsupported, CapturedAt: c.now(), Detail: "Backend SMART ini tersedia di Linux."}
}

func (c Collector) InspectTrim(context.Context) TrimAudit {
	return TrimAudit{
		Availability: domain.Unsupported, CapturedAt: c.now(), Manual: domain.Unsupported,
		Timer:  Timer{Availability: domain.Unsupported, Journal: domain.Unsupported},
		Detail: "Backend FITRIM ini tersedia di Linux.",
	}
}

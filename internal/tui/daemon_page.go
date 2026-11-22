package tui

import (
	"context"
	"errors"
	"io/fs"

	"github.com/christiandoxa/whwmpc/internal/daemon"
	"github.com/christiandoxa/whwmpc/internal/domain"
)

func daemonPage(ctx context.Context, demo bool, lang string) Page {
	l := translator(lang)
	p := Page{Availability: domain.Available, Found: []string{l("Observasi terjadwal", "Scheduled observations")}}
	p.Advice = []string{l("Buka whwmpc daemon --help untuk mengatur jadwal atau menghapus unit.",
		"Open whwmpc daemon --help to configure the schedule or remove the unit.")}
	if demo {
		p.Found = append(p.Found, l("DEMO · status host tidak dibaca", "DEMO · host status is not read"))
		return p
	}
	manager, err := daemon.Default()
	if err != nil {
		p.Availability = domain.Unsupported
		p.Found = append(p.Found, domain.SafeText(err.Error()))
		return p
	}
	status, err := manager.Manage(ctx, "status", 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		p.Found = append(p.Found, l("Timer belum terpasang", "Timer is not installed"))
	case err != nil:
		p.Availability = domain.Unknown
		p.Found = append(p.Found, l("Status timer belum tersedia", "Timer status unavailable"))
		p.Evidence = append(p.Evidence, domain.SafeText(err.Error()))
	default:
		p.Found = append(p.Found, l("Unit ditemukan · buka detail status", "Unit found · view status details"))
		p.Evidence = append(p.Evidence, status)
	}
	return p
}

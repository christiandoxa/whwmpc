package tui

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/christiandoxa/whwmpc/internal/daemon"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/state"
)

func daemonPage(ctx context.Context, store state.Store, now func() time.Time, demo bool, lang string) Page {
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
	status, err := manager.Status(ctx)
	s := &ResultSummary{Headline: p.Found[0], At: now()}
	p.Summary = s
	timer := resultStatus{Label: "Timer", Availability: domain.Available}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		timer.Value = l("Unit belum terpasang lengkap", "Units not fully installed")
	case err != nil:
		p.Availability = domain.Unknown
		if errors.Is(err, fs.ErrPermission) {
			p.Availability = domain.PermissionRequired
		}
		timer.Availability = p.Availability
		timer.Value = err.Error()
		p.Evidence = append(p.Evidence, domain.SafeText(err.Error()))
	default:
		timer.Value = status.ActiveState
		if status.UnitFileState != "" {
			s.Statuses = append(s.Statuses, resultStatus{Label: l("File unit", "Unit file"), Value: status.UnitFileState, Availability: domain.Available})
		}
		if timer.Value == "" {
			timer.Availability = domain.Unknown
		}
		p.Evidence = append(p.Evidence, status.Raw)
	}
	entries, err := store.History()
	s.Statuses = append([]resultStatus{timer, daemonSample(entries, s.At, err, lang)}, s.Statuses...)
	return p
}

func daemonSample(entries []state.Entry, at time.Time, err error, lang string) resultStatus {
	l := translator(lang)
	row := resultStatus{Label: l("Sampel terakhir", "Last sample"), Availability: domain.Unknown}
	if err != nil {
		row.Availability, row.Value = domain.Error, err.Error()
		return row
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.Category == "daemon" && entry.Kind == "scan" && entry.At.After(latest) {
			latest = entry.At
		}
	}
	if latest.IsZero() {
		return row
	}
	if latest.After(at) {
		row.Availability, row.Value = domain.Error, l("Waktu sampel di masa depan", "Sample timestamp is in the future")
		return row
	}
	row.Availability = domain.Available
	row.Value = latest.Format("02 Jan 15:04 MST") + " · " + l("usia ", "age ") + at.Sub(latest).Round(time.Second).String()
	return row
}

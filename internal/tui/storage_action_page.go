package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func firmwarePage(ctx context.Context, m *workflow.Maintenance, req Request) Page {
	l := translator(req.Language)
	if m.Demo {
		return Page{Availability: domain.Unsupported, Found: []string{l("DEMO · akses firmware terkunci", "DEMO · firmware access locked")}}
	}
	if req.Choice.Kind == "firmware-verify" || req.Choice.Kind == "firmware-resume" {
		target := req.Choice.Value
		if req.Choice.Kind == "firmware-resume" {
			var err error
			target, err = m.Store.FirmwareHandoff(req.Choice.Value)
			if err != nil {
				return unavailableAction(err, req.Language)
			}
		}
		v := firmware.Outcome(ctx, sysrun.Native{}, target)
		p := Page{Availability: v.Availability, Found: []string{l("Verifikasi firmware", "Firmware verification"), v.Detail()}}
		p.Snapshot = maintenanceSnapshot(v.Availability, "firmware", "fwupd verification", v.Detail())
		return p
	}
	updates := req.Choice.Kind == "firmware-cache"
	snap := firmware.Collect(ctx, sysrun.Native{}, updates)
	page := Page{Availability: snap.Availability, Found: []string{l("Firmware terpasang", "Installed firmware"),
		fmt.Sprintf("%d · %s", len(snap.Devices), statusNames[snap.Availability].Text(req.Language))}}
	if updates {
		page.Found = append(page.Found, l("Katalog lokal", "Cached catalog")+" · "+statusNames[snap.Updates].Text(req.Language))
	}
	page.Evidence = append(page.Evidence, snap.Detail)
	var candidates []Choice
	for _, d := range snap.Devices {
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s · %s", d.Name, d.Vendor, d.Version))
		for _, r := range d.Releases {
			target := d.ID + "@" + r.Version
			if _, _, err := firmware.Select(snap, target); err != nil {
				continue
			}
			candidates = append(candidates, Choice{l("Tinjau", "Review") + " " + domain.SafeText(d.Name) + " " + r.Version, actions.FirmwareUpdate, target})
		}
	}
	if m.Available() == nil {
		page.Choices = append(page.Choices, candidates...)
		page.Choices = append(page.Choices, Choice{l("Tinjau refresh LVFS lewat jaringan", "Review online LVFS refresh"), actions.FirmwareRefresh, firmware.LVFS})
	}
	page.Snapshot = maintenanceSnapshot(snap.Availability, "firmware", "fwupd inventory", page.Found[1])
	if !updates {
		page.Choices = append([]Choice{{l("Cari kandidat di katalog lokal", "Check cached updates"), "firmware-cache", ""}}, page.Choices...)
	}
	return page
}

func filesystemPage(ctx context.Context, m *workflow.Maintenance, req Request) Page {
	l := translator(req.Language)
	if m.Demo {
		return Page{Availability: domain.Unsupported, Found: []string{l("DEMO · akses filesystem terkunci", "DEMO · filesystem access locked")}}
	}
	if req.Choice.Kind == "filesystem-check" {
		r := filesystem.Check(ctx, sysrun.Native{}, req.Choice.Value)
		page := Page{Availability: r.Availability, Found: []string{l("Hasil pemeriksaan filesystem", "Filesystem check result"), r.Device + " · " + r.State, r.Detail}}
		page.Snapshot = maintenanceSnapshot(r.Availability, "filesystem", "Filesystem checker", fmt.Sprintf("%s · exit %d", r.State, r.ExitCode))
		return page
	}
	snap := filesystem.Discover(ctx, sysrun.Native{})
	page := Page{Availability: snap.Availability, Found: []string{l("Volume yang terlihat", "Detected volumes"), statusNames[snap.Availability].Text(req.Language)}}
	page.Advice = []string{l("Perbaikan ext memerlukan volume offline dan cadangan. Gunakan lingkungan pemulihan bila volume sedang terpasang.",
		"Ext repair requires an offline volume and backup. Use a recovery environment when the volume is mounted.")}
	page.Evidence = append(page.Evidence, snap.Detail)
	page.Snapshot = maintenanceSnapshot(snap.Availability, "filesystem", "Filesystem inventory", fmt.Sprintf("%d volumes detected", len(snap.Volumes)))
	for _, v := range snap.Volumes {
		target := domain.SafeText(v.Device)
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s · mounted=%t", target, v.Filesystem, v.Mounted()))
		if v.OfflineCandidate() {
			page.Choices = append(page.Choices, Choice{l("Periksa baca-saja", "Check read-only") + " " + target, "filesystem-check", v.Device})
			if !v.ReadOnly && m.Available() == nil {
				page.Choices = append(page.Choices, Choice{l("Tinjau perbaikan", "Review repair") + " " + target, actions.FilesystemRepair, v.Device})
			}
		}
	}
	return page
}

func maintenanceSnapshot(status domain.Availability, category, source, observation string) *workflow.Snapshot {
	at := time.Now().UTC()
	return &workflow.Snapshot{Input: report.Input{At: at, Findings: []diagnostics.Finding{{
		ID: category + ".observation", Category: category, Availability: status, Severity: diagnostics.Info,
		Observation: observation, Source: source, At: at,
	}}}}
}

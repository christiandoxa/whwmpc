package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func firmwarePage(ctx context.Context, m *workflow.Maintenance, req Request) Page {
	l := translator(req.Language)
	if m.Demo {
		return Page{Availability: domain.Unsupported, Found: []string{l("DEMO · akses firmware terkunci", "DEMO · firmware access locked")}}
	}
	if req.Choice.Kind == "firmware-resume" {
		outcome, err := m.ResumeFirmware(ctx, req.Choice.Value)
		if errors.Is(err, context.Canceled) {
			return Page{Availability: domain.Unknown, Pose: mascot.Cancelled,
				Found: []string{l("Verifikasi firmware dibatalkan.", "Firmware verification cancelled.")}}
		}
		if err != nil {
			return unavailableAction(err, req.Language)
		}
		page := firmwareVerificationPage(outcome.Verification, req.Language)
		if outcome.HistoryErr != nil {
			page.Advice = append(page.Advice, l("Riwayat gagal disimpan", "Could not save history")+" · "+domain.SafeText(outcome.HistoryErr.Error()))
		} else {
			page.refreshHistory = outcome.Verification.State == "verified" || outcome.Verification.State == "failed"
		}
		page.Choices = []Choice{{l("Lihat riwayat", "View history"), "navigate", string(assistant.History)}}
		return page
	}
	if req.Choice.Kind == "firmware-verify" {
		return firmwareVerificationPage(firmware.Outcome(ctx, sysrun.Native{}, req.Choice.Value), req.Language)
	}
	updates := req.Choice.Kind == "firmware-cache"
	return firmwareInventoryPage(firmware.Collect(ctx, sysrun.Native{}, updates), req.Language, updates, m.Available() == nil)
}

func firmwareInventoryPage(snap firmware.Snapshot, lang string, updates, canPlan bool) Page {
	l := translator(lang)
	status := statusNames[snap.Availability].Text(lang)
	if snap.Availability == domain.Available {
		status = fmt.Sprintf("%d · %s", len(snap.Devices), status)
	}
	page := Page{Availability: snap.Availability, Found: []string{l("Firmware terpasang", "Installed firmware"), status}}
	page.Snapshot = maintenanceSnapshot(snap.Availability, "firmware", "fwupd inventory", status)
	if snap.Availability == domain.DependencyMissing {
		page.Advice = []string{l("Pemeriksaan firmware memerlukan fwupdmgr.", "Firmware checks require fwupdmgr.")}
		return page
	}
	if snap.Availability == domain.Unsupported {
		page.Advice = []string{l("Pemeriksaan dengan fwupd tersedia di Linux.", "Firmware checks with fwupd are available on Linux.")}
		return page
	}
	if updates && snap.Updates != "" {
		page.Found = append(page.Found, l("Katalog lokal", "Cached catalog")+" · "+statusNames[snap.Updates].Text(lang))
	}
	if snap.Detail != "" {
		page.Evidence = append(page.Evidence, snap.Detail)
	}
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
	if canPlan {
		page.Choices = append(page.Choices, candidates...)
		page.Choices = append(page.Choices, Choice{l("Tinjau refresh LVFS lewat jaringan", "Review online LVFS refresh"), actions.FirmwareRefresh, firmware.LVFS})
	}
	if !updates {
		page.Choices = append([]Choice{{l("Cari kandidat di katalog lokal", "Check cached updates"), "firmware-cache", ""}}, page.Choices...)
	}
	return page
}

func firmwareVerificationPage(v firmware.Verification, lang string) Page {
	l := translator(lang)
	page := Page{Availability: v.Availability}
	switch {
	case v.State == "verified" && v.Availability == domain.Available:
		page.Found = []string{l("Firmware terverifikasi", "Firmware verified")}
		page.Pose = mascot.Success
	case v.State == "reboot_required":
		page.Found = []string{l("Firmware menunggu restart", "Firmware awaiting restart")}
	case v.State == "failed":
		page.Found = []string{l("Pembaruan firmware gagal", "Firmware update failed")}
	default:
		page.Found = []string{l("Hasil firmware belum terverifikasi", "Firmware outcome unverified"), statusNames[v.Availability].Text(lang)}
	}
	if v.Installed != "" {
		page.Found = append(page.Found, l("Versi aktif", "Active version")+" · "+v.Installed)
	}
	page.Snapshot = maintenanceSnapshot(v.Availability, "firmware", "fwupd verification", v.Detail())
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
	if snap.Backend != "e2fsprogs" && snap.Detail != "" {
		page.Advice = []string{snap.Detail}
	}
	page.Evidence = append(page.Evidence, snap.Detail)
	page.Snapshot = maintenanceSnapshot(snap.Availability, "filesystem", "Filesystem inventory", fmt.Sprintf("%d volumes detected", len(snap.Volumes)))
	for _, v := range snap.Volumes {
		target := domain.SafeText(v.Device)
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s · mounted=%t", target, v.Filesystem, v.Mounted()))
		if v.CheckCandidate() {
			page.Choices = append(page.Choices, Choice{l("Periksa baca-saja", "Check read-only") + " " + target, "filesystem-check", v.Device})
			if v.OfflineCandidate() && !v.ReadOnly && m.Available() == nil {
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

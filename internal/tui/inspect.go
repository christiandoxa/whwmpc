package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/storage"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func Inspector(scanner *inspection.Scanner, store state.Store, maintenance *workflow.Maintenance) Inspect {
	return func(ctx context.Context, req Request) (page Page) {
		defer func() {
			if !maintenance.Demo && len(page.Findings) > 0 {
				page.Choices = appendChoice(page.Choices, saveScanChoice(req.Language))
			}
		}()
		if page, handled := previewAction(ctx, maintenance, req); handled {
			return page
		}
		if req.Choice.Kind == "export" {
			return exportReport(ctx, scanner, store, req, maintenance.Demo)
		}
		if req.Choice.Kind == "save-scan" {
			return saveScan(scanner, store, req, maintenance.Demo)
		}
		allowActions := maintenance.Available() == nil
		a := diagnostics.Analyzer{Language: req.Language, Now: scanner.Now()}
		switch req.Intent {
		case assistant.Storage:
			snapshot := scanner.SMART(ctx, req.Choice.Kind == "rescan")
			return withFindings(storagePage(snapshot, req.Language), a.SMART(snapshot), req.Language)
		case assistant.Trim:
			snapshot := scanner.TRIM(ctx)
			page := withFindings(trimPage(snapshot, req.Language), a.TRIM(snapshot), req.Language)
			if allowActions {
				page.Choices = actionChoices(page.Choices, page.Findings, snapshot.Mounts)
			}
			return page
		case assistant.Power:
			snapshot := scanner.Power(ctx)
			page := withFindings(powerPage(snapshot, req.Language), a.Power(snapshot), req.Language)
			if allowActions {
				page.Choices = actionChoices(page.Choices, page.Findings, nil)
			}
			return page
		case assistant.History:
			return historyPage(store, maintenance, req.Language)
		case assistant.Cleanup:
			return cleanupPage(ctx, maintenance, req.Language)
		case assistant.Slow:
			return withFindings(overviewPage(req.Overview, req.Language), a.Slow(req.Overview, scanner.Power(ctx)), req.Language)
		case assistant.Recommend:
			workload := req.Workload
			if req.Choice.Kind == "context" {
				workload = ""
			}
			var snapshot power.Snapshot
			if slices.Contains(assistant.Workloads, workload) {
				snapshot = scanner.Power(ctx)
			}
			page := withFindings(Page{}, a.Recommend(req.Overview, snapshot, workload), req.Language)
			l := translator(req.Language)
			if allowActions {
				page.Choices = actionChoices(page.Choices, page.Findings, nil)
			}
			if workload != "" {
				page.Choices = append(page.Choices, Choice{Label: l("Ubah konteks", "Change context"), Kind: "context"})
			}
			if req.Choice.Kind == "workload" && !maintenance.Demo && workload != "" {
				if err := store.SetPreference("workload", string(workload)); err != nil {
					page.Advice = append(page.Advice, l("Preferensi hanya tersimpan untuk sesi ini.", "Preference is available for this session only."))
				}
			}
			return page
		default:
			return Page{Availability: domain.Unsupported, Found: []string{copy.needsData.Text(req.Language)}}
		}
	}
}

func withFindings(page Page, findings []diagnostics.Finding, lang string) Page {
	l := translator(lang)
	page.Findings = findings
	base := page.Found
	evidence := page.Evidence
	page.Found, page.Meaning, page.Advice, page.Evidence = nil, nil, nil, nil
	if page.Availability == "" && len(findings) > 0 {
		page.Availability = findings[0].Availability
	}
	for i, f := range findings {
		label := statusNames[f.Availability].Text(lang)
		switch f.Severity {
		case diagnostics.Critical:
			label = l("Kritis", "Critical") + " · " + label
			page.Pose = mascot.Critical
		case diagnostics.Warning:
			label = l("Peringatan", "Warning") + " · " + label
			if page.Pose != mascot.Critical {
				page.Pose = mascot.Warning
			}
		}
		if i < 2 {
			page.Found = append(page.Found, "["+label+"] "+f.Observation)
			page.Meaning = append(page.Meaning, f.Meaning)
			page.Advice = append(page.Advice, f.Recommendation)
		}
		page.Evidence = append(page.Evidence, f.ID+" · "+label, f.Observation, f.Meaning, f.Recommendation,
			l("Batas", "Limits")+" · "+f.Limits, f.Source+" · "+displayTime(f.At, "2006-01-02 15:04 MST"))
		for _, action := range f.Actions {
			switch action.Kind {
			case "navigate", "workload", "rescan":
				page.Choices = appendChoice(page.Choices, Choice{Label: action.Label, Kind: action.Kind, Value: action.Target})
			}
		}
	}
	if len(findings) > 0 {
		page.Evidence = append(page.Evidence, base...)
	}
	page.Evidence = append(page.Evidence, evidence...)
	if len(findings) == 0 {
		page.Found = base
	}
	if page.Pose == mascot.Critical && len(page.Advice) > 0 {
		page.Priority = page.Advice[0]
	}
	return page
}

func appendChoice(choices []Choice, next Choice) []Choice {
	if slices.ContainsFunc(choices, func(c Choice) bool { return c.Kind == next.Kind && c.Value == next.Value }) {
		return choices
	}
	return append(choices, next)
}

func storagePage(audit storage.SMARTAudit, lang string) Page {
	l := translator(lang)
	page := Page{Availability: audit.Availability}
	if audit.Detail != "" {
		page.Found = append(page.Found, audit.Detail)
	}
	page.Evidence = append(page.Evidence, "smartctl · "+audit.CapturedAt.Format("15:04:05 MST"))
	for _, drive := range audit.Drives {
		line := strings.TrimSpace(drive.Name + " " + drive.Model)
		page.Found = append(page.Found, fmt.Sprintf("%s · %s", line, statusNames[drive.Availability].Text(lang)))
		if drive.Detail != "" {
			page.Evidence = append(page.Evidence, line+" · "+drive.Detail)
		}
		if len(drive.MountIDs) > 0 {
			page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s %v", drive.Name, l("ID mount", "mount IDs"), drive.MountIDs))
		}
		h := drive.Health
		if h == nil {
			continue
		}
		if h.Passed != nil {
			page.Evidence = append(page.Evidence, fmt.Sprintf("SMART passed = %t · exit %d", *h.Passed, h.ExitCode))
		}
		if warning := h.CriticalWarning; (warning != nil && *warning != 0) || (h.Passed != nil && !*h.Passed) ||
			h.ExitCode&(storage.SMARTDiskFailing|storage.SMARTCurrentThreshold) != 0 {
			page.Pose = mascot.Critical
			page.Advice = append(page.Advice, l("Peringatan SMART kritis. Prioritaskan backup dan periksa drive.", "Critical SMART evidence. Prioritize backup and inspect the drive."))
		}
		var facts []string
		if h.CriticalWarning != nil {
			facts = append(facts, fmt.Sprintf("Critical Warning 0x%x", *h.CriticalWarning))
		}
		if h.TemperatureC != nil {
			facts = append(facts, fmt.Sprintf("%d °C", *h.TemperatureC))
		}
		if h.PercentageUsed != nil {
			facts = append(facts, fmt.Sprintf("%s %d%%", l("Estimasi endurance terpakai", "Estimated endurance used"), *h.PercentageUsed))
		}
		if h.Spare != nil {
			spare := fmt.Sprintf("%s %d%%", l("Spare tersedia", "Available spare"), *h.Spare)
			if h.SpareThreshold != nil {
				spare += fmt.Sprintf(" · %s %d%%", l("ambang", "threshold"), *h.SpareThreshold)
			}
			facts = append(facts, spare)
		}
		facts = appendKnown(facts, h.MediaErrors, l("Galat media", "Media errors"))
		facts = appendKnown(facts, h.UnsafeShutdowns, l("Mati tidak aman", "Unsafe shutdowns"))
		facts = appendKnown(facts, h.PowerOnHours, l("Jam aktif", "Power-on hours"))
		facts = appendKnown(facts, h.DataUnitsRead, l("Unit baca (512.000 B)", "Read units (512,000 B)"))
		facts = appendKnown(facts, h.DataUnitsWritten, l("Unit tulis (512.000 B)", "Write units (512,000 B)"))
		facts = append(facts, h.ExitFlags...)
		for _, attribute := range h.ATAAttributes {
			facts = append(facts, fmt.Sprintf("ATA %d %s · %s", attribute.ID, attribute.Name, attribute.Raw))
		}
		page.Evidence = append(page.Evidence, facts...)
	}
	if len(page.Found) == 0 {
		page.Found = []string{statusNames[audit.Availability].Text(lang)}
	}
	page.Meaning = []string{l("SMART melaporkan status yang diketahui drive saat ini. Endurance terpakai adalah estimasi vendor; angkanya dapat melebihi 100%.",
		"SMART reports the drive's known state. Endurance used is a vendor estimate and may exceed 100%.")}
	page.Choices = []Choice{{Label: l("Baca ulang SMART", "Rescan SMART"), Kind: "rescan"}}
	return page
}

func appendKnown(lines []string, value, label string) []string {
	if value != "" {
		return append(lines, label+" · "+value)
	}
	return lines
}

func trimPage(audit storage.TrimAudit, lang string) Page {
	l := translator(lang)
	page := Page{Availability: audit.Availability}
	timer := audit.Timer
	if timer.Availability == domain.Available {
		page.Found = append(page.Found, fmt.Sprintf("fstrim.timer · %s / %s", timer.UnitFileState, timer.ActiveState))
	} else {
		page.Found = append(page.Found, l("Jadwal", "Schedule")+" · "+statusNames[timer.Availability].Text(lang))
	}
	page.Found = append(page.Found, fmt.Sprintf("fstrim · %s", statusNames[audit.Manual].Text(lang)))
	page.Evidence = appendKnown(page.Evidence, timer.Detail, l("Jadwal", "Schedule"))
	page.Evidence = appendKnown(page.Evidence, timer.NextElapse, l("Jadwal berikutnya", "Next scheduled"))
	page.Evidence = appendKnown(page.Evidence, timer.NextMonotonic, l("Jadwal monotonic", "Monotonic deadline"))
	page.Evidence = appendKnown(page.Evidence, timer.LastTrigger, l("Trigger terakhir", "Last trigger"))
	page.Evidence = appendKnown(page.Evidence, timer.LastServiceExit, l("Service terakhir selesai", "Last service exit"))
	page.Evidence = appendKnown(page.Evidence, timer.ServiceResult, l("Hasil service", "Service result"))
	page.Evidence = append(page.Evidence, l("Bukti jurnal", "Journal evidence")+" · "+statusNames[timer.Journal].Text(lang))
	for _, mount := range audit.Mounts {
		line := fmt.Sprintf("%s · %s · %s", mount.Point, mount.FSType, statusNames[mount.Discard].Text(lang))
		page.Evidence = append(page.Evidence, line+" · "+mount.Evidence)
		if len(mount.MountOptions) > 0 {
			page.Evidence = append(page.Evidence, l("Opsi mount", "Mount options")+" · "+strings.Join(mount.MountOptions, ","))
		}
		for _, layer := range mount.Layers {
			size := "?"
			if layer.MaxDiscardBytes != nil {
				size = bytesText(*layer.MaxDiscardBytes)
			}
			page.Evidence = append(page.Evidence, fmt.Sprintf("%s (%s) · discard max %s", layer.Name, layer.DeviceID, size))
		}
		if mount.LastTrim != nil {
			page.Evidence = append(page.Evidence, mount.LastTrim.RecordedAt.Format("2006-01-02 15:04 MST")+" · "+
				bytesText(mount.LastTrim.PotentialDiscardBytes)+" "+l("potensi discard", "potential discard"))
		} else {
			page.Evidence = append(page.Evidence, l("Belum ada bukti TRIM untuk mount ini.", "No TRIM evidence for this mount."))
		}
	}
	page.Meaning = []string{l("Timer aktif menunjukkan jadwal. Bukti discard dinilai per filesystem.",
		"An active timer shows scheduling. Discard evidence is evaluated per filesystem.")}
	return page
}

func powerPage(snapshot power.Snapshot, lang string) Page {
	l := translator(lang)
	page := Page{Availability: snapshot.CPUFreqStatus}
	p := snapshot.Profiles
	page.Found = []string{fmt.Sprintf("CPUFreq · %s · %d %s", statusNames[snapshot.CPUFreqStatus].Text(lang), len(snapshot.Policies), l("policy", "policies"))}
	if p.Active != "" {
		page.Found = append(page.Found, fmt.Sprintf("%s · %s", l("Profil aktif", "Active profile"), p.Active))
	}
	page.Found = append(page.Found, l("Sumber daya", "Power source")+" · "+snapshot.PowerSource)
	if len(p.Conflicts) > 0 {
		page.Pose = mascot.Warning
		page.Advice = []string{l("Selesaikan konflik pengelola sebelum mengubah profil.", "Resolve manager conflicts before changing profiles.")}
	} else if snapshot.PowerStatus == domain.Available && len(snapshot.Batteries) != 0 {
		page.Pose = mascot.Battery
		for _, battery := range snapshot.Batteries {
			if snapshot.PowerSource == "ac" && strings.EqualFold(battery.Status, "Charging") {
				page.Pose = mascot.Charging
			}
		}
	}
	page.Evidence = []string{"CPUFreq · " + snapshot.At.Format("15:04:05 MST"), "PPD · " + statusNames[p.Status].Text(lang)}
	page.Evidence = appendKnown(page.Evidence, p.Detail, l("Backend", "Backend"))
	page.Evidence = appendKnown(page.Evidence, p.Degraded, l("Performa menurun", "Performance degraded"))
	page.Evidence = appendKnown(page.Evidence, p.Inhibited, l("Performa dibatasi", "Performance inhibited"))
	page.Evidence = appendKnown(page.Evidence, strings.Join(p.Available, ", "), l("Profil tersedia", "Available profiles"))
	page.Evidence = appendKnown(page.Evidence, strings.Join(p.Conflicts, ", "), l("Konflik", "Conflicts"))
	if snapshot.Boost != nil {
		page.Evidence = append(page.Evidence, fmt.Sprintf("Boost %t · %s", *snapshot.Boost, snapshot.BoostSource))
	}
	page.Evidence = appendKnown(page.Evidence, snapshot.PlatformProfile, "Platform profile")
	for _, battery := range snapshot.Batteries {
		line := fmt.Sprintf("%s · %s", battery.Name, battery.Status)
		if battery.Percent != nil {
			line += fmt.Sprintf(" · %d%%", *battery.Percent)
		}
		page.Evidence = append(page.Evidence, line)
	}
	for _, policy := range snapshot.Policies {
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s / %s · EPP %s", policy.Name, policy.Driver, policy.Governor, policy.EPP))
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s %s · %s %s · %s %s",
			l("Observasi", "Observed"), khz(policy.ObservedKHz), l("Rata-rata", "Average"), khz(policy.AverageKHz), "Driver", khz(policy.DriverKHz)))
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s %s–%s · %s %s–%s",
			l("Batas", "Limits"), khz(policy.ConfigMinKHz), khz(policy.ConfigMaxKHz), "HW", khz(policy.HardwareMinKHz), khz(policy.HardwareMaxKHz)))
		if policy.FirmwareCapKHz != nil {
			page.Evidence = append(page.Evidence, "BIOS · "+khz(policy.FirmwareCapKHz))
		}
		if policy.Boost != nil {
			page.Evidence = append(page.Evidence, fmt.Sprintf("Boost policy · %t", *policy.Boost))
		}
		page.Evidence = appendKnown(page.Evidence, strings.Join(policy.EPPChoices, ", "), "EPP options")
	}
	for _, cpu := range snapshot.EnergyBias {
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · EPB %d", cpu.CPU, cpu.Value))
	}
	page.Meaning = []string{l("scaling_cur_freq adalah nilai driver. Clock rendah saat idle dapat normal.",
		"scaling_cur_freq is a driver value. A low idle clock can be normal.")}
	return page
}

func khz(v *uint64) string {
	if v == nil {
		return "?"
	}
	return fmt.Sprintf("%.0f MHz", float64(*v)/1000)
}

func translator(lang string) func(string, string) string {
	return func(id, en string) string { return (Words{id, en}).Text(lang) }
}

func actionChoices(choices []Choice, findings []diagnostics.Finding, mounts []storage.Mount) []Choice {
	for _, finding := range findings {
		for _, a := range finding.Actions {
			switch a.Kind {
			case actions.PowerProfile:
				choices = appendChoice(choices, Choice{Label: a.Label, Kind: a.Kind, Value: a.Target})
			case actions.EnableTrimTimer:
				choices = appendChoice(choices, Choice{Label: a.Label, Kind: a.Kind})
			case actions.TrimMount:
				id, err := strconv.ParseUint(a.Target, 10, 64)
				if err != nil {
					continue
				}
				for _, mount := range mounts {
					if mount.ID == id && mount.Point != "" {
						choices = appendChoice(choices, Choice{Label: a.Label + " · " + domain.SafeText(mount.Point), Kind: a.Kind, Value: mount.Point})
						break
					}
				}
			}
		}
	}
	return choices
}

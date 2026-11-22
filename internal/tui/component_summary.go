package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

func ssdSummary(audit storage.SMARTAudit, lang string) *ResultSummary {
	l := translator(lang)
	s := &ResultSummary{Headline: l("Pemeriksaan kesehatan drive", "Drive health check"), At: audit.CapturedAt}
	if len(audit.Drives) == 0 {
		s.Statuses = []resultStatus{{Category: "ssd", Label: "SMART", Value: audit.Detail, Availability: audit.Availability}}
		return s
	}
	// Reuse the analyzer's priorities for each drive without probing it again.
	a := diagnostics.Analyzer{Language: lang, Now: audit.CapturedAt}
	best, rank := 0, 4
	for i, d := range audit.Drives {
		findings := a.SMART(storage.SMARTAudit{Source: audit.Source, CapturedAt: audit.CapturedAt, Drives: []storage.Drive{d}})
		for _, f := range findings {
			if next := severityRank(f); next < rank {
				best, rank = i, next
			}
		}
	}
	d := audit.Drives[best]
	name := strings.TrimSpace(d.Model + " " + d.Name)
	if name == "" {
		name = fmt.Sprintf("Drive #%d", best+1)
	}
	s.Metrics = append(s.Metrics, resultMetric{Label: "Drive", Value: name, Availability: d.Availability})
	if n := len(audit.Drives) - 1; n > 0 {
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Drive lain", "Other drives"), Value: strconv.Itoa(n), Availability: domain.Available})
	}
	health := resultMetric{Category: "ssd", Label: "SMART", Availability: d.Availability}
	if audit.Source == storage.WindowsStorageSource {
		health.Label = l("Status provider", "Provider status")
		if hasData(d.Availability) {
			health.Value = d.HealthStatus
		}
	} else if h := d.Health; hasData(d.Availability) && h != nil && h.Passed != nil {
		health.Value = l("Gagal", "Failed")
		if *h.Passed {
			health.Value = l("Lolos", "Passed")
		}
	}
	if health.Value == "" && hasData(health.Availability) {
		health.Availability = domain.Unknown
	}
	s.Metrics = append(s.Metrics, health)
	h := d.Health
	if !hasData(d.Availability) || h == nil {
		return s
	}
	add := func(label, value, unit, note string) {
		s.Metrics = append(s.Metrics, resultMetric{Label: label, Value: value, Unit: unit, Note: note, Availability: d.Availability})
	}
	if v := h.CriticalWarning; v != nil {
		add("NVMe critical warning", fmt.Sprintf("0x%02X", *v), "", "")
	}
	if v := h.TemperatureC; v != nil {
		add(l("Suhu drive", "Drive temperature"), strconv.FormatInt(*v, 10), "°C", "")
	}
	if v := h.PercentageUsed; v != nil {
		note := ""
		if *v > 100 {
			note = l("melampaui 100%", "above 100%")
		}
		add(l("Endurance terpakai", "Endurance used"), strconv.FormatUint(*v, 10), "%", note)
	}
	if h.Spare != nil || h.SpareThreshold != nil {
		spare, threshold := "—", "—"
		if h.Spare != nil {
			spare = fmt.Sprintf("%d%%", *h.Spare)
		}
		if h.SpareThreshold != nil {
			threshold = fmt.Sprintf("%d%%", *h.SpareThreshold)
		}
		add(l("Spare / ambang", "Spare / threshold"), spare+" / "+threshold, "", "")
	}
	for _, counter := range []struct{ label, value string }{
		{l("Kesalahan media", "Media errors"), h.MediaErrors},
		{l("Galat baca", "Uncorrected reads"), h.ReadErrorsUncorrected},
		{l("Galat tulis", "Uncorrected writes"), h.WriteErrorsUncorrected},
		{l("Jam menyala", "Power-on hours"), h.PowerOnHours},
	} {
		if counter.value != "" && strings.IndexFunc(counter.value, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
			add(counter.label, counter.value, "", "")
			s.Metrics[len(s.Metrics)-1].Secondary = true
		}
	}
	return s
}

func trimSummary(audit storage.TrimAudit, lang string) *ResultSummary {
	l := translator(lang)
	support := resultStatus{Category: "trim", Label: l("Dukungan", "Support"), Availability: domain.Unknown}
	schedule := resultStatus{Category: "trim", Label: l("Jadwal / kebijakan", "Schedule / policy"), Availability: domain.Unknown}
	evidence := resultStatus{Category: "trim", Label: l("Bukti terakhir", "Last evidence"), Availability: domain.Unknown}
	if audit.Source != "" {
		reported := &support
		on, off := l("didukung", "supported"), l("tidak didukung", "unsupported")
		if audit.Source == storage.WindowsTRIMSource {
			reported = &schedule
			on, off = l("aktif", "enabled"), l("nonaktif", "disabled")
		}
		reported.Availability = audit.Availability
		if hasData(audit.Availability) {
			var policies []string
			for _, p := range audit.Policy {
				value := off
				if p.Enabled {
					value = on
				}
				policies = append(policies, p.Scope+" "+value)
			}
			reported.Value = strings.Join(policies, " · ")
			if len(policies) == 0 {
				reported.Availability = domain.Unknown
			}
		}
	} else {
		support.Availability = audit.Availability
		supported := 0
		var recent *storage.TrimRecord
		var mountPoint string
		for _, m := range audit.Mounts {
			if m.Discard == domain.Available {
				supported++
			}
			if r := m.LastTrim; r != nil && !r.RecordedAt.IsZero() && (recent == nil || r.RecordedAt.After(recent.RecordedAt)) {
				recent, mountPoint = r, m.Point
			}
		}
		if len(audit.Mounts) != 0 {
			support.Value = fmt.Sprintf(l("%d/%d mount mendukung discard", "%d/%d mounts support discard"), supported, len(audit.Mounts))
		}
		timer := audit.Timer
		schedule.Availability = timer.Availability
		if hasData(timer.Availability) && timer.ActiveState != "" {
			schedule.Value = "fstrim.timer · " + timer.ActiveState
			if timer.UnitFileState != "" {
				schedule.Value += " / " + timer.UnitFileState
			}
		}
		completed := hasData(timer.Availability) && timer.LastServiceExit != "" && timer.ServiceResult != ""
		failed := completed && (timer.ServiceResult != "success" || timer.ServiceExitCode != nil && *timer.ServiceExitCode != 0)
		switch {
		case failed || completed && recent == nil:
			evidence.Availability = domain.Available
			evidence.Value = "fstrim.service · " + timer.ServiceResult
			if timer.ServiceExitCode != nil && *timer.ServiceExitCode != 0 {
				evidence.Value += fmt.Sprintf(" (exit %d)", *timer.ServiceExitCode)
			}
			evidence.Value += " · " + timer.LastServiceExit
			if failed {
				evidence.Severity = diagnostics.Warning
			}
		case recent != nil:
			evidence.Availability = domain.Available
			evidence.Value = mountPoint + " · " + recent.RecordedAt.Format("02 Jan 15:04 MST") + " · " +
				bytesText(recent.PotentialDiscardBytes) + " " + l("potensi discard", "potential discard")
		case !hasData(timer.Journal) && timer.Journal != "":
			evidence.Availability = timer.Journal
		}
	}
	return &ResultSummary{Headline: l("Pemeriksaan TRIM", "TRIM audit"), At: audit.CapturedAt, Statuses: []resultStatus{support, schedule, evidence}}
}

func powerSummary(p power.Snapshot, lang string) *ResultSummary {
	l := translator(lang)
	s := &ResultSummary{Headline: l("Status daya dan CPU", "Power and CPU status"), At: p.At}
	if p.LinuxInterfaces() {
		profile := resultMetric{Category: "power", Label: l("Profil aktif", "Active profile"), Availability: p.Profiles.Status}
		if hasData(profile.Availability) {
			profile.Value = p.Profiles.Active
			if profile.Value == "" {
				profile.Availability = domain.Unknown
			}
			if options := strings.Join(p.Profiles.Available, " · "); options != "" {
				s.Statuses = append(s.Statuses, resultStatus{Label: l("Profil tersedia", "Available profiles"), Value: options, Availability: p.Profiles.Status})
			}
		}
		s.Metrics = append(s.Metrics, profile)
	}
	source := resultMetric{Category: "power", Label: l("Sumber daya", "Power source"), Availability: p.PowerStatus}
	if hasData(p.PowerStatus) {
		switch p.PowerSource {
		case "ac":
			source.Value = "AC"
		case "battery":
			source.Value = l("Baterai", "Battery")
		default:
			source.Availability = domain.Unknown
		}
	}
	s.Metrics = append(s.Metrics, source)
	if hasData(p.PowerStatus) && len(p.Batteries) > 0 {
		b := p.Batteries[0]
		battery := resultMetric{Label: l("Baterai", "Battery"), Availability: p.PowerStatus, Note: b.Status}
		if b.Percent != nil {
			battery = metricUsage(battery, float64(*b.Percent), true)
		} else {
			battery.Availability = domain.Unknown
		}
		s.Metrics = append(s.Metrics, battery)
	}
	if hasData(p.CPUFreqStatus) && len(p.Policies) > 0 {
		policy := p.Policies[0]
		for _, clock := range []struct {
			label string
			khz   *uint64
		}{
			{l("Clock terukur", "Observed clock"), policy.ObservedKHz},
			{l("Clock rata-rata", "Average clock"), policy.AverageKHz},
			{l("Clock driver", "Driver clock"), policy.DriverKHz},
		} {
			if clock.khz != nil {
				s.Metrics = append(s.Metrics, resultMetric{Label: clock.label, Value: number(float64(*clock.khz)/1000, 3), Unit: "MHz", Note: policy.Name, Availability: p.CPUFreqStatus})
			}
		}
		if policy.ConfigMinKHz != nil || policy.ConfigMaxKHz != nil {
			s.Statuses = append(s.Statuses, resultStatus{Label: l("Batas konfigurasi", "Configured limits"), Value: khz(policy.ConfigMinKHz) + " – " + khz(policy.ConfigMaxKHz) + " · " + policy.Name, Availability: p.CPUFreqStatus})
		}
	} else if p.LinuxInterfaces() {
		s.Statuses = append(s.Statuses, resultStatus{Category: "power", Label: "CPUFreq", Availability: p.CPUFreqStatus})
	}
	return s
}

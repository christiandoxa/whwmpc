package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func gpuPage(ctx context.Context, m *workflow.Maintenance, lang string) Page {
	l := translator(lang)
	if m.Demo {
		return Page{Availability: domain.Unsupported, Found: []string{l("DEMO · akses GPU terkunci", "DEMO · GPU access locked")}}
	}
	return gpuInventoryPage(hardware.Scan(ctx, sysrun.Native{}), m.Available() == nil, lang)
}

func gpuInventoryPage(snapshot hardware.Snapshot, canTune bool, lang string) Page {
	l := translator(lang)
	page := Page{Availability: snapshot.Availability, Found: []string{l("GPU yang terdeteksi", "Detected GPUs")}}
	page.Summary = gpuSummary(snapshot, lang)
	page.Found = append(page.Found, fmt.Sprintf("%d · %s", len(snapshot.Devices), statusNames[snapshot.Availability].Text(lang)))
	page.Evidence = append(page.Evidence, snapshot.Detail)
	findings := []diagnostics.Finding{}
	for i, device := range snapshot.Devices {
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s · %s · %s %s", device.PCI, device.Model, device.Backend, device.Driver, device.Version))
		values := device.Metrics
		powerLabel := l("Daya", "Power")
		switch values.PowerScope {
		case "board":
			powerLabel = l("Daya papan GPU", "GPU board power")
		case "soc":
			powerLabel = l("Daya SoC", "SoC power")
		}
		var samples []string
		for _, entry := range []struct {
			name, unit string
			value      *int64
		}{
			{l("Beban", "Load"), "%", values.UtilizationPct}, {l("Suhu", "Temperature"), "°C", values.TemperatureC},
			{"VRAM", "B", values.VRAMUsedBytes}, {l("Kapasitas VRAM", "VRAM capacity"), "B", values.VRAMTotalBytes},
			{powerLabel, "mW", values.PowerMilliW}, {l("Batas daya yang ditegakkan", "Enforced power limit"), "mW", values.EnforcedMilliW},
			{l("Clock inti", "Core clock"), "MHz", values.CoreMHz}, {l("Clock memori", "Memory clock"), "MHz", values.MemoryMHz},
		} {
			if entry.value != nil {
				samples = append(samples, fmt.Sprintf("%s %d %s", entry.name, *entry.value, entry.unit))
			}
		}
		page.Evidence = append(page.Evidence, samples...)
		page.Evidence = append(page.Evidence, statusNames[device.TuningAvailability].Text(lang)+" · "+device.TuningDetail)
		for _, control := range device.Controls {
			label := fmt.Sprintf("%s · %s · %d–%d %s", device.PCI, actionLabel(control.Kind, lang), control.Min, control.Max, control.Unit)
			current := l("aktif", "active")
			if control.Kind == hardware.PowerLimit {
				current = l("diminta", "requested")
			}
			page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s %d %s", label, current, control.Current, control.Unit))
			if canTune && device.ID != "" {
				page.Choices = append(page.Choices, Choice{label, "gpu-value", control.Kind + " " + device.ID})
			}
		}
		findings = append(findings, diagnostics.Finding{ID: fmt.Sprintf("gpu.telemetry.%d", i+1), Category: "gpu", Availability: device.Availability,
			Severity: diagnostics.Info, Observation: fmt.Sprintf("GPU %d · %v", i+1, samples), Source: "GPU vendor telemetry", At: snapshot.At})
	}
	page.Snapshot = &workflow.Snapshot{Input: report.Input{At: snapshot.At, Findings: findings}}
	return page
}

func gpuSummary(snapshot hardware.Snapshot, lang string) *ResultSummary {
	l := translator(lang)
	s := &ResultSummary{Headline: l("Inventaris GPU", "GPU inventory"), At: snapshot.At}
	if !hasData(snapshot.Availability) && len(snapshot.Devices) == 0 {
		s.Statuses = []resultStatus{{Label: "GPU", Value: snapshot.Detail, Availability: snapshot.Availability}}
		return s
	}
	s.Metrics = append(s.Metrics, resultMetric{Label: l("Terdeteksi", "Detected"), Value: strconv.Itoa(len(snapshot.Devices)), Availability: snapshot.Availability})
	if len(snapshot.Devices) == 0 {
		return s
	}
	i := slices.IndexFunc(snapshot.Devices, func(d hardware.Device) bool { return hasData(d.Availability) })
	d := snapshot.Devices[max(0, i)]
	s.Statuses = []resultStatus{{Label: "GPU", Value: d.Model + " · " + d.PCI, Availability: d.Availability},
		{Label: "Driver", Value: d.Backend + " · " + d.Driver + " " + d.Version, Availability: d.Availability},
		{Label: "Tuning", Value: d.TuningDetail, Availability: d.TuningAvailability}}
	if !hasData(d.Availability) {
		return s
	}
	values := d.Metrics
	load := resultMetric{Label: l("Beban GPU", "GPU load"), Availability: d.Availability}
	if values.UtilizationPct != nil {
		s.Metrics = append(s.Metrics, metricUsage(load, float64(*values.UtilizationPct), true))
	}
	if v := values.TemperatureC; v != nil {
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Suhu GPU", "GPU temperature"), Value: strconv.FormatInt(*v, 10), Unit: "°C", Availability: d.Availability})
	}
	if u, t := values.VRAMUsedBytes, values.VRAMTotalBytes; u != nil || t != nil {
		used, total := "—", "—"
		if u != nil && *u >= 0 {
			used = bytesText(uint64(*u))
		}
		if t != nil && *t > 0 {
			total = bytesText(uint64(*t))
		}
		status := d.Availability
		if used == "—" || total == "—" {
			status = domain.Partial
		} else if *u > *t {
			status = domain.Error
		}
		s.Metrics = append(s.Metrics, resultMetric{Label: "VRAM", Value: used + " / " + total, Availability: status})
	}
	if v := values.PowerMilliW; v != nil && *v >= 0 {
		label := l("Daya GPU", "GPU power")
		switch values.PowerScope {
		case "board":
			label = l("Daya papan GPU", "GPU board power")
		case "soc":
			label = l("Daya SoC", "SoC power")
		}
		s.Metrics = append(s.Metrics, resultMetric{Label: label, Value: number(float64(*v)/1000, 3), Unit: "W", Availability: d.Availability})
	}
	if v := values.CoreMHz; v != nil && *v >= 0 {
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Clock inti", "Core clock"), Value: strconv.FormatInt(*v, 10), Unit: "MHz", Availability: d.Availability, Secondary: true})
	}
	return s
}

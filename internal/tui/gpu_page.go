package tui

import (
	"context"
	"fmt"

	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func gpuPage(ctx context.Context, m *workflow.Maintenance, lang string) Page {
	l := translator(lang)
	if m.Demo {
		return Page{Found: []string{l("DEMO · akses GPU terkunci", "DEMO · GPU access locked")}}
	}
	snapshot := hardware.Scan(ctx, sysrun.Native{})
	page := Page{Availability: snapshot.Availability, Found: []string{l("GPU yang terdeteksi", "Detected GPUs")}}
	page.Found = append(page.Found, fmt.Sprintf("%d · %s", len(snapshot.Devices), statusNames[snapshot.Availability].Text(lang)))
	page.Evidence = append(page.Evidence, snapshot.Detail)
	findings := []diagnostics.Finding{}
	for i, device := range snapshot.Devices {
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s · %s · %s %s", device.PCI, device.Model, device.Backend, device.Driver, device.Version))
		values := device.Metrics
		var samples []string
		for _, entry := range []struct {
			name, unit string
			value      *int64
		}{
			{l("Beban", "Load"), "%", values.UtilizationPct}, {l("Suhu", "Temperature"), "°C", values.TemperatureC},
			{"VRAM", "B", values.VRAMUsedBytes}, {l("Kapasitas VRAM", "VRAM capacity"), "B", values.VRAMTotalBytes},
			{l("Daya", "Power"), "mW", values.PowerMilliW}, {l("Clock inti", "Core clock"), "MHz", values.CoreMHz}, {l("Clock memori", "Memory clock"), "MHz", values.MemoryMHz},
		} {
			if entry.value != nil {
				samples = append(samples, fmt.Sprintf("%s %d %s", entry.name, *entry.value, entry.unit))
			}
		}
		page.Evidence = append(page.Evidence, samples...)
		page.Evidence = append(page.Evidence, statusNames[device.TuningAvailability].Text(lang)+" · "+device.TuningDetail)
		for _, control := range device.Controls {
			label := fmt.Sprintf("%s · %s · %d–%d %s", device.PCI, actionLabel(control.Kind, lang), control.Min, control.Max, control.Unit)
			page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s %d %s", label, l("aktif", "active"), control.Current, control.Unit))
			if m.Available() == nil && device.ID != "" {
				page.Choices = append(page.Choices, Choice{label, "gpu-value", control.Kind + " " + device.ID})
			}
		}
		findings = append(findings, diagnostics.Finding{ID: fmt.Sprintf("gpu.telemetry.%d", i+1), Category: "gpu", Availability: device.Availability,
			Severity: diagnostics.Info, Observation: fmt.Sprintf("GPU %d · %v", i+1, samples), Source: "GPU vendor telemetry", At: snapshot.At})
	}
	page.Snapshot = &workflow.Snapshot{Input: report.Input{At: snapshot.At, Findings: findings}}
	return page
}

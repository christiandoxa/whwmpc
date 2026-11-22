package tui

import (
	"fmt"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/privread"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

var componentNames = map[string]Words{
	"cpu": {"CPU", "CPU"}, "memory": {"RAM", "RAM"}, "disk": {"Ruang simpan", "Disk space"},
	"ssd": {"SSD", "SSD"}, "trim": {"TRIM", "TRIM"}, "power": {"Daya", "Power"},
	"temperature": {"Suhu", "Temperature"}, "process": {"Proses", "Processes"}, "recommendation": {"Profil daya", "Power profile"},
	"gpu": {"GPU", "GPU"}, "firmware": {"Firmware", "Firmware"}, "filesystem": {"Filesystem", "Filesystem"},
}

func componentKey(category string) string {
	if category == "thermal" {
		return "temperature"
	}
	return category
}

func componentName(key, lang string) string {
	if name := componentNames[key].Text(lang); name != "" {
		return name
	}
	return domain.SafeText(key)
}

func finishChoice(lang string) Choice {
	return Choice{Words{"Selesai", "Done"}.Text(lang), "finish", ""}
}

func detailsChoice(lang string) Choice {
	return Choice{Words{"Detail komponen", "Component details"}.Text(lang), "details", ""}
}

func findingDetail(f diagnostics.Finding, lang string) Choice {
	l := translator(lang)
	key := componentKey(f.Category)
	label := l("Lihat detail", "View details") + " · " + componentName(key, lang)
	if f.Severity == diagnostics.Critical {
		label = l("Lihat temuan kritis", "View critical findings") + " · " + componentName(key, lang)
	}
	return Choice{label, "component", key}
}

func findingChoices(p Page, f diagnostics.Finding) []Choice {
	var mounts []storage.Mount
	if p.Snapshot != nil && p.Snapshot.Input.Trim != nil {
		mounts = p.Snapshot.Input.Trim.Mounts
	}
	linked := actionChoices(nil, []diagnostics.Finding{f}, mounts)
	for _, a := range f.Actions {
		if a.Kind == "navigate" || a.Kind == "rescan" || a.Kind == "workload" {
			linked = appendChoice(linked, Choice{a.Label, a.Kind, a.Target})
		}
	}
	var allowed []Choice
	for _, c := range linked {
		for _, option := range p.Choices {
			if c.Kind == option.Kind && c.Value == option.Value {
				allowed = appendChoice(allowed, option)
				break
			}
		}
	}
	return allowed
}

func (m Model) componentChoices() []Choice {
	var choices []Choice
	for _, f := range sortedFindings(m.page.Findings) {
		key := componentKey(f.Category)
		if key != "" {
			choices = appendChoice(choices, Choice{componentName(key, m.options.Language), "component", key})
		}
	}
	return choices
}

func (m Model) detailChoices() []Choice {
	var choices []Choice
	if m.component != "" {
		for _, c := range m.page.Choices {
			if c.Kind == "complete-read" && readComponent(privread.Operation(c.Value)) == m.component {
				choices = appendChoice(choices, m.retryReadChoice(privread.Operation(c.Value)))
			}
		}
		for _, f := range sortedFindings(m.page.Findings) {
			if componentKey(f.Category) == m.component {
				for _, c := range findingChoices(m.page, f) {
					choices = appendChoice(choices, c)
				}
			}
		}
		choices = append(choices, m.evidenceChoice())
	} else if m.page.Snapshot != nil {
		choices = append(choices, m.saveChoice())
	}
	return append(choices, m.backChoice())
}

func (m Model) detailTitle() string {
	if m.component != "" {
		return m.tr(menuNames[m.active]) + " · " + componentName(m.component, m.options.Language)
	}
	return m.evidenceChoice().Label
}

func (m Model) componentEvidence() []string {
	var findings []diagnostics.Finding
	for _, f := range m.page.Findings {
		if componentKey(f.Category) == m.component {
			findings = append(findings, f)
		}
	}
	lines := findingEvidence(findings, m.options.Language)
	if p := m.page.Snapshot; p != nil {
		in := p.Input
		lang := m.options.Language
		var data Page
		switch m.component {
		case "ssd":
			if in.SMART != nil {
				data = storagePage(*in.SMART, lang)
			}
		case "trim":
			if in.Trim != nil {
				data = trimPage(*in.Trim, lang)
			}
		case "power", "recommendation":
			if in.Power != nil {
				data = powerPage(*in.Power, lang)
			}
		case "cpu", "process", "disk", "temperature":
			if o := in.Overview; o != nil {
				switch m.component {
				case "cpu":
					data.Found = []string{o.CPUModel}
					if o.LogicalCPUs > 0 {
						data.Found = append(data.Found, fmt.Sprintf("%d %s", o.LogicalCPUs, m.tr(Words{"CPU logis", "logical CPUs"})))
					}
				case "process":
					data.Found = processEvidence(*o, lang)
				case "disk":
					for _, disk := range o.Disks {
						status := resultStatusLabel(disk.Availability, lang)
						if hasData(disk.Availability) && disk.TotalBytes > 0 && disk.FreeBytes <= disk.TotalBytes {
							status = bytesText(disk.FreeBytes) + " / " + bytesText(disk.TotalBytes)
						}
						data.Found = append(data.Found, fmt.Sprintf("%s · %s · %s", disk.Mount, disk.FSType, status))
					}
				case "temperature":
					data.Found = []string{o.Temperature.Source}
				}
			}
		}
		lines = append(lines, data.Found...)
		lines = append(lines, data.Evidence...)
	}
	return distinctEvidence(lines)
}

func distinctEvidence(lines []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, line := range lines {
		key := strings.TrimSpace(domain.SafeText(line))
		if key != "" && !seen[key] {
			out = append(out, key)
			seen[key] = true
		}
	}
	return out
}

func evidenceSource(f diagnostics.Finding) string {
	if f.At.IsZero() {
		return f.Source
	}
	if f.Source == "" {
		return f.At.Format("2006-01-02 15:04:05 MST")
	}
	return f.Source + " · " + f.At.Format("2006-01-02 15:04:05 MST")
}

func readComponent(op privread.Operation) string {
	if op == privread.SMART {
		return "ssd"
	}
	return string(op)
}

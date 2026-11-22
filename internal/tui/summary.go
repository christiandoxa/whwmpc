package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

type ResultSummary struct {
	Headline, Coverage string
	Highlights         []string
	Primary            Choice
}

var checkNames = map[workflow.Check]Words{
	workflow.SystemCheck: {"CPU, memori, dan penyimpanan", "CPU, memory, and storage"},
	workflow.DiskCheck:   {"Ruang simpan", "Disk space"},
	workflow.PowerCheck:  {"Daya dan performa", "Power and performance"},
	workflow.SSDCheck:    {"Kesehatan SSD", "SSD health"},
	workflow.TRIMCheck:   {"Perawatan SSD", "SSD maintenance"},
}

func coverageLine(checks []workflow.Coverage, lang string) string {
	counts := [5]int{}
	seen := make(map[workflow.Check]bool)
	for _, c := range checks {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		switch c.Status {
		case domain.Available:
			counts[0]++
		case domain.Partial:
			counts[1]++
		case domain.Error:
			counts[2]++
		case "":
			counts[4]++
		default:
			counts[3]++
		}
	}
	labels := []Words{{"selesai", "complete"}, {"parsial", "partial"}, {"gagal", "failed"}, {"belum tersedia", "unavailable"}, {"belum dijalankan", "not run"}}
	var items []string
	for i, n := range counts {
		if n > 0 {
			items = append(items, fmt.Sprintf("%d %s", n, labels[i].Text(lang)))
		}
	}
	return strings.Join(items, " · ")
}

func severityRank(f diagnostics.Finding) int {
	switch f.Severity {
	case diagnostics.Critical:
		return 0
	case diagnostics.Warning:
		return 1
	}
	if f.Availability != domain.Available {
		return 2
	}
	return 3
}

func sortedFindings(findings []diagnostics.Finding) []diagnostics.Finding {
	out := slices.Clone(findings)
	slices.SortStableFunc(out, func(a, b diagnostics.Finding) int { return severityRank(a) - severityRank(b) })
	return out
}

var plainCategories = map[string]Words{
	"cpu": {"Beban prosesor", "Processor load"}, "memory": {"Memori", "Memory"}, "disk": {"Ruang simpan", "Disk space"},
	"ssd": {"Kesehatan SSD", "SSD health"}, "trim": {"Perawatan SSD", "SSD maintenance"}, "power": {"Daya", "Power"},
	"thermal": {"Suhu", "Temperature"}, "temperature": {"Suhu", "Temperature"}, "process": {"Aplikasi yang berjalan", "Running applications"},
	"recommendation": {"Profil daya", "Power profile"},
}

func highlight(f diagnostics.Finding, lang string) string {
	l := translator(lang)
	name := plainCategories[f.Category].Text(lang)
	if name == "" {
		name = l("Pemeriksaan", "Check")
	}
	reason := statusNames[f.Availability].Text(lang)
	switch {
	case f.Severity == diagnostics.Critical:
		reason = l("perlu perhatian segera", "needs immediate attention")
	case f.Availability != domain.Available && f.Availability != domain.Partial:
		// Keep the availability label when a measurement could not be taken.
	case f.Severity == diagnostics.Warning:
		switch {
		case f.ID == "trim.scheduler" && slices.ContainsFunc(f.Actions, func(a diagnostics.Action) bool { return a.Kind == actions.EnableTrimTimer }):
			reason = l("jadwal belum aktif", "schedule is not active")
		case strings.HasPrefix(f.ID, "overview.disk."):
			reason = l("ruang hampir habis", "space is running low")
		default:
			reason = l("ada peringatan pada sampel", "sample has a warning")
		}
	case f.Availability == domain.Available:
		reason = l("sampel tercatat", "sample recorded")
	}
	return name + " · " + reason
}

func summarize(page Page, intent assistant.Intent, lang string) ResultSummary {
	l := translator(lang)
	summary := ResultSummary{Coverage: coverageLine(page.Coverage, lang)}
	if len(page.Coverage) == 0 {
		summary.Coverage = coverageLine([]workflow.Coverage{{ID: workflow.Check(intent), Status: page.Availability}}, lang)
	}
	findings := sortedFindings(page.Findings)
	critical, warning := 0, 0
	incomplete := false
	for _, check := range page.Coverage {
		incomplete = incomplete || check.Status != domain.Available
	}
	for _, f := range findings {
		if f.Severity == diagnostics.Critical {
			critical++
		}
		if f.Severity == diagnostics.Warning {
			warning++
		}
		incomplete = incomplete || f.Availability != domain.Available
		if len(summary.Highlights) < 3 {
			summary.Highlights = append(summary.Highlights, highlight(f, lang))
		}
	}
	switch {
	case critical > 0:
		summary.Headline = fmt.Sprintf(l("%d temuan kritis perlu perhatian", "%d critical findings need attention"), critical)
	case warning > 0:
		summary.Headline = fmt.Sprintf(l("Ada %d peringatan untuk ditinjau", "%d warnings to review"), warning)
	case intent == assistant.Slow:
		summary.Headline = l("Gejala lambat belum terlihat pada sampel ini", "Slowdown was not observed in this sample")
	case incomplete:
		summary.Headline = l("Sebagian pemeriksaan perlu ditinjau", "Some checks need review")
	default:
		summary.Headline = l("Tidak ada peringatan pada sampel ini", "No warnings in this sample")
	}
	summary.Primary = Choice{l("Lihat semua hasil", "View all results"), "details", ""}
	if critical > 0 {
		summary.Primary.Label = l("Lihat temuan kritis", "View critical findings")
		return summary
	}
	for _, c := range page.Choices {
		switch c.Kind {
		case actions.EnableTrimTimer, actions.PowerProfile, actions.TrimMount:
			summary.Primary = c
			return summary
		}
		if c.Kind == "navigate" && (warning > 0 || intent == assistant.Power) {
			summary.Primary = c
		}
	}
	if intent == assistant.Slow && summary.Primary.Kind == "details" {
		summary.Primary = Choice{l("Periksa ulang saat gejala muncul", "Check again during the slowdown"), "rescan", ""}
	}
	return summary
}

func findingEvidence(findings []diagnostics.Finding, lang string) []string {
	l := translator(lang)
	var lines []string
	for _, f := range sortedFindings(findings) {
		lines = append(lines, highlight(f, lang), f.Observation, f.Meaning,
			l("Saran", "Advice")+" · "+f.Recommendation,
			l("Sumber", "Source")+" · "+f.Source+" · "+displayTime(f.At, "2006-01-02 15:04 MST"),
			l("Batas", "Limits")+" · "+f.Limits, "")
	}
	return lines
}

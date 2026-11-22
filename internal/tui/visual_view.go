package tui

import (
	"math"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
)

var asciiVisual = strings.NewReplacer("·", "|", "–", "-", "—", "-", "…", "...", "°C", "C", "×", "x")

func (m Model) visualText(s string) string {
	s = domain.SafeText(s)
	if m.options.ASCII {
		return asciiVisual.Replace(s)
	}
	return s
}

func usageBar(pct float64, cells int, ascii bool) string {
	full, empty := "█", "░"
	if ascii {
		full, empty = "#", "-"
	}
	filled := int(math.Round(pct / 100 * float64(cells)))
	return "[" + strings.Repeat(full, filled) + strings.Repeat(empty, cells-filled) + "]"
}

func (m Model) summaryBody(s ResultSummary, w, room int) []string {
	lang := m.options.Language
	l := translator(lang)
	ellipsis := "…"
	if m.options.ASCII {
		ellipsis = "..."
	}
	type row struct {
		text     string
		priority int
	}
	var rows []row
	add := func(text string, style lipgloss.Style, priority int) {
		text = ansi.Truncate(m.visualText(text), w, ellipsis)
		rows = append(rows, row{style.Render(text), priority})
	}
	headline := strings.Split(ansi.Hardwrap(ansi.Wrap(m.visualText(s.Headline), w, ""), w, false), "\n")
	for _, line := range headline[:min(2, len(headline))] {
		add(line, m.palette.accent, 0)
	}
	coverage := l("Cakupan", "Coverage") + "  " + s.Coverage
	if !s.At.IsZero() {
		stamp := s.At.Format("02 Jan 15:04:05 MST")
		if m.stalePage() {
			stamp += " · " + l("KEDALUWARSA", "STALE")
		}
		if ansi.StringWidth(m.visualText(coverage+"  ·  "+stamp)) <= w {
			coverage += "  ·  " + stamp
		} else {
			add(coverage, m.palette.secondary, 0)
			coverage = stamp
		}
	}
	add(coverage, m.palette.secondary, 0)
	for i, exception := range s.Highlights {
		add("!  "+exception, m.palette.alert, 1+min(i, 1))
	}
	if len(s.Metrics)+len(s.Statuses) > 0 {
		add("", m.palette.text, 4)
	}
	labelWidth := 0
	for _, metric := range s.Metrics {
		labelWidth = max(labelWidth, ansi.StringWidth(m.visualText(metric.Label)))
	}
	for _, status := range s.Statuses {
		labelWidth = max(labelWidth, ansi.StringWidth(m.visualText(status.Label)))
	}
	labelWidth = min(labelWidth, w/3)
	label := func(name string) string {
		name = ansi.Truncate(m.visualText(name), labelWidth, ellipsis)
		return name + strings.Repeat(" ", labelWidth-ansi.StringWidth(name)+2)
	}
	for _, metric := range s.Metrics {
		text := label(metric.Label)
		if metric.Usage != nil {
			cells := 10
			if w < 60 {
				cells = 6
			}
			text += usageBar(*metric.Usage, cells, m.options.ASCII) + "  "
		}
		value := metric.Value
		if value == "" {
			value = "—"
		} else if metric.Unit == "%" {
			value += metric.Unit
		} else if metric.Unit != "" {
			value += " " + metric.Unit
		}
		text += value
		style, priority := m.palette.text, 1
		if metric.Secondary {
			priority = 3
		}
		if metric.Availability != domain.Available {
			text += " · " + resultStatusLabel(metric.Availability, lang)
			style, priority = m.palette.alert, min(priority, 2)
		}
		if metric.Note != "" {
			text += "  " + metric.Note
		}
		add(text, style, priority)
	}
	for _, status := range s.Statuses {
		text := label(status.Label)
		style, priority := m.palette.text, 3
		switch status.Severity {
		case diagnostics.Critical:
			text += l("KRITIS", "CRITICAL") + " · "
			style, priority = m.palette.alert, 1
		case diagnostics.Warning:
			text += l("PERINGATAN", "WARNING") + " · "
			style, priority = m.palette.alert, 2
		}
		if status.Availability != domain.Available || status.Value == "" {
			text += resultStatusLabel(status.Availability, lang)
			if status.Value != "" {
				text += " · "
			}
		}
		text += status.Value
		if emphasis(status) {
			style, priority = m.palette.alert, min(priority, 2)
		}
		add(text, style, priority)
	}
	if m.question != "" {
		add(l("Pertanyaan", "Question")+" · "+m.question, m.palette.muted, 5)
	}
	for len(rows) > room {
		drop := 0
		for i, row := range rows {
			if row.priority >= rows[drop].priority {
				drop = i
			}
		}
		rows = slices.Delete(rows, drop, drop+1)
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = row.text
	}
	return lines
}

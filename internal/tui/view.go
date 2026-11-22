package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
)

type palette struct {
	accent, secondary, text, muted, frame, selected, alert lipgloss.Style
}

func newPalette(theme string) palette {
	base := lipgloss.NewStyle()
	if theme == "terminal" {
		return palette{base.Bold(true), base.Bold(true), base, base, base, base.Reverse(true), base.Bold(true)}
	}
	cyan, violet, text, muted, frame, dark, alert := "#67E8F9", "#C4A9FF", "#E2E8F0", "#9BAEC9", "#445978", "#23354C", "#FACC74"
	if theme == "light" {
		cyan, violet, text, muted, frame, dark, alert = "#04637C", "#6445A5", "#162536", "#4D5C6E", "#697A8E", "#D7ECF3", "#8A4710"
	}
	return palette{
		base.Foreground(lipgloss.Color(cyan)).Bold(true),
		base.Foreground(lipgloss.Color(violet)).Bold(true),
		base.Foreground(lipgloss.Color(text)),
		base.Foreground(lipgloss.Color(muted)),
		base.Foreground(lipgloss.Color(frame)),
		base.Foreground(lipgloss.Color(cyan)).Background(lipgloss.Color(dark)).Bold(true),
		base.Foreground(lipgloss.Color(alert)).Bold(true),
	}
}

func inputStyles(p palette) textinput.Styles {
	focused := textinput.StyleState{Text: p.text, Placeholder: p.muted, Prompt: p.accent}
	return textinput.Styles{Focused: focused, Blurred: focused, Cursor: textinput.CursorStyle{Blink: false}}
}

const minWidth, minHeight = 48, 18

func (m Model) hasRoom() bool { return m.width >= minWidth && m.height >= minHeight }

func (m Model) bounds() (int, int) {
	if m.narrow {
		return max(1, min(m.width, 58)-4), min(m.height, minHeight)
	}
	return max(1, min(m.width-4, 76)), m.height
}

func (m Model) choices() []Choice {
	switch m.screen {
	case scanning, executing, questionInput, fileInput, help:
		return nil
	case home, more, gallery, taskQuestion, reviewPlan:
		return m.page.Choices
	case components:
		return append(m.componentChoices(), m.evidenceChoice(), m.backChoice())
	case detail:
		return m.detailChoices()
	case resultOptions:
		return m.additionalResultChoices()
	}
	if (m.screen == results || m.screen == outcome) && m.page.Summary != nil {
		return m.resultChoices()
	}
	choices := slices.Clone(m.page.Choices)
	if m.page.Snapshot != nil {
		choices = append(choices, m.saveChoice())
	}
	if len(m.page.Findings) > 0 || len(m.page.Evidence) > 0 {
		choices = append(choices, detailsChoice(m.options.Language))
	}
	return append(choices, m.backChoice())
}

func (m Model) bodyRoom(h, options int) int {
	return max(1, h-7-min(min(options, 8), max(1, h-8)))
}

func (m Model) reviewReady() bool {
	w, h := m.bounds()
	return m.hasRoom() && (len(m.body(w)) <= m.bodyRoom(h, 2) || m.reviewThrough >= len(m.body(w)))
}

func (m *Model) recordReview() {
	if m.screen != reviewPlan || !m.hasRoom() {
		return
	}
	w, h := m.bounds()
	if m.scroll <= m.reviewThrough {
		m.reviewThrough = max(m.reviewThrough, min(len(m.body(w)), m.scroll+m.bodyRoom(h, 2)))
	}
}

func (m Model) body(w int) []string {
	var lines []string
	l := translator(m.options.Language)
	text := func(s string, style lipgloss.Style) {
		for _, line := range strings.Split(ansi.Wrap(m.visualText(s), w, ""), "\n") {
			for _, part := range strings.Split(ansi.Hardwrap(line, w, false), "\n") {
				lines = append(lines, style.Render(part))
			}
		}
	}
	title := func(s string) { text(s, m.palette.accent); lines = append(lines, "") }
	normal := func(s string) { text(s, m.palette.text) }
	echoQuestion := func() {
		if m.question != "" {
			label := l("Pertanyaan", "Question") + " · " + m.question
			text(ansi.Truncate(label, w, "…"), m.palette.muted)
		}
	}
	switch m.screen {
	case home:
		title(l("Mau dibantu apa?", "What can I help with?"))
		normal(l("Pahami kondisi PC, cari tanda masalah, dan tinjau perawatan.", "Understand your PC, look for signs of problems, and review maintenance."))
		if m.lastChecked.IsZero() {
			text(l("Belum diperiksa", "No checks yet"), m.palette.muted)
		} else {
			text(l("Pemeriksaan terakhir", "Last check")+" · "+m.lastChecked.Local().Format("02 Jan 15:04")+" · "+m.lastCoverage, m.palette.muted)
			text(m.lastConclusion, m.palette.text)
		}
		if m.options.Privilege != nil {
			if st := m.options.Privilege.State(); st.Requested && !st.Active {
				text(privilegeReason(st, m.options.Language), m.palette.alert)
			}
		}
	case more:
		title(l("Apa yang ingin dibuka?", "What would you like to open?"))
	case components:
		title(l("Pilih komponen", "Choose a component"))
	case resultOptions:
		title(l("Pilihan untuk hasil ini", "Options for these results"))
	case questionInput:
		title(l("Apa yang ingin kamu tanyakan?", "What would you like to ask?"))
		text(m.input.View(), m.palette.text)
		text(l("Coba “cek SSD” atau “kenapa PC terasa lambat?”", "Try “check SSD” or “why is my PC slow?”"), m.palette.muted)
	case fileInput:
		switch m.exportType {
		case "read":
			title(l("File mana yang ingin dibaca?", "Which file should be read?"))
		case "gpu":
			title(m.parameter.Label)
		case "write":
			title(l("Direktori mana untuk file sementara?", "Which directory should hold the temporary file?"))
		default:
			title(l("Simpan ke file mana?", "Where should I save the file?"))
		}
		text(m.input.View(), m.palette.text)
	case scanning, executing:
		label := m.workName
		if label == "" {
			label = displayStage(m.activity, m.options.Language)
		}
		if m.options.Motion == motion.Full && !m.cancelling {
			label = m.spinner.View() + " " + label
		}
		title(label)
		if m.screen == scanning {
			echoQuestion()
		}
	case help:
		title(l("Bantuan keyboard", "Keyboard help"))
		for _, line := range []string{l("↑ / ↓  Pilih atau gulir", "↑ / ↓  Select or scroll"), l("Enter  Buka pilihan", "Enter  Open selection"), l("Esc  Kembali atau minta penghentian", "Esc  Back or request cancellation"), l("/  Buka halaman pertanyaan", "/  Open question entry")} {
			normal(line)
		}
	case detail:
		title(m.detailTitle())
		if m.stalePage() {
			text(l("Data kedaluwarsa", "Stale data"), m.palette.alert)
		}
		if m.component != "" {
			for _, line := range m.componentEvidence() {
				normal(line)
			}
			break
		}
		if s := m.page.Summary; s != nil && !s.At.IsZero() {
			normal(l("Sampel", "Sample") + " · " + s.At.Format("2006-01-02 15:04:05 MST"))
		}
		for _, check := range m.page.Coverage {
			status := statusNames[check.Status].Text(m.options.Language)
			if status == "" {
				status = l("Belum dijalankan", "Not run")
			}
			normal(checkNames[check.ID].Text(m.options.Language) + " · " + status)
		}
		if len(m.page.Coverage) > 0 {
			lines = append(lines, "")
		}
		for _, line := range distinctEvidence(slices.Concat(m.page.Found, findingEvidence(m.page.Findings, m.options.Language), m.page.Evidence, m.page.Meaning, m.page.Advice)) {
			normal(line)
		}
	default:
		if (m.screen == results || m.screen == outcome) && m.page.Summary != nil {
			_, h := m.bounds()
			return m.summaryBody(*m.page.Summary, w, m.bodyRoom(h, len(m.choices())))
		}
		if len(m.page.Found) > 0 {
			title(m.page.Found[0])
		}
		if m.stalePage() {
			text(l("Data kedaluwarsa", "Stale data"), m.palette.alert)
		}
		for _, group := range [][]string{m.page.Found[min(1, len(m.page.Found)):], m.page.Meaning, m.page.Advice} {
			for _, s := range group {
				normal(s)
			}
		}
		if m.screen == results || m.screen == taskQuestion {
			echoQuestion()
		}
	}
	if m.screen == gallery && !m.narrow && w >= 64 && m.height >= 28 {
		lines = append(lines, "")
		for _, row := range mascot.Render(m.visual.pose, m.visual.frame, m.options.ASCII) {
			lines = append(lines, m.palette.muted.Render(row))
		}
	}
	return lines
}

func (m Model) maxScroll() int {
	w, h := m.bounds()
	return max(0, len(m.body(w))-m.bodyRoom(h, len(m.choices())))
}

func (m Model) View() tea.View {
	w, h := m.bounds()
	if !m.hasRoom() {
		hint := fmt.Sprintf("%s (%dx%d)", m.tr(copy.fallback), minWidth, minHeight)
		controls := m.tr(Words{"Esc Kembali  /  q Keluar", "Esc Back  /  q Quit"})
		if ansi.StringWidth(controls) > m.width {
			controls = "Esc / q"
		}
		rows := []string{ansi.Truncate(hint, max(1, m.width), ""), ansi.Truncate(controls, max(1, m.width), "")}
		v := tea.NewView(strings.Join(rows[:min(len(rows), max(h, 1))], "\n"))
		v.AltScreen = true
		return v
	}
	pad := strings.Repeat(" ", max(0, (m.width-w)/2))
	context := m.tr(menuNames[m.active])
	label := "whwmpc"
	if m.options.Demo {
		label += "  /  DEMO"
	}
	if m.criticalSSD || m.criticalDisk {
		label += "  /  " + m.tr(Words{"KRITIS", "CRITICAL"})
	}
	if context != "" {
		label += "  /  " + context
	}
	if m.options.Privilege != nil && !m.options.Gallery && (m.accessChosen || m.options.Privilege.State().Requested) {
		if m.options.Privilege.State().Active {
			label += "  /  " + m.tr(Words{"Mendalam", "Deep"})
		} else {
			label += "  /  " + m.tr(Words{"Biasa", "Ordinary"})
		}
	}
	header := m.palette.secondary.Render(label)
	const mascotGap = 2
	var art []string
	if !m.narrow && m.screen != gallery && h >= 28 && m.width-w-len(pad) >= mascot.Width+mascotGap {
		art = mascot.Render(m.visual.pose, m.visual.frame, m.options.ASCII)
	} else {
		face := mascot.Face(m.visual.pose, m.visual.frame, m.options.ASCII)
		if m.canAnimate() && ansi.StringWidth(label)+mascot.CompactWidth+8 <= w {
			face = mascot.Compact(m.visual.pose, m.visual.frame, m.options.ASCII)
		}
		header += "   " + m.palette.muted.Render(face+" Piko")
	}
	separator := ""
	if left := m.visual.panelUntil.Sub(m.now()); left > 0 && m.canAnimate() && m.screen != reviewPlan {
		width := 3
		switch {
		case left <= motion.PanelDuration/3:
			width = 6
		case left <= 2*motion.PanelDuration/3:
			width = 5
		}
		mark := "─"
		if m.options.ASCII {
			mark = "-"
		}
		separator = m.palette.frame.Render(strings.Repeat(mark, width))
	}
	rows := []string{header, separator}
	body := m.body(w)
	choices := m.choices()

	optionRows := min(min(len(choices), 8), max(1, h-8))
	room := m.bodyRoom(h, len(choices))
	start := min(m.scroll, max(0, len(body)-room))
	rows = append(rows, body[start:min(len(body), start+room)]...)
	if len(body) > room {
		rows = append(rows, m.palette.muted.Render(fmt.Sprintf("%d–%d / %d", start+1, min(len(body), start+room), len(body))))
	}
	if len(choices) > 0 {
		rows = append(rows, "")
		first := max(0, m.choice-optionRows+1)
		for i := first; i < min(len(choices), first+optionRows); i++ {
			c := choices[i]
			mark, style := "  ", m.palette.text
			if i == m.choice {
				mark, style = "› ", m.palette.selected
				if m.options.ASCII {
					mark = "> "
				}
				if m.canAnimate() && m.screen != reviewPlan && !m.visual.focusUntil.IsZero() {
					mark = "» "
					if m.options.ASCII {
						mark = "+ "
					}
				}
			}
			label := c.Label
			if m.screen == home && h >= 20 && i < 3 {
				desc := []Words{{"Kondisi komponen dan hal yang perlu ditangani", "Components and what needs attention"},
					{"Amati saat kelambatan terjadi", "Observe while the slowdown happens"}, {"Tinjau ruang yang bisa dibersihkan", "Review space you can reclaim"}}
				label += "  ·  " + m.tr(desc[i])
			}
			if m.screen == reviewPlan && c.Kind == "apply" {
				switch {
				case m.demoCleanupAllowed(m.page.Plan):
					label = m.tr(Words{"Simulasikan pembersihan · DEMO", "Simulate cleanup · DEMO"})
					if !m.reviewReady() {
						label += " · " + m.tr(Words{"baca dulu", "review first"})
					}
				case m.options.Demo:
					label += " · DEMO"
				case !m.reviewReady():
					label += " · " + m.tr(Words{"baca dulu", "review first"})
				}
			}
			rows = append(rows, style.Render(ansi.Truncate(mark+label, w, "…")))
		}
	}
	footer := m.tr(Words{"↑↓ Pilih  ·  Enter Buka  ·  Esc Kembali  ·  ? Bantuan", "↑↓ Select  ·  Enter Open  ·  Esc Back  ·  ? Help"})
	switch m.screen {
	case home:
		footer = m.tr(Words{"↑↓ Pilih  ·  Enter Pilih tujuan  ·  / Tanya  ·  q Keluar", "↑↓ Select  ·  Enter Choose a goal  ·  / Ask  ·  q Quit"})
	case taskQuestion:
		if m.access != nil && m.access.purpose == firstReads {
			footer = m.tr(Words{"↑↓ Pilih akses  ·  Enter Mulai  ·  Esc Kembali", "↑↓ Select access  ·  Enter Start  ·  Esc Back"})
		}
	case reviewPlan:
		switch {
		case m.options.Demo && !m.demoCleanupAllowed(m.page.Plan):
			footer = m.tr(Words{"DEMO · Aksi terkunci · Esc Kembali", "DEMO · Actions locked · Esc Back"})
		case !m.reviewReady():
			footer = m.tr(Words{"↓ Baca seluruh rencana · Esc Kembali", "↓ Read the full plan · Esc Back"})
		default:
			footer = m.tr(Words{"↑↓ Pilih · Enter Lanjut · Esc Kembali", "↑↓ Select · Enter Continue · Esc Back"})
		}
	case scanning, executing:
		footer = m.tr(Words{"Esc Minta penghentian", "Esc Request cancellation"})
	case questionInput, fileInput:
		footer = m.tr(Words{"Enter Lanjut  ·  Esc Kembali", "Enter Continue  ·  Esc Back"})
	case detail:
		footer = m.tr(Words{"↑↓ Pilih · PgUp/PgDn Gulir · Esc Kembali", "↑↓ Select · PgUp/PgDn Scroll · Esc Back"})
	case help:
		footer = m.tr(Words{"↑↓ Gulir  ·  Esc Kembali", "↑↓ Scroll  ·  Esc Back"})
	}
	if m.options.ASCII {
		footer = strings.NewReplacer("↑↓", "Up/Down", "↓", "Down", "·", "|").Replace(footer)
	}
	if ansi.StringWidth(footer) > w && m.screen != reviewPlan {
		switch m.screen {
		case detail:
			footer = m.tr(Words{"PgUp/PgDn Gulir / Esc Kembali", "PgUp/PgDn Scroll / Esc Back"})
		case home:
			footer = m.tr(Words{"Enter Buka  /  ? Bantuan  /  q Keluar", "Enter Open  /  ? Help  /  q Quit"})
		default:
			footer = m.tr(Words{"Enter Buka  /  Esc Kembali", "Enter Open  /  Esc Back"})
		}
	}
	if m.toast != "" {
		rows = append(rows, m.palette.secondary.Render(m.toast))
	}
	rows = append(rows, "", m.palette.muted.Render(footer))
	for len(rows) < len(art) {
		rows = append(rows, "")
	}
	for i, line := range rows {
		line = ansi.Truncate(line, w, "")
		if i < len(art) {
			line += strings.Repeat(" ", w+mascotGap-ansi.StringWidth(line)) + m.palette.muted.Render(art[i])
		}
		rows[i] = pad + line
	}
	v := tea.NewView(strings.Join(rows[:min(h, len(rows))], "\n"))
	v.AltScreen = true
	if m.typing() && h > 4 {
		if c := m.input.Cursor(); c != nil {
			c.X = min(c.X, w-1) + len(pad)
			c.Y = 2 + len(body) - 1 - start
			v.Cursor = c
		}
	}
	return v
}

func overviewPage(o domain.Overview, lang string) Page {
	l := func(id, en string) string { return (Words{id, en}).Text(lang) }
	status := func(a domain.Availability) string { return statusNames[a].Text(lang) }
	page := Page{Availability: o.Availability, Summary: &ResultSummary{At: o.CapturedAt, Metrics: overviewMetrics(o, lang)}}
	if o.CapturedAt.IsZero() {
		return page
	}
	context := o.Distro
	if context == "" {
		context = o.OS
	}
	if o.Environment != "" && o.Environment != "undetermined" {
		context += " / " + strings.ReplaceAll(o.Environment, "_", " ")
	}
	if o.LogicalCPUs > 0 {
		context += fmt.Sprintf(" · %d %s", o.LogicalCPUs, l("CPU logis", "logical CPUs"))
	}
	page.Found = []string{context}
	if cpu, valid := o.CPU.Latest(); valid {
		label := "CPU"
		if o.CPU.Scope == domain.PrimaryProcessorGroup {
			label = l("CPU grup primer", "Primary group CPU")
		}
		page.Found = append(page.Found, fmt.Sprintf("%s %.0f%% · %d %s", label, cpu, len(o.CPU.Samples), l("sampel", "samples")))
	}
	page.Meaning = []string{l("Ini cuplikan singkat. Beban bisa berubah saat tugas berjalan.", "This is a short sample. Load may change as tasks run.")}
	page.Advice = []string{l("Pilih diagnosis lambat, SSD, TRIM, atau daya untuk pemeriksaan lebih dekat.", "Choose a slowdown, SSD, TRIM or power check for more detail.")}
	page.Evidence = []string{
		fmt.Sprintf("%s: %s", l("Kernel", "Kernel"), o.Kernel),
		fmt.Sprintf("%s: %s", l("CPU", "CPU"), o.CPUModel),
		fmt.Sprintf("%s: %s", copy.memory.Text(lang), status(o.Memory.Availability)),
		fmt.Sprintf("%s: %s", l("Suhu", "Temperature"), status(o.Temperature.Availability)),
	}
	if o.Memory.SwapStatus == domain.Available {
		page.Evidence = append(page.Evidence, fmt.Sprintf("Swap · %s / %s", bytesText(o.Memory.SwapUsed), bytesText(o.Memory.SwapBytes)))
	}
	if o.UptimeSeconds != nil {
		up := (time.Duration(*o.UptimeSeconds) * time.Second).Truncate(time.Minute)
		page.Evidence = append(page.Evidence, copy.uptime.Text(lang)+": "+up.String())
	}
	for _, disk := range o.Disks {
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · %s · %s", disk.Mount, disk.FSType, percent(disk.UsedPercent, disk.Availability == domain.Available)))
	}
	page.Evidence = append(page.Evidence, processEvidence(o, lang)...)
	return page
}

func processEvidence(o domain.Overview, lang string) []string {
	processCPU := "CPU"
	if o.CPU.Scope == domain.PrimaryProcessorGroup {
		processCPU = (Words{"CPU host", "host CPU"}).Text(lang)
	}
	var lines []string
	for _, process := range o.Processes {
		lines = append(lines, fmt.Sprintf("%s · %s %.1f%% · RSS %s", process.Name, processCPU, process.CPU, bytesText(process.RSS)))
	}
	return lines
}

func percent(value float64, known bool) string {
	if !known {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", value)
}

func bytesText(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

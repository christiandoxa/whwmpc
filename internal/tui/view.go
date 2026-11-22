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

func (m Model) bounds() (int, int) {
	if m.narrow {
		return min(m.width, 58) - 4, min(m.height, 18)
	}
	return max(1, min(m.width-4, 76)), m.height
}

func (m Model) choices() []Choice {
	switch m.screen {
	case scanning, executing, questionInput, fileInput, detail, help:
		return nil
	case home, more, gallery, taskQuestion, reviewPlan:
		return m.page.Choices
	}
	if m.screen == results && m.page.Summary != nil {
		primary := m.page.Summary.Primary
		choices := []Choice{primary}
		if primary.Kind == "details" && m.page.Snapshot != nil {
			choices = append(choices, Choice{m.tr(Words{"Simpan hasil ini", "Save these results"}), "save-snapshot", ""})
		}
		if primary.Kind != "details" {
			choices = append(choices, Choice{m.tr(Words{"Lihat semua hasil", "View all results"}), "details", ""})
		}
		return append(choices, Choice{m.tr(Words{"Kembali", "Back"}), "back", ""})
	}
	choices := slices.Clone(m.page.Choices)
	if m.page.Snapshot != nil {
		choices = append(choices, Choice{m.tr(Words{"Simpan hasil ini", "Save these results"}), "save-snapshot", ""})
	}
	if len(m.page.Findings) > 0 || len(m.page.Evidence) > 0 {
		choices = append(choices, Choice{m.tr(Words{"Lihat semua hasil", "View all results"}), "details", ""})
	}
	return append(choices, Choice{m.tr(Words{"Kembali", "Back"}), "back", ""})
}

func (m Model) bodyRoom(h, options int) int {
	return max(1, h-7-min(min(options, 8), max(1, h-8)))
}

func (m Model) reviewReady() bool {
	w, h := m.bounds()
	return w >= 32 && h >= 12 && (len(m.body(w)) <= m.bodyRoom(h, 2) || m.reviewThrough >= len(m.body(w)))
}

func (m *Model) recordReview() {
	if m.screen != reviewPlan {
		return
	}
	w, h := m.bounds()
	if w >= 32 && h >= 12 && m.scroll <= m.reviewThrough {
		m.reviewThrough = max(m.reviewThrough, min(len(m.body(w)), m.scroll+m.bodyRoom(h, 2)))
	}
}

func (m Model) body(w int) []string {
	var lines []string
	l := translator(m.options.Language)
	text := func(s string, style lipgloss.Style) {
		for _, line := range strings.Split(ansi.Wrap(domain.SafeText(s), w, ""), "\n") {
			for _, part := range strings.Split(ansi.Hardwrap(line, w, false), "\n") {
				lines = append(lines, style.Render(part))
			}
		}
	}
	title := func(s string) { text(s, m.palette.accent); lines = append(lines, "") }
	normal := func(s string) { text(s, m.palette.text) }
	switch m.screen {
	case home:
		title(l("Mau dibantu apa?", "What can I help with?"))
		if m.lastChecked.IsZero() {
			text(l("Belum diperiksa", "No checks yet"), m.palette.muted)
		} else {
			text(l("Pemeriksaan terakhir", "Last check")+" · "+m.lastChecked.Local().Format("02 Jan 15:04")+" · "+m.lastCoverage, m.palette.muted)
		}
	case more:
		title(l("Apa yang ingin dibuka?", "What would you like to open?"))
	case questionInput:
		title(l("Apa yang ingin kamu tanyakan?", "What would you like to ask?"))
		text(m.input.View(), m.palette.text)
	case fileInput:
		switch m.exportType {
		case "read":
			title(l("File mana yang ingin dibaca?", "Which file should be read?"))
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
	case help:
		title(l("Bantuan keyboard", "Keyboard help"))
		for _, line := range []string{l("↑ / ↓  Pilih atau gulir", "↑ / ↓  Select or scroll"), l("Enter  Buka pilihan", "Enter  Open selection"), l("Esc  Kembali atau minta penghentian", "Esc  Back or request cancellation"), l("/  Buka halaman pertanyaan", "/  Open question entry")} {
			normal(line)
		}
	case detail:
		title(l("Semua hasil", "All results"))
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
		for _, s := range m.page.Evidence {
			normal(s)
		}
	default:
		if m.screen == results && m.page.Summary != nil {
			s := m.page.Summary
			title(s.Headline)
			text(s.Coverage, m.palette.secondary)
			if m.stalePage() {
				text(l("Data kedaluwarsa", "Stale data"), m.palette.alert)
			}
			for _, line := range s.Highlights {
				normal("· " + line)
			}
			return lines
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
	}
	return lines
}

func (m Model) maxScroll() int {
	w, h := m.bounds()
	return max(0, len(m.body(w))-m.bodyRoom(h, len(m.choices())))
}

func (m Model) View() tea.View {
	w, h := m.bounds()
	if w < 16 || h < 7 {
		v := tea.NewView(ansi.Truncate(m.tr(copy.fallback), max(1, m.width), ""))
		v.AltScreen = true
		return v
	}
	pad := strings.Repeat(" ", max(0, (m.width-w)/2))
	context := m.tr(menuNames[m.active])
	label := "whwmpc"
	if m.options.Demo {
		label += "  /  DEMO"
	}
	if context != "" {
		label += "  /  " + context
	}
	if m.critical {
		label += "  /  " + m.tr(Words{"KRITIS", "CRITICAL"})
	}
	face := mascot.Face(m.visual.pose, m.visual.frame, m.options.ASCII)
	rows := []string{m.palette.secondary.Render(label) + "   " + m.palette.muted.Render(face+" Piko"), ""}
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
			}
			label := c.Label
			if m.screen == home && h >= 20 && i < 3 {
				desc := []Words{{"Ringkasan kondisi dan saran", "Condition and next steps"},
					{"Cari tanda penyebabnya", "Look for likely causes"}, {"Tinjau yang bisa dibersihkan", "Review what can be cleaned"}}
				label += "  ·  " + m.tr(desc[i])
			}
			if m.screen == reviewPlan && c.Kind == "apply" {
				switch {
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
		footer = m.tr(Words{"↑↓ Pilih  ·  Enter Buka  ·  / Tanya  ·  ? Bantuan", "↑↓ Select  ·  Enter Open  ·  / Ask  ·  ? Help"})
	case reviewPlan:
		switch {
		case m.options.Demo:
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
		footer = m.tr(Words{"↑↓ Gulir  ·  Enter Pilihan lanjutan  ·  Esc Kembali", "↑↓ Scroll  ·  Enter Next steps  ·  Esc Back"})
	case help:
		footer = m.tr(Words{"↑↓ Gulir  ·  Esc Kembali", "↑↓ Scroll  ·  Esc Back"})
	}
	if m.options.ASCII {
		footer = strings.NewReplacer("↑↓", "Up/Down", "·", "|").Replace(footer)
	}
	if m.toast != "" {
		rows = append(rows, m.palette.secondary.Render(m.toast))
	}
	rows = append(rows, "", m.palette.muted.Render(footer))
	for i, line := range rows {
		rows[i] = pad + ansi.Truncate(line, w, "")
	}
	v := tea.NewView(strings.Join(rows[:min(h, len(rows))], "\n"))
	v.AltScreen = true
	if m.typing() && h > 4 {
		if c := m.input.Cursor(); c != nil {
			c.X = min(c.X, w-1) + len(pad)
			c.Y = 4
			v.Cursor = c
		}
	}
	return v
}

func overviewPage(o domain.Overview, lang string) Page {
	l := func(id, en string) string { return (Words{id, en}).Text(lang) }
	status := func(a domain.Availability) string { return statusNames[a].Text(lang) }
	page := Page{Availability: o.Availability}
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
	page.Found = []string{fmt.Sprintf("%s · %d %s", context, o.LogicalCPUs, l("CPU logis", "logical CPUs"))}
	if cpu, valid := o.CPU.Latest(); valid {
		page.Found = append(page.Found, fmt.Sprintf("CPU %.0f%% · %d %s", cpu, len(o.CPU.Samples), l("sampel", "samples")))
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
	for _, process := range o.Processes {
		page.Evidence = append(page.Evidence, fmt.Sprintf("%s · CPU %.1f%% · RSS %s", process.Name, process.CPU, bytesText(process.RSS)))
	}
	return page
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

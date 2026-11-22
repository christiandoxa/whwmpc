package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/christiandoxa/whwmpc/internal/assistant"
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

func (m Model) bounds() (w, h int) {
	w, h = min(138, max(1, m.width)), min(38, max(1, m.height))
	if m.narrow {
		return min(w, 60), min(h, 18)
	}
	return w, h
}

func (m Model) split() (nav, session, evidence int) {
	w, h := m.bounds()
	w -= 2
	if w < 74 || h < 22 {
		return 0, max(1, w), 0
	}
	nav = 26
	if w >= 102 {
		evidence = min(32, w/4)
	}
	session = w - nav - evidence - 1
	if evidence > 0 {
		session--
	}
	return nav, session, evidence
}

func (m Model) sessionWidth() int {
	_, session, _ := m.split()
	return session
}

func (m Model) panelHeight() int {
	_, h := m.bounds()
	if nav, _, _ := m.split(); nav == 0 {
		return max(4, h-9)
	}
	return h - 12
}

func (m Model) sessionContentHeight() int { return max(1, m.panelHeight()-4) }

func (m Model) View() tea.View {
	w, h := m.bounds()
	var lines []string
	if w < 43 || h < 14 {
		lines = []string{"What Happens With My PC", "Piko " + mascot.Face(m.visual.pose, m.visual.frame, m.options.ASCII), m.tr(copy.fallback), "q / Ctrl+C"}
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], w, "")
		}
		v := tea.NewView(strings.Join(lines[:min(h, len(lines))], "\n"))
		v.AltScreen = true
		return v
	}
	w -= 2
	header := m.header(w)
	lines = append(lines, header...)
	lines = append(lines, m.palette.frame.Render(strings.Repeat(m.symbol("─", "-"), w)))
	navW, sessionW, evW := m.split()
	if navW == 0 {
		lines = append(lines, m.palette.muted.Render(ansi.Truncate(m.tr(copy.fallback), w, "")))
		if m.overview.CapturedAt.IsZero() {
			lines = append(lines, m.palette.muted.Render(m.tr(copy.waiting)))
		} else {
			current, _ := m.overview.CPU.Latest()
			cpu := m.tr(copy.missing)
			if _, ok := m.overview.CPU.Latest(); ok {
				cpu = fmt.Sprintf("%.0f%%", current)
			}
			lines = append(lines, m.palette.accent.Render("CPU "+cpu)+"  "+m.palette.muted.Render(m.tr(menuNames[assistant.Menu[m.selected]])))
		}
	} else {
		lines = append(lines, strings.Split(m.cards(w), "\n")...)
	}
	lines = append(lines, "")
	parts := []string{m.sessionPanel(sessionW)}
	if navW > 0 {
		parts = append([]string{m.navPanel(navW), " "}, parts...)
	}
	if evW > 0 {
		parts = append(parts, " ", m.evidencePanel(evW))
	}
	lines = append(lines, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, parts...), "\n")...)
	lines = append(lines, "")
	y := len(lines)
	lines = append(lines, m.prompt(w), m.footer(w))
	for i, line := range lines {
		lines[i] = " " + ansi.Truncate(line, w, "")
	}
	v := tea.NewView(strings.Join(lines[:min(h, len(lines))], "\n"))
	v.AltScreen = true
	if m.focus == inputFocus && y < h {
		if c := m.input.Cursor(); c != nil {
			c.Y = y
			c.X = min(c.X+ansi.StringWidth(m.tr(copy.ask))+6, w)
			v.Cursor = c
		}
	}
	return v
}

func (m Model) symbol(unicode, ascii string) string {
	if m.options.ASCII {
		return ascii
	}
	return unicode
}

func (m Model) header(w int) []string {
	titleStyle := m.palette.accent
	if m.visual.pose == mascot.Greeting && m.visual.frame < 2 && m.options.Motion == motion.Full {
		titleStyle = m.palette.secondary
	}
	title := titleStyle.Render("WHAT HAPPENS WITH MY PC")
	host := m.overview.Distro
	if host == "" {
		host = m.overview.OS
	}
	if m.overview.Arch != "" {
		host = strings.TrimSpace(host + " / " + m.overview.Arch)
	}
	status := m.palette.secondary.Render(m.tr(copy.ready))
	if m.busy {
		status = m.palette.secondary.Render(m.tr(copy.scanning))
		if m.options.Motion == motion.Full {
			status = m.spinner.View() + " " + status
		}
	}
	if !m.overview.CapturedAt.IsZero() {
		status += "  " + m.palette.muted.Render(m.overview.CapturedAt.Format("15:04:05"))
	}
	if m.critical {
		status = m.palette.alert.Render(m.tr(Words{"KRITIS", "CRITICAL"})) + "  " + status
	}
	sub := m.palette.muted.Render(m.tr(copy.tagline))
	if m.options.Demo {
		sub = m.palette.secondary.Render(m.tr(copy.demo))
	}
	if _, _, evidence := m.split(); evidence == 0 {
		status = m.palette.accent.Render(mascot.Face(m.visual.pose, m.visual.frame, m.options.ASCII)+" Piko") + "   " + status
	}
	return []string{place(title, m.palette.muted.Render(host), w), place(sub, status, w)}
}

func place(left, right string, width int) string {
	right = ansi.Truncate(right, max(0, width-ansi.StringWidth(left)-2), "")
	spaces := max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right))
	return left + strings.Repeat(" ", spaces) + right
}

func (m Model) diskRoot() (domain.Disk, bool) {
	for _, disk := range m.overview.Disks {
		if disk.Root && disk.Availability == domain.Available {
			return disk, true
		}
	}
	return domain.Disk{}, false
}

func (m Model) cards(w int) string {
	width := (w - 3) / 4
	cpu, cpuOK := m.overview.CPU.Latest()
	mem, memOK := m.overview.Memory.Use.Latest()
	disk, diskOK := m.diskRoot()
	temp := m.tr(copy.missing)
	if m.overview.Temperature.Celsius != nil {
		temp = fmt.Sprintf("%.0f %s", *m.overview.Temperature.Celsius, m.symbol("°C", "C"))
	}
	cpuTrail := m.spark(m.overview.CPU.Samples)
	memTrail := m.tr(copy.missing)
	if m.overview.Memory.Availability == domain.Available {
		memTrail = m.tr(Words{"total", "total"}) + " " + bytesText(m.overview.Memory.TotalBytes)
	}
	diskTrail := m.tr(copy.missing)
	if diskOK {
		diskTrail = fmt.Sprintf("%s %s", bytesText(disk.FreeBytes), m.tr(Words{"bebas", "free"}))
	}
	labels := []string{"CPU", m.tr(copy.memory), m.tr(copy.disk), m.tr(copy.temp)}
	values := []string{percent(cpu, cpuOK), percent(mem, memOK), percent(disk.UsedPercent, diskOK), temp}
	trails := []string{cpuTrail, memTrail, diskTrail, m.powerText()}
	fills := []string{m.meter(cpu, cpuOK, 7), m.meter(mem, memOK, 7), m.meter(disk.UsedPercent, diskOK, 7), ""}
	var parts []string
	for i := range labels {
		if i > 0 {
			parts = append(parts, " ")
		}
		inner := width - 4
		value := m.palette.text.Bold(true).Render(values[i])
		if fills[i] != "" {
			value = place(value, fills[i], inner)
		}
		parts = append(parts, m.box(width, 5, labels[i], []string{value, m.palette.muted.Render(trails[i])}, false))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m Model) meter(v float64, available bool, width int) string {
	if !available {
		return ""
	}
	bar := m.meterModel
	bar.SetWidth(width)
	return bar.ViewAs(v / 100)
}

func (m Model) spark(samples []float64) string {
	if len(samples) == 0 {
		return m.tr(copy.missing)
	}
	shape := []rune(m.symbol("▁▂▃▄▅▆▇█", "._-=+*#@"))
	var out strings.Builder
	for _, value := range samples[max(0, len(samples)-10):] {
		out.WriteRune(shape[int(math.Max(0, math.Min(value/100*float64(len(shape)), float64(len(shape)-1))))])
	}
	return m.palette.secondary.Render(out.String())
}

func (m Model) powerText() string {
	switch {
	case m.overview.OnAC != nil && *m.overview.OnAC:
		if m.overview.Battery != nil {
			return fmt.Sprintf("AC · %.0f%%", *m.overview.Battery)
		}
		return m.tr(copy.connected)
	case m.overview.Battery != nil:
		return fmt.Sprintf("%s %.0f%%", m.tr(copy.battery), *m.overview.Battery)
	default:
		return m.tr(copy.powerUnknown)
	}
}

func (m Model) box(width, height int, title string, lines []string, focus bool) string {
	border := m.palette.frame
	head := m.palette.secondary
	if focus {
		border, head = m.palette.accent, m.palette.accent
	}
	if title == m.tr(copy.session) && !m.visual.panelUntil.IsZero() {
		border, head = m.palette.secondary, m.palette.secondary
	}
	tl, tr := m.symbol("╭", "+"), m.symbol("╮", "+")
	bl, br := m.symbol("╰", "+"), m.symbol("╯", "+")
	h, v := m.symbol("─", "-"), border.Render(m.symbol("│", "|"))
	out := []string{border.Render(tl + strings.Repeat(h, width-2) + tr)}
	rows := append([]string{head.Render(title)}, lines...)
	for i := range height - 2 {
		var text string
		if i < len(rows) {
			text = ansi.Truncate(rows[i], width-4, "")
		}
		out = append(out, v+" "+text+strings.Repeat(" ", max(0, width-3-ansi.StringWidth(text)))+v)
	}
	return strings.Join(append(out, border.Render(bl+strings.Repeat(h, width-2)+br)), "\n")
}

func (m Model) navPanel(width int) string {
	var lines []string
	for i, intent := range assistant.Menu {
		name := m.tr(menuNames[intent])
		line := "  " + name
		if m.selected == i {
			style := m.palette.selected
			if !m.visual.focusUntil.IsZero() && m.focus == menuFocus {
				style = m.palette.secondary
			}
			line = style.Render(m.symbol("›", ">") + " " + name)
		} else if m.active == intent {
			line = m.palette.accent.Render(m.symbol("·", "*") + " " + name)
		}
		lines = append(lines, line)
	}
	return m.box(width, m.panelHeight(), m.tr(copy.menu), lines, m.focus == menuFocus)
}

func (m Model) sessionPanel(width int) string {
	height := m.panelHeight()
	all := m.sessionLines(width - 4)
	room := m.sessionContentHeight()
	at := min(m.scroll, max(0, len(all)-room))
	visible := append([]string(nil), all[at:min(at+room, len(all))]...)
	action := m.palette.muted.Render(m.tr(menuNames[m.active]))
	if len(m.page.Choices) > 0 && !m.help && !m.detail && !m.busy {
		choice := m.page.Choices[m.choice]
		action = m.palette.secondary.Render(fmt.Sprintf("%s %s %s", m.symbol("‹", "<"), choice.Label, m.symbol("›", ">")))
	} else if len(all) > room {
		action = m.palette.muted.Render(fmt.Sprintf("↕ %d–%d / %d", at+1, min(at+room, len(all)), len(all)))
	}
	for len(visible) < room {
		visible = append(visible, "")
	}
	visible = append(visible, action)
	return m.box(width, height, m.tr(copy.session), visible, m.focus == pageFocus || !m.visual.panelUntil.IsZero())
}

func (m Model) sessionLines(w int) []string {
	if w < 1 {
		return nil
	}
	var lines []string
	add := func(s string) {
		lines = append(lines, strings.Split(ansi.Hardwrap(ansi.Wrap(s, w, ""), w, false), "\n")...)
	}
	section := func(title Words, content []string) {
		if len(content) == 0 {
			return
		}
		add(m.palette.accent.Render(m.tr(title)))
		for _, entry := range content {
			add(m.palette.text.Render(domain.SafeText(entry)))
		}
	}
	switch {
	case m.help:
		section(copy.showHelp, []string{
			"↑/↓ atau j/k · " + m.tr(Words{"navigasi dan gulir", "navigate and scroll"}),
			"Tab / Shift+Tab · " + m.tr(Words{"pindah fokus", "change focus"}),
			"Enter · " + m.tr(Words{"buka atau lanjut", "open or continue"}),
			"/ · " + m.tr(copy.palette),
			"m · " + m.tr(Words{"atur mode gerak", "change motion mode"}),
			"d · " + m.tr(copy.details),
			"r · " + m.tr(Words{"periksa ulang", "scan again"}),
			"Esc · " + m.tr(Words{"kembali", "back"}),
			"q / Ctrl+C · " + m.tr(Words{"keluar", "quit"}),
		})
	case m.detail:
		entries := m.page.Evidence
		if len(entries) == 0 {
			entries = []string{m.tr(copy.needsData)}
		}
		section(copy.details, entries)
	case m.overview.CapturedAt.IsZero() && !m.options.Gallery:
		add(m.palette.text.Bold(true).Render(m.tr(copy.greeting)))
		add(m.palette.muted.Render(m.tr(copy.initial)))
		add(m.palette.secondary.Render(m.tr(copy.waiting)))
	default:
		if m.question != "" && !m.options.Gallery {
			add(m.palette.secondary.Render(m.tr(Words{"Kamu", "You"}) + " › " + domain.SafeText(m.question)))
		}
		if m.active == assistant.Overview && m.page.Availability != domain.Unknown && !m.options.Gallery {
			add(m.palette.text.Bold(true).Render(m.tr(copy.greeting)))
			add("")
		}
		if m.busy && m.page.Availability == "" {
			add(m.palette.secondary.Render(displayStage(m.activity, m.options.Language)))
		}
		if m.page.Priority != "" {
			add(m.palette.alert.Render(domain.SafeText(m.page.Priority)))
		}
		section(copy.found, m.page.Found)
		section(copy.meaning, m.page.Meaning)
		section(copy.advice, m.page.Advice)
		if len(m.page.Choices) > 0 {
			add(m.palette.muted.Render(m.tr(copy.choices) + " · " + m.tr(copy.choicesHint)))
		}
	}
	return lines
}

func (m Model) evidencePanel(width int) string {
	override := m.page.Evidence
	if len(override) == 0 {
		override = overviewPage(m.overview, m.options.Language).Evidence
	}
	name := m.palette.accent.Render("Piko")
	lines := []string{name}
	style := m.palette.secondary
	if m.visual.pose == mascot.Critical || m.visual.pose == mascot.Warning || m.visual.pose == mascot.Error {
		style = m.palette.alert
	}
	for _, line := range mascot.Render(m.visual.pose, m.visual.frame, m.options.ASCII) {
		lines = append(lines, "  "+style.Render(line))
	}
	lines = append(lines, "")
	for _, evidence := range override {
		lines = append(lines, strings.Split(ansi.Wrap(m.palette.muted.Render(domain.SafeText(evidence)), width-4, ""), "\n")...)
	}
	return m.box(width, m.panelHeight(), m.tr(copy.evidence), lines, false)
}

func (m Model) prompt(width int) string {
	tag := m.palette.muted
	if m.focus == inputFocus {
		tag = m.palette.accent
	}
	return ansi.Truncate(tag.Render(m.tr(copy.ask))+"  "+tag.Render(m.symbol("›", ">"))+"  "+m.input.View(), width, "")
}

func (m Model) footer(w int) string {
	var text string
	if m.toast != "" {
		return m.palette.secondary.Render(ansi.Truncate(m.toast, w, m.symbol("…", ".")))
	}
	if m.options.Gallery && m.focus != inputFocus {
		return m.palette.muted.Render(ansi.Truncate("←/→  Enter  Esc   m · "+string(m.options.Motion)+"   n · "+m.tr(Words{"ukuran ringkas", "narrow"})+"   q", w, ""))
	}
	switch m.focus {
	case inputFocus:
		text = m.tr(Words{"Enter kirim   Esc kembali   Tab fokus", "Enter send   Esc back   Tab focus"})
	case pageFocus:
		text = m.tr(Words{"↑↓ gulir   ←→ tindakan   Enter pilih   d bukti   Tab fokus   ?", "↑↓ scroll   ←→ actions   Enter select   d evidence   Tab focus   ?"})
	default:
		text = m.tr(Words{"↑↓/jk pilih   Enter buka   Tab fokus   / cari   d bukti   ? bantuan   q keluar", "↑↓/jk select   Enter open   Tab focus   / search   d evidence   ? help   q quit"})
	}
	if m.options.ASCII {
		text = strings.NewReplacer("↑↓", "up/down", "←→", "left/right").Replace(text)
	}
	return m.palette.muted.Render(ansi.Truncate(text, w, ""))
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

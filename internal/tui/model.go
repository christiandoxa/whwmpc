package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
)

type Options struct {
	Language, Theme      string
	Demo, ASCII, Gallery bool
	Motion               motion.Mode
	Scheduler            *motion.Scheduler
	SaveMotion           func(motion.Mode) error
	Workload             assistant.Workload
}

type Choice struct {
	Label string
	Kind  string
	Value string
}

type Page struct {
	Availability     domain.Availability
	Pose             mascot.Pose
	Findings         []diagnostics.Finding
	Priority         string
	Found, Meaning   []string
	Advice, Evidence []string
	Choices          []Choice
}

type Request struct {
	Intent   assistant.Intent
	Choice   Choice
	Overview domain.Overview
	Workload assistant.Workload
	Language string
}

type Inspect func(context.Context, Request) Page

type focus int

const (
	menuFocus focus = iota
	pageFocus
	inputFocus
)

type Model struct {
	ctx           context.Context
	source        platform.Provider
	inspect       Inspect
	options       Options
	palette       palette
	input         textinput.Model
	spinner       spinner.Model
	meterModel    progress.Model
	width         int
	height        int
	focus         focus
	selected      int
	active        assistant.Intent
	queued        assistant.Intent
	page          Page
	overview      domain.Overview
	busy          bool
	help          bool
	detail        bool
	palSearch     bool
	scroll        int
	choice        int
	now           func() time.Time
	scheduler     *motion.Scheduler
	visual        presentation
	activity      mascot.Pose
	refreshing    bool
	question      string
	toast         string
	stage         int
	scenario      string
	narrow        bool
	motionSaving  bool
	motionPending motion.Mode
	critical      bool
}

func New(ctx context.Context, source platform.Provider, inspect Inspect, opts Options) Model {
	if !opts.Motion.Valid() {
		opts.Motion = motion.Full
	}
	colors := newPalette(opts.Theme)
	field := textinput.New()
	field.Prompt = ""
	field.CharLimit = 200
	field.Placeholder = copy.placeholder.Text(opts.Language)
	field.SetStyles(inputStyles(colors))
	field.SetVirtualCursor(false)
	spin := spinner.New()
	if !opts.ASCII {
		spin.Spinner = spinner.MiniDot
	}
	full, empty := '━', '╌'
	if opts.ASCII {
		full, empty = '#', '-'
	}
	meter := progress.New(progress.WithoutPercentage(), progress.WithFillCharacters(full, empty),
		progress.WithColors(colors.secondary.GetForeground()))
	meter.EmptyColor = colors.frame.GetForeground()
	m := Model{
		ctx: ctx, source: source, inspect: inspect, options: opts, palette: colors,
		input: field, spinner: spin, meterModel: meter, width: 80, height: 24, active: assistant.Overview, busy: true,
		now: time.Now, scheduler: opts.Scheduler, activity: mascot.Scanning,
	}
	if opts.Gallery {
		m.focus = pageFocus
		m.galleryPage()
	} else {
		m.show(mascot.Greeting, mascot.Scanning, false)
	}
	return m
}

type overviewMsg struct{ data domain.Overview }
type replyMsg struct{ page Page }
type refreshMsg struct{}
type motionSavedMsg struct {
	mode motion.Mode
	err  error
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.sample(), m.nextRefresh())
}

func (m Model) sample() tea.Cmd {
	return func() tea.Msg {
		return overviewMsg{m.source.Overview(m.ctx)}
	}
}

func (m Model) nextRefresh() tea.Cmd {
	if m.options.Demo {
		return nil
	}
	return tea.Tick(8*time.Second, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m Model) tr(w Words) string { return w.Text(m.options.Language) }

func (m *Model) setFocus(f focus) tea.Cmd {
	m.focus = f
	m.showingFocus()
	if f == inputFocus {
		return m.input.Focus()
	}
	m.input.Blur()
	m.palSearch = false
	m.input.Placeholder = m.tr(copy.placeholder)
	return nil
}

func (m *Model) start(intent assistant.Intent, choice Choice) tea.Cmd {
	if strings.HasPrefix(choice.Kind, "gallery-") && m.options.Gallery {
		m.galleryChoice(choice)
		return nil
	}
	if choice.Kind == "motion" {
		return m.setMotion(motion.Mode(choice.Value))
	}
	if m.busy {
		if choice.Kind == "" {
			m.queued = intent
		}
		return nil
	}
	m.active, m.scroll, m.choice, m.detail = intent, 0, 0, false
	if i := slices.Index(assistant.Menu, intent); i >= 0 {
		m.selected = i
	}
	if choice.Kind == "" && intent == assistant.Overview {
		m.page = m.overviewFindingPage(m.overview)
		m.showReply()
		return nil
	}
	if m.inspect == nil && choice.Kind == "" && intent == assistant.Slow {
		m.page = m.overviewFindingPage(m.overview)
		m.showReply()
		return nil
	}
	if choice.Kind == "" && intent == assistant.Settings {
		m.settingsPage()
		return nil
	}
	if m.inspect == nil {
		m.page = Page{Availability: domain.Unsupported, Found: []string{m.tr(copy.needsData)}}
		m.showReply()
		return nil
	}
	m.busy = true
	m.activity = poseFor(intent)
	m.show(m.activity, "", true)
	m.page = Page{}
	if choice.Kind == "workload" {
		workload := assistant.Workload(choice.Value)
		if slices.Contains(assistant.Workloads, workload) {
			m.options.Workload = workload
		}
	}
	request := Request{Intent: intent, Choice: choice, Overview: m.overview, Language: m.options.Language, Workload: m.options.Workload}
	return func() tea.Msg { return replyMsg{m.inspect(m.ctx, request)} }
}

func (m *Model) updateOverview(data domain.Overview) tea.Cmd {
	wasInitial := m.overview.CapturedAt.IsZero()
	wasRefreshing := m.refreshing
	m.overview, m.refreshing = data, false
	if wasRefreshing {
		if m.active == assistant.Overview && !m.busy && m.page.Availability != domain.Unknown {
			m.page = m.overviewFindingPage(data)
		}
		return nil
	}
	m.busy = false
	if m.options.Gallery {
		return nil
	}
	if wasInitial && m.visual.pose == mascot.Greeting && data.Availability == domain.Available {
		m.visual.rest = mascot.Idle
		m.scheduleMotion(m.now())
	} else if wasInitial || m.visual.pose == mascot.Scanning {
		m.show(replyPose(Page{Availability: data.Availability}), "", false)
	}
	if m.queued != "" {
		intent := m.queued
		m.queued = ""
		return m.start(intent, Choice{})
	}
	if (wasInitial || m.active == assistant.Overview) && m.page.Availability != domain.Unknown {
		m.page = m.overviewFindingPage(data)
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scheduleMotion(m.now())
		w, _ := m.bounds()
		m.input.SetWidth(max(1, w-13))
	case overviewMsg:
		cmd = m.updateOverview(msg.data)
	case replyMsg:
		m.page, m.busy = msg.page, false
		if m.active == assistant.Storage && msg.page.Availability == domain.Available {
			m.critical = msg.page.Pose == mascot.Critical
		} else if msg.page.Pose == mascot.Critical {
			m.critical = true
		}
		m.showReply()
		if m.queued != "" {
			intent := m.queued
			m.queued = ""
			cmd = m.start(intent, Choice{})
		}
	case refreshMsg:
		cmd = m.nextRefresh()
		if !m.busy && !m.refreshing {
			m.refreshing = true
			cmd = tea.Batch(cmd, m.sample())
		}
	case motion.Tick:
		m.motionTick(msg)
	case motionSavedMsg:
		m.motionSaving = false
		if requested := m.motionPending; requested != "" && requested != msg.mode {
			m.motionPending = ""
			cmd = m.saveMotion(requested)
		} else {
			m.motionPending = ""
			if msg.err != nil {
				m.toast = m.tr(Words{"Preferensi gagal disimpan", "Could not save preference"}) + " · " + domain.SafeText(msg.err.Error())
			} else {
				m.toast = m.tr(Words{"Preferensi disimpan", "Preference saved"}) + " · " + string(msg.mode)
			}
		}
	case tea.KeyPressMsg:
		cmd = m.key(msg)
	default:
		if m.focus == inputFocus {
			m.input, cmd = m.input.Update(msg)
		}
	}
	return m, cmd
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	m.toast = ""
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "tab":
		return m.setFocus((m.focus + 1) % 3)
	case "shift+tab":
		return m.setFocus((m.focus + 2) % 3)
	case "esc":
		m.help, m.detail = false, false
		if m.options.Gallery {
			m.galleryPage()
			return m.setFocus(pageFocus)
		}
		m.question = ""
		m.input.Reset()
		return m.setFocus(menuFocus)
	}
	if m.focus == inputFocus {
		if msg.String() == "enter" {
			return m.submit()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	switch msg.String() {
	case "q":
		return m.quit()
	case "?":
		m.help, m.scroll = !m.help, 0
		m.accent(false, true)
	case "m":
		return m.setMotion(m.options.Motion.Next())
	case "n":
		if m.options.Gallery {
			m.narrow = !m.narrow
			w, _ := m.bounds()
			m.input.SetWidth(max(1, w-13))
			m.accent(false, true)
		}
	case "/":
		cmd := m.setFocus(inputFocus)
		m.palSearch = true
		m.input.Placeholder = m.tr(copy.palette)
		return cmd
	case "d":
		m.detail, m.scroll = !m.detail, 0
		m.accent(false, true)
	case "r":
		if m.active == assistant.Overview && !m.busy && !m.refreshing {
			m.busy = true
			m.activity = mascot.Scanning
			m.show(m.activity, "", true)
			return m.sample()
		}
		if m.active == assistant.Storage {
			return m.start(m.active, Choice{Kind: "rescan"})
		}
		return m.start(m.active, Choice{})
	case "up", "k", "down", "j", "pgup", "pgdown":
		step := 1
		if msg.String() == "up" || msg.String() == "k" || msg.String() == "pgup" {
			step = -1
		}
		if m.focus == menuFocus {
			m.selected = (m.selected + len(assistant.Menu) + step) % len(assistant.Menu)
			m.showingFocus()
		} else {
			if msg.String() == "pgup" || msg.String() == "pgdown" {
				step *= 6
			}
			m.scroll = min(max(0, m.scroll+step), max(0, len(m.sessionLines(m.sessionWidth()-4))-m.sessionContentHeight()))
		}
	case "left", "right":
		if m.focus == pageFocus && len(m.page.Choices) > 0 {
			step := 1
			if msg.String() == "left" {
				step = -1
			}
			m.choice = (m.choice + len(m.page.Choices) + step) % len(m.page.Choices)
			m.accent(true, false)
		}
	case "enter":
		if m.options.Gallery && m.focus == menuFocus {
			return m.setFocus(pageFocus)
		}
		if m.help {
			m.help, m.scroll = false, 0
			return nil
		}
		if m.focus == menuFocus {
			m.question = ""
			return m.start(assistant.Menu[m.selected], Choice{})
		}
		if m.focus == pageFocus && len(m.page.Choices) > 0 {
			choice := m.page.Choices[m.choice]
			if choice.Kind == "navigate" {
				return m.start(assistant.Intent(choice.Value), Choice{})
			}
			return m.start(m.active, choice)
		}
	}
	return nil
}

func (m *Model) submit() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	palette := m.palSearch
	if value == "" {
		return nil
	}
	m.input.Reset()
	m.setFocus(pageFocus)
	m.question = domain.SafeText(value)
	if m.options.Gallery {
		for _, choice := range m.page.Choices {
			if strings.EqualFold(choice.Label, value) || strings.EqualFold(choice.Value, value) {
				m.galleryChoice(choice)
				break
			}
		}
		return nil
	}
	m.help, m.detail, m.scroll, m.choice = false, false, 0, 0
	if m.active == assistant.Recommend {
		if workload := assistant.Context(value); workload != "" {
			return m.start(assistant.Recommend, Choice{Kind: "workload", Value: string(workload)})
		}
	}
	intents := assistant.Route(value)
	if palette {
		intents = nil
		for _, intent := range assistant.Menu {
			if strings.Contains(strings.ToLower(m.tr(menuNames[intent])), strings.ToLower(value)) ||
				strings.Contains(string(intent), strings.ToLower(value)) {
				intents = append(intents, intent)
			}
		}
		if len(intents) == 0 {
			intents = assistant.Route(value)
		}
	}
	switch len(intents) {
	case 1:
		cmd := m.start(intents[0], Choice{})
		if m.busy {
			m.show(mascot.Thinking, m.activity, false)
		} else if m.page.Pose == "" && m.page.Availability == domain.Available {
			m.show(mascot.Thinking, mascot.Idle, false)
		}
		return cmd
	case 0:
		m.clarify(m.tr(copy.unknown), assistant.Menu)
	default:
		m.clarify(m.tr(copy.clarify), intents)
	}
	return nil
}

func (m *Model) clarify(message string, intents []assistant.Intent) {
	m.page = Page{Availability: domain.Unknown, Found: []string{message}}
	m.show(mascot.Thinking, mascot.Unknown, false)
	for _, intent := range intents {
		m.page.Choices = append(m.page.Choices, Choice{Label: m.tr(menuNames[intent]), Kind: "navigate", Value: string(intent)})
	}
}

func (m *Model) quit() tea.Cmd {
	m.show(mascot.Farewell, "", false)
	if m.scheduler != nil {
		m.scheduler.Set(m.visual.generation, time.Time{})
	}
	return tea.Quit
}

func (m *Model) settingsPage() {
	m.page = Page{Availability: domain.Available, Found: []string{
		m.tr(Words{"Mode gerak Piko", "Piko motion mode"}) + " · " + string(m.options.Motion),
	}, Advice: []string{m.tr(Words{
		"Pilih full, reduced (pose sederhana), atau off (tanpa timer animasi).",
		"Select full, reduced (simple poses), or off (no motion timers).",
	})}}
	for _, mode := range []motion.Mode{motion.Full, motion.Reduced, motion.Off} {
		m.page.Choices = append(m.page.Choices, Choice{Label: string(mode), Kind: "motion", Value: string(mode)})
	}
	m.showReply()
}

func (m *Model) setMotion(mode motion.Mode) tea.Cmd {
	if !mode.Valid() {
		return nil
	}
	m.options.Motion = mode
	m.visual.focusUntil, m.visual.panelUntil = time.Time{}, time.Time{}
	m.show(m.visual.pose, m.visual.rest, m.visual.loop)
	if m.active == assistant.Settings {
		m.settingsPage()
	}
	m.toast = m.tr(Words{"Gerak", "Motion"}) + " · " + string(mode)
	return m.saveMotion(mode)
}

func (m *Model) saveMotion(mode motion.Mode) tea.Cmd {
	save := m.options.SaveMotion
	if save == nil || m.options.Gallery {
		return nil
	}
	if m.motionSaving {
		m.motionPending = mode
		return nil
	}
	m.motionSaving = true
	return func() tea.Msg { return motionSavedMsg{mode, save(mode)} }
}

func (m Model) overviewFindingPage(data domain.Overview) Page {
	page := overviewPage(data, m.options.Language)
	if data.CapturedAt.IsZero() {
		return page
	}
	at := m.now()
	if m.options.Demo {
		at = platform.DemoTime
	}
	a := diagnostics.Analyzer{Language: m.options.Language, Now: at}
	page = withFindings(page, a.Overview(data), m.options.Language)
	return page
}

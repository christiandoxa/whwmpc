package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
)

type Options struct {
	Language, Theme           string
	Demo, ASCII, ReduceMotion bool
}

type Choice struct {
	Label string
	Kind  string
	Value string
}

type Page struct {
	Availability     domain.Availability
	Found, Meaning   []string
	Advice, Evidence []string
	Choices          []Choice
}

type Request struct {
	Intent   assistant.Intent
	Choice   Choice
	Overview domain.Overview
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
	ctx       context.Context
	source    platform.Provider
	inspect   Inspect
	options   Options
	palette   palette
	input     textinput.Model
	spinner   spinner.Model
	width     int
	height    int
	focus     focus
	selected  int
	active    assistant.Intent
	queued    assistant.Intent
	page      Page
	overview  domain.Overview
	busy      bool
	help      bool
	detail    bool
	palSearch bool
	scroll    int
	choice    int
}

func New(ctx context.Context, source platform.Provider, inspect Inspect, opts Options) Model {
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
	return Model{
		ctx: ctx, source: source, inspect: inspect, options: opts, palette: colors,
		input: field, spinner: spin, width: 80, height: 24, active: assistant.Overview, busy: true,
	}
}

type overviewMsg struct{ data domain.Overview }
type replyMsg struct{ page Page }
type refreshMsg struct{}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.sample(), m.animate(), m.nextRefresh())
}

func (m Model) sample() tea.Cmd {
	return func() tea.Msg {
		return overviewMsg{m.source.Overview(m.ctx)}
	}
}

func (m Model) animate() tea.Cmd {
	if m.options.ReduceMotion {
		return nil
	}
	return m.spinner.Tick
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
	if f == inputFocus {
		return m.input.Focus()
	}
	m.input.Blur()
	m.palSearch = false
	m.input.Placeholder = m.tr(copy.placeholder)
	return nil
}

func (m *Model) start(intent assistant.Intent, choice Choice) tea.Cmd {
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
		m.page = overviewPage(m.overview, m.options.Language)
		return nil
	}
	if m.inspect == nil && choice.Kind == "" && intent == assistant.Slow {
		m.page = overviewPage(m.overview, m.options.Language)
		return nil
	}
	if m.inspect == nil {
		m.page = Page{Availability: domain.Unsupported, Found: []string{m.tr(copy.needsData)}}
		return nil
	}
	m.busy = true
	m.page = Page{}
	request := Request{Intent: intent, Choice: choice, Overview: m.overview, Language: m.options.Language}
	return tea.Batch(func() tea.Msg { return replyMsg{m.inspect(m.ctx, request)} }, m.animate())
}

func (m *Model) updateOverview(data domain.Overview) tea.Cmd {
	wasInitial := m.overview.CapturedAt.IsZero()
	m.overview, m.busy = data, false
	if m.queued != "" {
		intent := m.queued
		m.queued = ""
		return m.start(intent, Choice{})
	}
	if (wasInitial || m.active == assistant.Overview) && m.page.Availability != domain.Unknown {
		m.page = overviewPage(data, m.options.Language)
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, min(m.width, 138)-13))
	case overviewMsg:
		cmd = m.updateOverview(msg.data)
	case replyMsg:
		m.page, m.busy = msg.page, false
		if m.queued != "" {
			intent := m.queued
			m.queued = ""
			cmd = m.start(intent, Choice{})
		}
	case refreshMsg:
		cmd = m.nextRefresh()
		if !m.busy {
			m.busy = true
			cmd = tea.Batch(cmd, m.sample(), m.animate())
		}
	case spinner.TickMsg:
		if m.busy {
			m.spinner, cmd = m.spinner.Update(msg)
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
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "tab":
		return m.setFocus((m.focus + 1) % 3)
	case "shift+tab":
		return m.setFocus((m.focus + 2) % 3)
	case "esc":
		m.help, m.detail = false, false
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
		return tea.Quit
	case "?":
		m.help, m.scroll = !m.help, 0
	case "/":
		cmd := m.setFocus(inputFocus)
		m.palSearch = true
		m.input.Placeholder = m.tr(copy.palette)
		return cmd
	case "d":
		m.detail, m.scroll = !m.detail, 0
	case "r":
		if m.active == assistant.Overview && !m.busy {
			m.busy = true
			return tea.Batch(m.sample(), m.animate())
		}
		return m.start(m.active, Choice{})
	case "up", "k", "down", "j", "pgup", "pgdown":
		step := 1
		if msg.String() == "up" || msg.String() == "k" || msg.String() == "pgup" {
			step = -1
		}
		if m.focus == menuFocus {
			m.selected = (m.selected + len(assistant.Menu) + step) % len(assistant.Menu)
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
		}
	case "enter":
		if m.help {
			m.help, m.scroll = false, 0
			return nil
		}
		if m.focus == menuFocus {
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
	m.help, m.detail, m.scroll, m.choice = false, false, 0, 0
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
		return m.start(intents[0], Choice{})
	case 0:
		m.clarify(m.tr(copy.unknown), assistant.Menu)
	default:
		m.clarify(m.tr(copy.clarify), intents)
	}
	return nil
}

func (m *Model) clarify(message string, intents []assistant.Intent) {
	m.page = Page{Availability: domain.Unknown, Found: []string{message}}
	for _, intent := range intents {
		m.page.Choices = append(m.page.Choices, Choice{Label: m.tr(menuNames[intent]), Kind: "navigate", Value: string(intent)})
	}
}

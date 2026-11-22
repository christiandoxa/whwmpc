package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

type Options struct {
	Language, Theme      string
	Demo, ASCII, Gallery bool
	Motion               motion.Mode
	Scheduler            *motion.Scheduler
	SaveMotion           func(motion.Mode) error
	Authenticate         Authenticator
	Send                 func(tea.Msg)
	Workload             assistant.Workload
}

type Choice struct {
	Label string
	Kind  string
	Value string
}

type Page struct {
	Snapshot                  *workflow.Snapshot
	Coverage                  []workflow.Coverage
	Summary                   *ResultSummary
	Availability              domain.Availability
	Pose                      mascot.Pose
	Findings                  []diagnostics.Finding
	Priority                  string
	Plan                      *actions.Plan
	Confirm                   bool
	Found, Meaning            []string
	Advice, Evidence, Sidebar []string
	Choices                   []Choice
}

type Request struct {
	ScanProgress func(workflow.ScanProgress)
	Output       string
	Printing     func()
	Findings     []diagnostics.Finding
	Plan         *actions.Plan
	Progress     func(actions.Stage)
	Intent       assistant.Intent
	Choice       Choice
	Overview     domain.Overview
	Workload     assistant.Workload
	Language     string
}

type Inspect func(context.Context, Request) Page

type screen int

const (
	home screen = iota
	more
	questionInput
	fileInput
	scanning
	results
	detail
	reviewPlan
	executing
	outcome
	help
	gallery
	taskQuestion
)

type screenState struct {
	screen         screen
	page           Page
	active         assistant.Intent
	choice, scroll int
	exportType     string
}

type Model struct {
	screenState
	back                                         []screenState
	ctx                                          context.Context
	source                                       platform.Provider
	inspect                                      Inspect
	options                                      Options
	palette                                      palette
	input                                        textinput.Model
	spinner                                      spinner.Model
	width, height                                int
	now                                          func() time.Time
	scheduler                                    *motion.Scheduler
	visual                                       presentation
	activity, actionPose                         mascot.Pose
	overview                                     domain.Overview
	lastScreen                                   string
	toast                                        string
	stage                                        int
	scenario                                     string
	narrow                                       bool
	motionSaving                                 bool
	motionPending                                motion.Mode
	critical                                     bool
	job                                          uint64
	cancelJob                                    context.CancelFunc
	workName                                     string
	busy, runningAction, cancelling, quitPending bool
}

func New(ctx context.Context, source platform.Provider, inspect Inspect, opts Options) Model {
	if !opts.Motion.Valid() {
		opts.Motion = motion.Full
	}
	colors := newPalette(opts.Theme)
	field := textinput.New()
	field.Prompt = ""
	field.CharLimit = 1024
	field.Placeholder = copy.placeholder.Text(opts.Language)
	field.SetStyles(inputStyles(colors))
	field.SetVirtualCursor(false)
	m := Model{ctx: ctx, source: source, inspect: inspect, options: opts, palette: colors, input: field,
		width: 80, height: 24, now: time.Now, scheduler: opts.Scheduler}
	m.homePage()
	m.show(mascot.Greeting, mascot.Idle, false)
	if opts.Gallery {
		m.screen = gallery
		m.galleryPage()
	}
	return m
}

type overviewMsg struct{ data domain.Overview }
type replyMsg struct {
	job  uint64
	page Page
}
type motionSavedMsg struct {
	mode motion.Mode
	err  error
}

func (m Model) Init() tea.Cmd     { return nil }
func (m Model) tr(w Words) string { return w.Text(m.options.Language) }

func (m *Model) homePage() {
	m.screenState = screenState{screen: home, page: Page{Choices: []Choice{
		{m.tr(menuNames[assistant.Overview]), "navigate", string(assistant.Overview)},
		{m.tr(menuNames[assistant.Slow]), "navigate", string(assistant.Slow)},
		{m.tr(menuNames[assistant.Cleanup]), "navigate", string(assistant.Cleanup)},
		{m.tr(Words{"Lainnya…", "More…"}), "more", ""},
	}}}
}

func (m *Model) remember() {
	m.back = append(slices.Clone(m.back[max(0, len(m.back)-7):]), m.screenState)
}

func (m *Model) returnTo() tea.Cmd {
	m.input.Blur()
	m.input.Reset()
	if len(m.back) > 0 {
		m.screenState = m.back[len(m.back)-1]
		m.back = m.back[:len(m.back)-1]
	} else if m.options.Gallery {
		m.screen = gallery
		m.galleryPage()
	} else {
		m.homePage()
	}
	m.showReply()
	if m.typing() {
		return m.input.Focus()
	}
	return nil
}

func (m Model) typing() bool { return m.screen == questionInput || m.screen == fileInput }

func (m *Model) ask() tea.Cmd {
	m.remember()
	m.screenState = screenState{screen: questionInput}
	m.input.Reset()
	m.input.Placeholder = m.tr(copy.placeholder)
	m.show(mascot.Listening, "", false)
	return m.input.Focus()
}

func (m *Model) start(intent assistant.Intent, choice Choice) tea.Cmd {
	if m.busy {
		return nil
	}
	plan := m.page.Plan
	switch choice.Kind {
	case "back", "cancel-plan":
		return m.returnTo()
	case "more":
		m.remember()
		m.screenState = screenState{screen: more}
		for _, id := range []assistant.Intent{assistant.Storage, assistant.Trim, assistant.Power, assistant.History, assistant.Report, assistant.Settings} {
			m.page.Choices = append(m.page.Choices, Choice{m.tr(menuNames[id]), "navigate", string(id)})
		}
		m.page.Choices = append(m.page.Choices, Choice{m.tr(Words{"Tanya sesuatu", "Ask a question"}), "ask", ""})
		return nil
	case "ask":
		return m.ask()
	case "result-options":
		m.remember()
		m.screen, m.choice, m.scroll = taskQuestion, 0, 0
		m.page.Found = []string{m.tr(Words{"Apa langkah berikutnya?", "What would you like to do next?"})}
		m.page.Meaning, m.page.Advice = nil, nil
		m.page.Choices = append(slices.Clone(m.page.Choices), Choice{m.tr(Words{"Kembali", "Back"}), "back", ""})
		return nil
	case "details":
		m.remember()
		m.screen, m.scroll = detail, 0
		return nil
	case "motion":
		return m.setMotion(motion.Mode(choice.Value))
	case "confirm-plan":
		if m.screen == reviewPlan && !m.options.Demo && plan != nil && choice.Value == plan.ID && m.reviewFits() {
			m.confirmPage()
		}
		return nil
	case "apply":
		if m.screen != reviewPlan || m.options.Demo || plan == nil || !m.page.Confirm || choice.Value != plan.ID || !m.reviewFits() {
			return nil
		}
	case "export-select":
		m.remember()
		m.screen = fileInput
		m.exportType = choice.Value
		m.input.Placeholder = m.tr(Words{"Jalur file baru", "Path to a new file"})
		name := "whwmpc-report.md"
		if choice.Value == "json" {
			name = "whwmpc-report.json"
		}
		m.input.SetValue(name)
		m.input.CursorEnd()
		return m.input.Focus()
	}
	if m.options.Gallery && strings.HasPrefix(choice.Kind, "gallery-") {
		m.galleryChoice(choice)
		return nil
	}
	if choice.Kind == "navigate" {
		intent = assistant.Intent(choice.Value)
		choice = Choice{}
	}
	if choice.Kind != "rescan" && choice.Kind != "apply" && choice.Kind != "workload" {
		m.remember()
	}
	req := Request{Intent: intent, Choice: choice, Overview: m.overview, Plan: plan, Findings: m.page.Findings,
		Language: m.options.Language, Workload: m.options.Workload}
	m.screenState = screenState{screen: results, active: intent}
	if choice.Kind == "" && intent == assistant.Settings {
		m.settingsPage()
		return nil
	}
	if choice.Kind == "" && intent == assistant.Report {
		m.page = reportPage(m.options.Language)
		return nil
	}
	if choice.Kind == "workload" && slices.Contains(assistant.Workloads, assistant.Workload(choice.Value)) {
		m.options.Workload = assistant.Workload(choice.Value)
		req.Workload = m.options.Workload
	}
	if m.inspect == nil && intent != assistant.Overview {
		m.page = Page{Availability: domain.Unsupported, Found: []string{m.tr(copy.needsData)}}
		return nil
	}
	return m.launch(req, aimFor(intent, choice, plan))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scheduleMotion(m.now())
		w, _ := m.bounds()
		m.input.SetWidth(max(1, w-4))
	case overviewMsg:
		m.overview = msg.data
	case authMsg:
		if msg.job == m.job && m.busy {
			if msg.err != nil || msg.ctx.Err() != nil {
				page := cancellationPage(m.options.Language, m.active)
				if msg.err != nil && msg.ctx.Err() == nil {
					page = unavailableAction(msg.err, m.options.Language)
				}
				cmd = func() tea.Msg { return replyMsg{msg.job, page} }
			} else {
				m.workName = ""
				cmd = m.doRequest(msg.ctx, msg.job, msg.req)
			}
		}
	case scanMsg:
		if m.busy && msg.job == m.job && !m.cancelling {
			m.workName = fmt.Sprintf(m.tr(Words{"Memeriksa %s · tahap %d/%d", "Checking %s · stage %d/%d"}), m.tr(checkNames[msg.progress.Check]), msg.progress.Step, msg.progress.Total)
		}
	case stageMsg:
		m.updateStage(msg)
	case printingMsg:
		if m.busy && m.job == msg.job && !m.cancelling {
			m.activity = mascot.Printing
			m.workName = displayStage(m.activity, m.options.Language)
			m.show(m.activity, "", true)
		}
	case ShutdownMsg:
		if m.cancelCurrent() {
			m.quitPending = true
		} else {
			cmd = m.quit()
		}
	case replyMsg:
		if msg.job != m.job || !m.busy {
			return m, nil
		}
		m.cancelJob()
		m.cancelJob = nil
		if m.cancelling && !m.runningAction {
			m.busy, m.cancelling = false, false
			if m.quitPending {
				return m, m.quit()
			}
			return m, m.returnTo()
		}
		wasAction := m.screen == executing
		m.page, m.busy, m.runningAction, m.cancelling, m.workName = msg.page, false, false, false, ""
		m.screen, m.choice, m.scroll = results, 0, 0
		if len(m.page.Findings) > 0 && m.active != assistant.Recommend {
			summary := summarize(m.page, m.active, m.options.Language)
			m.page.Summary = &summary
		}
		if m.page.Plan != nil {
			m.screen = reviewPlan
		}
		if wasAction {
			m.screen = outcome
		}
		if m.active == assistant.Storage && len(m.page.Findings) > 0 {
			if m.page.Pose == mascot.Critical {
				m.critical = true
			} else if m.page.Availability == domain.Available {
				m.critical = false
			}
		}
		m.showReply()
		if m.quitPending {
			cmd = m.quit()
		}
	case motion.Tick:
		m.motionTick(msg)
	case motionSavedMsg:
		m.motionSaving = false
		requested := m.motionPending
		m.motionPending = ""
		if requested != "" && requested != msg.mode {
			cmd = m.saveMotion(requested)
		} else if msg.err != nil {
			m.toast = m.tr(Words{"Preferensi gagal disimpan", "Could not save preference"}) + " · " + domain.SafeText(msg.err.Error())
		} else {
			m.toast = m.tr(Words{"Preferensi disimpan", "Preference saved"})
		}
	case tea.KeyPressMsg:
		cmd = m.key(msg)
	default:
		if m.typing() {
			m.input, cmd = m.input.Update(msg)
		}
	}
	return m, cmd
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	m.toast = ""
	key := msg.String()
	if key == "ctrl+c" || (key == "q" && !m.typing()) {
		if m.cancelCurrent() {
			m.quitPending = true
			return nil
		}
		return m.quit()
	}
	if key == "esc" {
		if m.cancelCurrent() {
			return nil
		}
		if m.screen == home {
			return nil
		}
		return m.returnTo()
	}
	if m.typing() {
		if key == "enter" {
			return m.submit()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	if m.busy {
		return nil
	}
	switch key {
	case "?":
		if m.screen == help {
			return m.returnTo()
		}
		m.remember()
		m.screen = help
		m.scroll = 0
	case "/":
		if m.screen != reviewPlan && !m.options.Gallery {
			return m.ask()
		}
	case "n":
		if m.options.Gallery {
			m.narrow = !m.narrow
		}
	case "m":
		if m.options.Gallery {
			return m.setMotion(m.options.Motion.Next())
		}
	case "up", "down", "j", "k", "pgup", "pgdown":
		step := 1
		if key == "up" || key == "k" || key == "pgup" {
			step = -1
		}
		if key == "pgup" || key == "pgdown" {
			step *= 5
		}
		if m.screen == detail || m.screen == help || key == "pgup" || key == "pgdown" || len(m.choices()) == 0 {
			m.scroll = max(0, min(m.scroll+step, m.maxScroll()))
		} else {
			m.choice = max(0, min(m.choice+step, len(m.choices())-1))
			if m.screen != reviewPlan {
				m.accent(true, false)
			}
		}
	case "enter":
		if m.screen == detail {
			return m.start(m.active, Choice{Kind: "result-options"})
		}
		choices := m.choices()
		if m.choice < len(choices) {
			return m.start(m.active, choices[m.choice])
		}
	}
	return nil
}

func (m *Model) submit() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return nil
	}
	m.input.Blur()
	m.input.Reset()
	if m.screen == fileInput {
		format := m.exportType
		m.screen = results
		return m.launch(Request{Intent: assistant.Report, Choice: Choice{Kind: "export", Value: format}, Output: value,
			Overview: m.overview, Language: m.options.Language}, mascot.Scanning)
	}
	intents := assistant.Route(value)
	if len(intents) == 1 {
		if intent := intents[0]; intent != "" {
			m.returnTo()
			return m.start(intent, Choice{})
		}
	}
	m.screen = taskQuestion
	message := m.tr(copy.clarify)
	if len(intents) == 0 {
		message = m.tr(copy.unknown)
		intents = assistant.Menu
	}
	m.page = Page{Availability: domain.Unknown, Found: []string{message}}
	for _, id := range intents {
		m.page.Choices = append(m.page.Choices, Choice{m.tr(menuNames[id]), "navigate", string(id)})
	}
	return nil
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
	if !m.options.Demo {
		page.Choices = appendChoice(page.Choices, saveScanChoice(m.options.Language))
	}
	return page
}

func (m Model) stalePage() bool {
	if m.options.Demo || m.busy || len(m.page.Findings) == 0 {
		return false
	}
	var stamp time.Time
	for _, finding := range m.page.Findings {
		if finding.At.After(stamp) {
			stamp = finding.At
		}
	}
	if stamp.IsZero() {
		return false
	}
	age := 30 * time.Second
	switch m.active {
	case assistant.Power, assistant.Recommend:
		age = time.Minute
	case assistant.Storage:
		age = 30 * time.Minute
	case assistant.Trim:
		age = 15 * time.Minute
	}
	return m.now().Sub(stamp) > age
}

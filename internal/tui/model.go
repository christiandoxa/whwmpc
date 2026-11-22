package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
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
	"github.com/christiandoxa/whwmpc/internal/privread"
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
	Privilege            *privread.Session
	Send                 func(tea.Msg)
	Workload             assistant.Workload
	DemoCleanupResult    bool
}

type Choice struct {
	Label string
	Kind  string
	Value string
}

type Page struct {
	Snapshot         *workflow.Snapshot
	Coverage         []workflow.Coverage
	Summary          *ResultSummary
	Availability     domain.Availability
	Pose             mascot.Pose
	Findings         []diagnostics.Finding
	Plan             *actions.Plan
	Found, Meaning   []string
	Advice, Evidence []string
	Choices          []Choice
	refreshHistory   bool
}

type Request struct {
	Snapshot        *workflow.Snapshot
	ScanProgress    func(workflow.ScanProgress)
	Output          string
	Printing        func()
	Findings        []diagnostics.Finding
	Plan            *actions.Plan
	Progress        func(actions.Stage)
	Intent          assistant.Intent
	Choice          Choice
	Overview        domain.Overview
	Workload        assistant.Workload
	Language        string
	Complete        privread.Operation
	ObserveFor      time.Duration
	SimulateCleanup bool
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
	components
	resultOptions
)

type screenState struct {
	screen         screen
	page           Page
	active         assistant.Intent
	choice, scroll int
	reviewThrough  int
	exportType     string
	pendingOutput  string
	parameter      Choice
	question       string
	access         *readAccess
	readErr        error
	request        *Request
	component      string
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
	accessChosen                                 bool
	lastChecked                                  time.Time
	lastCoverage                                 string
	lastConclusion                               string
	toast                                        string
	stage                                        int
	scenario                                     string
	narrow                                       bool
	motionSaving                                 bool
	motionPending                                motion.Mode
	criticalSSD, criticalDisk                    bool
	job                                          uint64
	cancelJob                                    context.CancelFunc
	workName                                     string
	beforeRead                                   *screenState
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
	m.accent(false, true)
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
		{m.tr(Words{"Pemeriksaan lain", "Other checks"}), "more", ""},
	}}}
}

func (m *Model) remember() {
	m.back = append(slices.Clone(m.back[max(0, len(m.back)-7):]), m.screenState)
}

func (m *Model) returnTo() tea.Cmd {
	refreshHistory := m.screen == results && m.page.refreshHistory
	fromOutcome := m.screen == outcome
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
	if refreshHistory && m.active == assistant.History {
		return m.start(assistant.History, Choice{Kind: "rescan"})
	}
	if fromOutcome && m.screen == results {
		m.toast = m.tr(Words{"Ringkasan sebelum tindakan", "Summary from before the action"})
	}
	if m.typing() {
		m.show(mascot.Listening, "", false)
		return m.input.Focus()
	}
	m.showReply()
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
	case "check-level":
		m.remember()
		m.privilegeChoices(readAccess{purpose: changeReads, scope: privread.All})
		return nil
	case "ordinary":
		if m.access == nil {
			return nil
		}
		m.options.Privilege.Disable()
		return m.finishReadAuthorization(nil)
	case "complete-read":
		return m.completeRead(privread.Operation(choice.Value))
	case "deep-authorize":
		return m.authorizeReads()
	case "back", "cancel-plan":
		return m.returnTo()
	case "finish":
		m.back = nil
		m.homePage()
		m.showReply()
		return nil
	case "more":
		m.remember()
		m.screenState = screenState{screen: more}
		for _, id := range []assistant.Intent{assistant.Storage, assistant.Trim, assistant.Power} {
			m.page.Choices = append(m.page.Choices, Choice{m.tr(menuNames[id]), "navigate", string(id)})
		}
		m.page.Choices = append(m.page.Choices, Choice{m.tr(Words{"Pemeriksaan lanjutan", "Advanced checks"}), "advanced", ""},
			Choice{m.tr(Words{"Alat bantu", "Utilities"}), "utilities", ""})
		return nil
	case "utilities":
		m.remember()
		m.screenState = screenState{screen: taskQuestion, page: Page{Found: []string{m.tr(Words{"Alat bantu", "Utilities"})}}}
		for _, id := range []assistant.Intent{assistant.History, assistant.Report, assistant.Settings} {
			m.page.Choices = append(m.page.Choices, Choice{m.tr(menuNames[id]), "navigate", string(id)})
		}
		m.page.Choices = append(m.page.Choices, Choice{m.tr(Words{"Tanya sesuatu", "Ask a question"}), "ask", ""})
		return nil
	case "advanced":
		m.remember()
		m.screenState = screenState{screen: taskQuestion, page: Page{Found: []string{m.tr(Words{"Apa yang ingin ditinjau?", "What would you like to review?"})}}}
		for _, id := range []assistant.Intent{assistant.GPU, assistant.Firmware, assistant.Filesystem, assistant.Benchmark, assistant.Daemon} {
			m.page.Choices = append(m.page.Choices, Choice{m.tr(menuNames[id]), "navigate", string(id)})
		}
		return nil
	case "gpu-value":
		m.remember()
		m.screen = fileInput
		m.exportType = "gpu"
		m.parameter = choice
		m.input.Reset()
		m.input.Placeholder = m.tr(Words{"Nilai integer", "Integer value"})
		return m.input.Focus()
	case "ask":
		return m.ask()
	case "result-options":
		m.remember()
		m.screen, m.choice, m.scroll = resultOptions, 0, 0
		return nil
	case "details":
		m.remember()
		m.screen, m.component, m.choice, m.scroll = components, "", 0, 0
		switch choices := m.componentChoices(); len(choices) {
		case 0:
			m.screen = detail
		case 1:
			m.screen, m.component = detail, choices[0].Value
		}
		return nil
	case "component", "evidence":
		m.remember()
		m.screen, m.component, m.choice, m.scroll = detail, choice.Value, 0, 0
		return nil
	case "motion":
		return m.setMotion(motion.Mode(choice.Value))
	case "apply":
		if m.screen != reviewPlan || plan == nil || m.options.Demo && !m.demoCleanupAllowed(plan) || choice.Value != plan.ID ||
			m.choice != 1 || !m.reviewReady() {
			return nil
		}
	case "save-snapshot":
		m.remember()
		m.screen, m.choice, m.scroll = taskQuestion, 0, 0
		page := reportPage(m.options.Language)
		page.Snapshot = m.page.Snapshot
		page.Found = []string{m.tr(Words{"Simpan hasil ini", "Save these results"})}
		page.Meaning = []string{m.tr(Words{"Gunakan hasil yang sedang dibuka.", "Export the results already open."})}
		m.page = page
		return nil
	case "bench-review":
		m.remember()
		m.screen, m.choice, m.scroll = taskQuestion, 0, 0
		duration := m.tr(Words{"3 detik, satu worker", "3 seconds, one worker"})
		if choice.Value == "stress" {
			duration = m.tr(Words{"15 detik, satu worker, berhenti pada 85 °C atau saat sensor hilang", "15 seconds, one worker, stops at 85 °C or if the sensor is lost"})
		}
		m.page = Page{Found: []string{m.tr(Words{"Jalankan pengujian CPU?", "Start the CPU workload?"}), duration},
			Choices: []Choice{{m.tr(Words{"Kembali", "Back"}), "back", ""}, {m.tr(Words{"Mulai pengujian", "Start workload"}), "benchmark-run", choice.Value}}}
		return nil
	case "bench-path":
		m.remember()
		m.screen = fileInput
		m.exportType = choice.Value
		m.input.Placeholder = m.tr(Words{"Path file atau direktori yang diuji", "Path to the file or directory to test"})
		m.input.Reset()
		return m.input.Focus()
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
	if intent == assistant.Slow && choice.Kind == "" {
		return m.slowdownQuestion()
	}
	if choice.Kind == "slow-observe" && m.screen == results && m.active == assistant.Slow && m.request != nil {
		req := *m.request
		req.Choice, req.Complete, req.Snapshot, req.ObserveFor = choice, "", nil, workflow.ObservationWindow
		return m.repeatCheck(req)
	}
	if choice.Kind == "rescan" && m.page.Summary != nil && m.request != nil && m.request.Intent == intent && m.request.Plan == nil && startsInspection(*m.request) {
		req := *m.request
		req.Choice, req.Complete, req.Snapshot = choice, "", nil
		return m.repeatCheck(req)
	}
	question := m.question
	if choice.Kind != "rescan" && choice.Kind != "apply" && choice.Kind != "workload" {
		m.remember()
	}
	req := Request{Intent: intent, Choice: choice, Output: m.pendingOutput, Overview: m.overview, Plan: plan, Findings: m.page.Findings,
		Language: m.options.Language, Workload: m.options.Workload}
	if choice.Kind == "apply" && m.demoCleanupAllowed(plan) {
		req.SimulateCleanup = true
	}
	if choice.Kind == "slow-observe" {
		req.ObserveFor = workflow.ObservationWindow
	}
	m.screenState = screenState{screen: results, active: intent, question: question}
	if choice.Kind == "" && intent == assistant.Settings {
		m.settingsPage()
		return nil
	}
	if choice.Kind == "" && intent == assistant.Benchmark {
		m.page = benchmarkPage(m.options.Language)
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
		if m.screen == reviewPlan && (m.width != msg.Width || m.height != msg.Height) {
			m.reviewThrough, m.scroll = 0, 0
		}
		m.width, m.height = msg.Width, msg.Height
		m.recordReview()
		m.scheduleMotion(m.now())
		w, _ := m.bounds()
		m.input.SetWidth(max(1, w-4))
	case overviewMsg:
		m.overview = msg.data
	case privilegeMsg:
		if m.job == msg.job && m.busy && m.access != nil {
			if m.cancelling && msg.err == nil {
				m.options.Privilege.Disable()
				msg.err = privread.ErrCancelled
			}
			m.cancelJob()
			m.cancelJob = nil
			m.busy, m.cancelling, m.workName = false, false, ""
			if m.quitPending {
				cmd = m.quit()
			} else {
				cmd = m.finishReadAuthorization(msg.err)
			}
		}
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
			if p := msg.progress; p.Window > 0 {
				m.workName = m.observationLabel(p.Elapsed, p.Window)
			} else {
				m.workName = fmt.Sprintf(m.tr(Words{"Memeriksa %s · tahap %d/%d", "Checking %s · stage %d/%d"}), m.tr(checkNames[msg.progress.Check]), msg.progress.Step, msg.progress.Total)
			}
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
		previousRead := m.beforeRead
		m.beforeRead = nil
		if previousRead != nil {
			if note := m.repeatFailure(msg.page); note != "" {
				m.screenState = *previousRead
				m.busy, m.cancelling, m.workName = false, false, ""
				if m.quitPending {
					return m, m.quit()
				}
				m.toast = note
				m.showReply()
				return m, nil
			}
		}
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
		if m.page.Plan != nil {
			m.screen = reviewPlan
			m.reviewThrough = 0
			m.recordReview()
		}
		if m.active == assistant.Recommend && slices.ContainsFunc(m.page.Choices, func(c Choice) bool { return c.Kind == "workload" }) {
			m.screen = taskQuestion
		}
		if wasAction {
			m.screen = outcome
		}
		if m.screen == results || m.screen == outcome {
			summary := summarize(m.page, m.active, m.options.Language)
			m.summarizeReads(&summary)
			m.page.Summary = &summary
			if !summary.At.IsZero() && (m.screen == outcome || m.request != nil && startsInspection(*m.request)) {
				m.lastChecked, m.lastCoverage = summary.At, summary.Coverage
				m.lastConclusion = m.tr(menuNames[m.active]) + " · " + summary.Headline
			}
		}
		if previousRead != nil {
			m.updateResultContext(*previousRead)
			m.toast = m.tr(Words{"Pemeriksaan ulang selesai", "Repeat check finished"})
		}
		m.updateCritical()
		if m.visual.pose == mascot.Nod && replyPose(m.page) == mascot.Idle {
			m.visual.rest = mascot.Idle
			m.accent(false, true)
		} else {
			m.showReply()
		}
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
		if m.quitPending && !m.busy && !m.motionSaving {
			cmd = m.quit()
		}
	case tea.KeyPressMsg:
		previous := m.screen
		cmd = m.key(msg)
		if m.screen != previous && m.screen != reviewPlan {
			m.visual.focusUntil = time.Time{}
			if m.screen == fileInput {
				m.show(mascot.Listening, "", false)
			}
			m.accent(false, true)
		}
	default:
		if m.typing() {
			m.input, cmd = m.input.Update(msg)
		}
	}
	return m, cmd
}

func (m *Model) updateCritical() {
	for _, check := range m.page.Coverage {
		if check.Status != domain.Available {
			continue
		}
		switch check.ID {
		case workflow.SSDCheck:
			m.criticalSSD = false
		case workflow.DiskCheck:
			m.criticalDisk = false
		}
	}
	for _, f := range m.page.Findings {
		if f.Severity != diagnostics.Critical {
			continue
		}
		switch f.Category {
		case "ssd":
			m.criticalSSD = true
		case "disk":
			m.criticalDisk = true
		}
	}
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.quitPending {
		return nil
	}
	m.toast = ""
	key := msg.String()
	if key == "ctrl+c" || (key == "q" && (!m.typing() || !m.hasRoom())) {
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
	if !m.hasRoom() {
		return nil
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
		if m.screen != reviewPlan && m.access == nil && !m.options.Gallery {
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
		if m.screen == reviewPlan && (!m.reviewReady() || key == "pgup" || key == "pgdown") {
			w, h := m.bounds()
			room := m.bodyRoom(h, len(m.choices()))
			m.scroll = max(0, min(m.scroll+max(-room, min(room, step)), max(0, len(m.body(w))-room)))
			m.recordReview()
			return nil
		}
		if m.screen == help || key == "pgup" || key == "pgdown" || len(m.choices()) == 0 {
			m.scroll = max(0, min(m.scroll+step, m.maxScroll()))
		} else {
			previous := m.choice
			m.choice = max(0, min(m.choice+step, len(m.choices())-1))
			if m.choice != previous && m.screen != reviewPlan {
				if !m.options.Gallery {
					switch m.visual.pose {
					case mascot.Greeting, mascot.Idle, mascot.Blink, mascot.LookLeft, mascot.LookRight:
						pose := mascot.LookRight
						if step < 0 {
							pose = mascot.LookLeft
						}
						m.show(pose, mascot.Idle, false)
					}
				}
				m.accent(true, false)
			}
		}
	case "enter":
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
		if format == "gpu" {
			n, err := strconv.ParseInt(value, 10, 32)
			kind, id, ok := strings.Cut(m.parameter.Value, " ")
			m.screen = results
			if err != nil || !ok {
				m.choice, m.scroll = 0, 0
				m.page = Page{Availability: domain.Error, Found: []string{m.tr(Words{"Nilai integer tidak valid.", "Invalid integer value."})}}
				m.showReply()
				return nil
			}
			return m.start(assistant.GPU, Choice{Kind: kind, Value: id + "=" + strconv.FormatInt(n, 10)})
		}
		if format == "read" || format == "write" {
			m.screen = taskQuestion
			m.page = Page{Found: []string{m.tr(Words{"Uji lokasi ini?", "Test this location?"}), domain.SafeText(value),
				m.tr(Words{"16 MiB, maksimal 30 detik.", "16 MiB, up to 30 seconds."})},
				Choices: []Choice{{m.tr(Words{"Kembali", "Back"}), "back", ""}, {m.tr(Words{"Mulai pengujian", "Start workload"}), "benchmark-run", format}}}
			m.choice, m.scroll = 0, 0
			m.pendingOutput = value
			return nil
		}
		m.screen = results
		return m.launch(Request{Intent: assistant.Report, Choice: Choice{Kind: "export", Value: format}, Output: value,
			Overview: m.overview, Snapshot: m.page.Snapshot, Language: m.options.Language}, mascot.Scanning)
	}
	intents := assistant.Route(value)
	if len(intents) == 1 {
		if intent := intents[0]; intent != "" {
			m.returnTo()
			cmd := m.start(intent, Choice{})
			m.question = domain.SafeText(value)
			if m.busy && m.options.Motion == motion.Full && m.canAnimate() {
				m.show(mascot.Nod, m.activity, false)
				m.spinFor(m.activity)
			}
			return cmd
		}
	}
	m.screen = taskQuestion
	message := m.tr(copy.clarify)
	if len(intents) == 0 {
		message = m.tr(copy.unknown)
		intents = []assistant.Intent{assistant.Overview, assistant.Slow, assistant.Cleanup}
	}
	m.page = Page{Availability: domain.Unknown, Found: []string{message}}
	m.question = domain.SafeText(value)
	for _, id := range intents {
		m.page.Choices = append(m.page.Choices, Choice{m.tr(menuNames[id]), "navigate", string(id)})
	}
	if message == m.tr(copy.unknown) {
		m.page.Choices = append(m.page.Choices, Choice{m.tr(Words{"Pemeriksaan lain", "Other checks"}), "more", ""})
	}
	if m.options.Motion == motion.Full && m.canAnimate() {
		m.show(mascot.Nod, mascot.Thinking, false)
	} else {
		m.show(mascot.Thinking, "", false)
	}
	return nil
}

func (m *Model) quit() tea.Cmd {
	m.quitPending = true
	m.show(mascot.Farewell, "", false)
	if m.motionSaving {
		m.toast = m.tr(Words{"Menyimpan preferensi gerak", "Saving motion preference"})
		return nil
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
	if m.options.Privilege != nil {
		m.page.Choices = append(m.page.Choices, Choice{Label: m.tr(Words{"Tingkat pemeriksaan", "Inspection level"}), Kind: "check-level"})
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
	if m.options.Demo || m.busy {
		return false
	}
	var stamp time.Time
	if m.page.Summary != nil {
		stamp = m.page.Summary.At
	}
	if stamp.IsZero() {
		for _, finding := range m.page.Findings {
			if finding.At.After(stamp) {
				stamp = finding.At
			}
		}
	}
	if stamp.IsZero() {
		return false
	}
	var age time.Duration
	switch m.active {
	case assistant.Overview, assistant.Slow:
		age = 30 * time.Second
	case assistant.Power, assistant.Recommend:
		age = time.Minute
	case assistant.Storage:
		age = 30 * time.Minute
	case assistant.Trim:
		age = 15 * time.Minute
	}
	return age != 0 && m.now().Sub(stamp) > age
}

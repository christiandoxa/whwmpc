package tui

import (
	"context"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

type Authenticator func(context.Context, actions.Plan, string, io.Reader, io.Writer, io.Writer) error

type foregroundAuth struct {
	run            Authenticator
	ctx            context.Context
	plan           actions.Plan
	id             string
	stdin          io.Reader
	stdout, stderr io.Writer
}

func (a *foregroundAuth) Run() error {
	return a.run(a.ctx, a.plan, a.id, a.stdin, a.stdout, a.stderr)
}
func (a *foregroundAuth) SetStdin(r io.Reader)  { a.stdin = r }
func (a *foregroundAuth) SetStdout(w io.Writer) { a.stdout = w }
func (a *foregroundAuth) SetStderr(w io.Writer) { a.stderr = w }

type authMsg struct {
	job uint64
	err error
	req Request
	ctx context.Context
}

type scanMsg struct {
	job      uint64
	progress workflow.ScanProgress
}

type stageMsg struct {
	job   uint64
	stage actions.Stage
}

type ShutdownMsg struct{}

func (m *Model) launch(req Request, pose mascot.Pose) tea.Cmd {
	m.busy, m.cancelling, m.workName = true, false, ""
	m.activity = pose
	switch req.Choice.Kind {
	case "export":
		m.runningAction = true // retain the file result if cancellation arrives after the write
		m.workName = m.tr(Words{"Mengumpulkan bukti laporan", "Collecting report evidence"})
	case "save-scan":
		m.runningAction = true
		m.workName = m.tr(Words{"Menyimpan ringkasan lokal", "Saving local summary"})
	}
	if req.Intent == assistant.History && req.Choice.Kind == "" {
		m.workName = m.tr(Words{"Membaca riwayat lokal", "Reading local history"})
	}
	if req.Choice.Kind == "apply" {
		m.actionPose = pose
		m.activity = mascot.Confirm
	}
	m.job++
	job := m.job
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelJob = cancel
	m.show(m.activity, "", req.Choice.Kind != "apply")
	m.page = Page{}
	m.screen = scanning
	if req.Choice.Kind == "apply" {
		m.screen = executing
	}
	sender := m.options.Send
	if sender != nil {
		req.ScanProgress = func(progress workflow.ScanProgress) { sender(scanMsg{job, progress}) }
		req.Progress = func(stage actions.Stage) { sender(stageMsg{job, stage}) }
		req.Printing = func() { sender(printingMsg{job}) }
	}
	if req.Choice.Kind == "apply" {
		m.runningAction = true
		if req.Plan.Privilege == actions.Sudo && m.options.Authenticate != nil {
			m.workName = m.tr(Words{"Autentikasi sudo di terminal", "Authenticate with sudo in the terminal"})
			adapter := &foregroundAuth{run: m.options.Authenticate, ctx: ctx, plan: *req.Plan, id: req.Choice.Value}
			return tea.Exec(adapter, func(err error) tea.Msg { return authMsg{job, err, req, ctx} })
		}
	}
	return m.doRequest(ctx, job, req)
}

func (m Model) doRequest(ctx context.Context, job uint64, req Request) tea.Cmd {
	return func() tea.Msg {
		if m.inspect == nil && req.Intent == assistant.Overview && (req.Choice.Kind == "" || req.Choice.Kind == "rescan") {
			return replyMsg{job, m.overviewFindingPage(m.source.Overview(ctx))}
		}
		return replyMsg{job, m.inspect(ctx, req)}
	}
}

func (m *Model) updateStage(msg stageMsg) {
	if msg.job != m.job || !m.busy || m.cancelling {
		return
	}
	switch msg.stage {
	case actions.Revalidating:
		m.workName = m.tr(Words{"Memeriksa ulang target dan kondisi awal", "Revalidating target and preconditions"})
		m.activity = mascot.Scanning
		m.show(m.activity, "", true)
	case actions.Executing:
		m.workName = ""
		m.activity = m.actionPose
		m.show(m.activity, "", true)
	case actions.Verifying:
		m.workName = m.tr(Words{"Memeriksa hasil aktual", "Verifying actual state"})
		m.activity = mascot.Scanning
		m.show(m.activity, "", true)
	}
	m.accent(false, true)
}

func (m *Model) cancelCurrent() bool {
	if !m.busy || m.cancelJob == nil {
		return false
	}
	if !m.cancelling {
		m.cancelJob()
		m.cancelling = true
		m.workName = displayStage(mascot.Cancelled, m.options.Language)
		m.show(mascot.Cancelled, "", false)
	}
	return true
}

func aimFor(intent assistant.Intent, choice Choice, plan *actions.Plan) mascot.Pose {
	if choice.Kind == "apply" && plan != nil {
		switch plan.Kind {
		case actions.CleanAPT:
			return mascot.Cleaning
		case actions.RestorePower, actions.RestoreTrimTimer:
			return mascot.Restore
		default:
			return mascot.Applying
		}
	}
	switch choice.Kind {
	case actions.PowerProfile, actions.TrimMount, actions.EnableTrimTimer, actions.CleanAPT, "restore":
		return mascot.Preview
	default:
		return poseFor(intent)
	}
}

func previewAction(ctx context.Context, m *workflow.Maintenance, req Request) (Page, bool) {
	switch req.Choice.Kind {
	case actions.PowerProfile, actions.TrimMount, actions.CleanAPT, actions.EnableTrimTimer:
		p, err := m.Preview(ctx, req.Choice.Kind, req.Choice.Value, false)
		if err != nil {
			return unavailableAction(err, req.Language), true
		}
		return planPage(p, req.Language), true
	case "restore":
		p, err := m.Restore(ctx, req.Choice.Value)
		if err != nil {
			return unavailableAction(err, req.Language), true
		}
		return planPage(p, req.Language), true
	case "apply":
		if req.Plan == nil {
			return unavailableAction(fmt.Errorf("rencana belum siap"), req.Language), true
		}
		outcome, _ := m.Execute(ctx, *req.Plan, req.Choice.Value, req.Progress)
		return resultPage(outcome, req.Language, req.Intent), true
	default:
		return Page{}, false
	}
}

func unavailableAction(err error, lang string) Page {
	l := translator(lang)
	return Page{
		Availability: domain.Error, Pose: mascot.Error,
		Found:  []string{l("Tindakan belum dimulai.", "The action has not started."), domain.SafeText(err.Error())},
		Advice: []string{l("Periksa kembali capability atau buat preview baru.", "Review capabilities or open a new preview.")},
	}
}

func cancellationPage(lang string, intent assistant.Intent) Page {
	l := translator(lang)
	return Page{
		Availability: domain.Unknown, Pose: mascot.Cancelled,
		Found:   []string{l("Dibatalkan.", "Cancelled.")},
		Meaning: []string{l("Tindakan sistem belum dimulai.", "The system action has not started.")},
		Choices: []Choice{{Label: l("Kembali ke pemeriksaan", "Return to the check"), Kind: "navigate", Value: string(intent)}},
	}
}

func planPage(plan actions.Plan, lang string) Page {
	l := translator(lang)
	rollback := l("Tidak tersedia", "Unavailable")
	if plan.Rollback {
		rollback = l("Bisa ditinjau setelah perubahan terverifikasi", "Can be previewed after a verified change")
	}
	label := l("Rencana", "Plan")
	found := []string{
		fmt.Sprintf("%s · %s", label, plan.Kind),
		l("Target", "Target") + " · " + plan.Target,
		l("Risiko", "Risk") + " · " + plan.Risk,
		l("Izin", "Privilege") + " · " + plan.Privilege,
		l("Kedaluwarsa", "Expires") + " · " + plan.ExpiresAt.Format("15:04:05 MST"),
	}
	if plan.EstimatedBytes != nil {
		found = append(found, l("Estimasi teramati", "Observed estimate")+" · "+bytesText(*plan.EstimatedBytes))
	}
	evidence := []string{
		"ID · " + plan.ID,
		fmt.Sprintf("%s · %d", l("Versi", "Version"), plan.Version),
		l("Operasi persis", "Exact operation") + " · " + fmt.Sprintf("%q", plan.Operation),
		l("Kondisi awal", "Preconditions") + " · " + plan.Initial,
		l("Pemulihan", "Restore") + " · " + rollback,
	}
	advice := append([]string(nil), evidence...)
	page := Page{
		Availability: domain.Available, Pose: mascot.Preview, Plan: &plan,
		Found: found, Meaning: []string{plan.Impact},
		Advice: advice, Evidence: append(append([]string(nil), found...), evidence...),
		Choices: []Choice{
			{Label: l("Batalkan", "Cancel"), Kind: "cancel-plan"},
			{Label: l("Lanjut ke konfirmasi", "Continue to confirmation"), Kind: "confirm-plan", Value: plan.ID},
		},
	}
	if plan.Kind == actions.CleanAPT {
		page.Pose = mascot.Broom
	}
	return page
}

func (m *Model) confirmPage() {
	l := translator(m.options.Language)
	page := &m.page
	page.Pose, page.Confirm = mascot.Confirm, true
	page.Choices = []Choice{
		{Label: l("Batalkan", "Cancel"), Kind: "cancel-plan"},
		{Label: l("Terapkan rencana ini", "Apply this plan"), Kind: "apply", Value: page.Plan.ID},
	}
	m.choice, m.scroll = 0, 0
	m.showReply()
}

func resultPage(outcome workflow.Outcome, lang string, intent assistant.Intent) Page {
	l := translator(lang)
	result := outcome.Result
	page := Page{Availability: domain.Available}
	state := l("Hasil belum diketahui", "Outcome unknown")
	switch result.Status {
	case actions.Verified:
		page.Pose, state = mascot.Success, l("Terverifikasi", "Verified")
		if result.Plan.Kind == actions.PowerProfile && result.Changed != nil && *result.Changed {
			switch result.Plan.Target {
			case "performance":
				page.Pose = mascot.Performance
			case "power-saver":
				page.Pose = mascot.Saver
			case "balanced":
				page.Pose = mascot.Balanced
			}
		}
	case actions.Partial:
		page.Availability, page.Pose, state = domain.Partial, mascot.Warning, l("Terverifikasi sebagian", "Partially verified")
	case actions.NotStarted:
		page.Availability, page.Pose, state = domain.Error, mascot.Error, l("Belum dimulai", "Not started")
		if result.CancellationRequested {
			page.Availability, page.Pose = domain.Unknown, mascot.Cancelled
		}
	case actions.Failed:
		page.Availability, page.Pose, state = domain.Error, mascot.Error, l("Gagal", "Failed")
	default:
		page.Availability, page.Pose = domain.Unknown, mascot.Warning
	}
	page.Found = []string{state + " · " + result.Plan.Kind, result.Message}
	if result.Status == actions.Verified &&
		(result.Plan.Kind == actions.PowerProfile || result.Plan.Kind == actions.RestorePower) {
		page.Found = append(page.Found, l("Profil aktif", "Active profile")+" · "+result.Plan.Target)
		page.Meaning = []string{l("Profil daemon terverifikasi. Kecepatan pekerjaan perlu diukur terpisah.", "Daemon profile verified. Workload speed requires separate measurement.")}
	}
	if result.CancellationRequested {
		page.Found = append(page.Found, l("Penghentian diminta.", "Cancellation requested."))
		if result.Status != actions.NotStarted {
			page.Pose = mascot.Cancelled
		}
	}
	if result.Changed != nil {
		if *result.Changed {
			page.Found = append(page.Found, l("Perubahan teramati.", "Change observed."))
		} else {
			page.Found = append(page.Found, l("Keadaan awal masih teramati.", "Initial state is still observed."))
		}
	}
	if result.PotentialDiscardBytes != nil {
		page.Found = append(page.Found, bytesText(*result.PotentialDiscardBytes)+" "+l("berpotensi diajukan untuk discard", "potentially submitted for discard"))
	}
	if result.RemovedBytes != nil {
		page.Found = append(page.Found, bytesText(*result.RemovedBytes)+" "+l("teramati terhapus", "observed removed"))
	}
	if result.FreeSpaceDelta != nil {
		page.Found = append(page.Found, fmt.Sprintf("%+d B · %s", *result.FreeSpaceDelta, l("perubahan ruang bebas filesystem arsip", "archive filesystem free-space change")))
	}
	if outcome.HistoryErr != nil {
		page.Advice = append(page.Advice, l("Riwayat gagal disimpan", "Could not save history")+" · "+domain.SafeText(outcome.HistoryErr.Error()))
	}
	if result.Status == actions.Unknown || result.Status == actions.Partial {
		page.Advice = append(page.Advice, l("Periksa bukti aktual sebelum mempertimbangkan tindakan baru.", "Check current evidence before considering another action."))
	}
	page.Evidence = []string{"ID · " + result.Plan.ID, result.FinishedAt.Format("2006-01-02 15:04:05 MST")}
	page.Choices = []Choice{{Label: l("Periksa ulang", "Scan again"), Kind: "navigate", Value: string(intent)}}
	if result.Receipt != "" && outcome.HistoryErr == nil {
		page.Choices = append(page.Choices, Choice{Label: l("Tinjau pemulihan", "Preview restore"), Kind: "restore", Value: result.Plan.ID})
	}
	return page
}

func cleanupPage(ctx context.Context, m *workflow.Maintenance, lang string) Page {
	l := translator(lang)
	inv, err := m.Cleanup(ctx)
	page := Page{Availability: inv.Availability}
	if page.Availability == "" {
		page.Availability = domain.Error
	}
	page.Found = []string{fmt.Sprintf("%s · %s", inv.Category, statusNames[page.Availability].Text(lang))}
	if err != nil {
		page.Found = append(page.Found, domain.SafeText(err.Error()))
	}
	if len(inv.Scope) > 0 {
		page.Pose = mascot.Broom
		page.Found = append(page.Found, fmt.Sprintf("%s · %d %s", bytesText(inv.KnownBytes), inv.KnownFiles, l("file teramati", "observed files")))
		page.Meaning = []string{inv.Impact, l("Paket yang sudah terpasang tetap tersedia. Isi cache yang dihapus perlu diunduh ulang.", "Installed packages stay available. Removed cache contents may need to be downloaded again.")}
		page.Advice = []string{l("Seluruh kategori cache APT. Penghapusan permanen.", "Entire APT cache category. Permanent deletion.")}
		page.Evidence = append(page.Evidence, inv.Scope...)
	}
	if len(inv.Unknown) > 0 {
		page.Evidence = append(page.Evidence, l("Cakupan belum terukur", "Unmeasured scope"))
		page.Evidence = append(page.Evidence, inv.Unknown...)
	}
	if inv.Detail != "" {
		page.Evidence = append(page.Evidence, inv.Detail)
	}
	if m.Available() == nil && len(inv.Scope) > 0 && (inv.Availability == domain.Available || inv.Availability == domain.Partial) {
		page.Choices = []Choice{{Label: l("Tinjau seluruh cache APT", "Preview entire APT cache"), Kind: actions.CleanAPT}}
	}
	if m.Demo {
		page.Advice = append(page.Advice, l("DEMO · tindakan terkunci.", "DEMO · actions locked."))
	}
	return page
}

func displayTime(t time.Time, format string) string {
	if t.IsZero() {
		return "?"
	}
	return t.Format(format)
}

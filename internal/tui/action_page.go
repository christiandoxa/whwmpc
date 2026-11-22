package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/report"
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
	if m.options.Privilege != nil && !m.accessChosen && startsInspection(req) {
		m.privilegeChoices(readAccess{purpose: firstReads, scope: readScope(req.Intent), request: req})
		return nil
	}
	request := req
	m.request = &request
	m.busy, m.cancelling, m.workName = true, false, ""
	m.activity = pose
	if req.ObserveFor > 0 && req.Complete == "" {
		m.workName = m.observationLabel(0, req.ObserveFor)
	}
	switch req.Choice.Kind {
	case "benchmark-run":
		m.runningAction = true
		m.workName = m.tr(Words{"Menjalankan beban yang dipilih", "Running the selected workload"})
	case "export":
		m.runningAction = true // retain the file result if cancellation arrives after the write
		m.workName = m.tr(Words{"Mengumpulkan bukti laporan", "Collecting report evidence"})
	case "save-scan":
		m.runningAction = true
		m.workName = m.tr(Words{"Menyimpan ringkasan lokal", "Saving local summary"})
	case "firmware-resume":
		m.runningAction = true
		m.workName = m.tr(Words{"Memverifikasi hasil fwupd", "Verifying the fwupd result"})
	}
	if req.Intent == assistant.History && (req.Choice.Kind == "" || req.Choice.Kind == "rescan") {
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
		if !req.SimulateCleanup && req.Plan.Privilege == actions.Sudo && m.options.Authenticate != nil {
			m.workName = m.tr(Words{"Autentikasi sudo di terminal", "Authenticate with sudo in the terminal"})
			adapter := &foregroundAuth{run: m.options.Authenticate, ctx: ctx, plan: *req.Plan, id: req.Choice.Value}
			return tea.Exec(adapter, func(err error) tea.Msg { return authMsg{job, err, req, ctx} })
		}
	}
	return m.doRequest(ctx, job, req)
}

const demoCleanupID = "demo-cleanup"

func (m Model) demoCleanupAllowed(p *actions.Plan) bool {
	return m.options.Demo && m.options.DemoCleanupResult && m.active == assistant.Cleanup && p != nil && p.ID == demoCleanupID && p.Kind == actions.CleanAPT
}

func (m *Model) repeatCheck(req Request) tea.Cmd {
	origin := m.screenState
	m.beforeRead = &origin
	m.readErr = nil
	cmd := m.launch(req, aimFor(req.Intent, req.Choice, req.Plan))
	if m.busy && m.workName == "" {
		m.workName = m.tr(Words{"Memeriksa data terbaru", "Reading new measurements"})
	}
	return cmd
}

func (m Model) repeatFailure(p Page) string {
	if m.cancelling {
		return m.tr(Words{"Pemeriksaan dihentikan", "Check stopped"})
	}
	if m.request != nil && m.request.Complete != "" && m.options.Privilege != nil {
		st := m.options.Privilege.State()
		if !st.Active && st.Err != nil && needsRead(p, permissionChecks(p, m.active), m.request.Complete) {
			return privilegeReason(st, m.options.Language)
		}
	}
	if p.Snapshot == nil && p.Availability == domain.Error {
		return strings.Join(p.Found, " · ")
	}
	return ""
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
		case actions.CleanAPT, actions.CleanPacman, actions.CleanDNF:
			return mascot.Cleaning
		case actions.RestorePower, actions.RestoreTrimTimer, hardware.RestorePowerLimit, hardware.RestoreCoreMax, hardware.RestoreVoltageOffset:
			return mascot.Restore
		default:
			return mascot.Applying
		}
	}
	switch choice.Kind {
	case "firmware-verify", "firmware-resume":
		return mascot.Scanning
	case actions.PowerProfile, actions.TrimMount, actions.EnableTrimTimer, actions.CleanAPT, actions.CleanPacman, actions.CleanDNF,
		hardware.PowerLimit, hardware.CoreMax, hardware.VoltageOffset, actions.FirmwareUpdate, actions.FirmwareRefresh, actions.FilesystemRepair, "restore":
		return mascot.Preview
	default:
		return poseFor(intent)
	}
}

func previewAction(ctx context.Context, m *workflow.Maintenance, req Request) (Page, bool) {
	if m.Demo && req.Choice.Kind == actions.CleanAPT {
		l := translator(req.Language)
		inv, _ := m.Cleanup(ctx)
		return planPage(actions.Plan{ID: demoCleanupID, Kind: actions.CleanAPT, Target: inv.Category,
			Initial: strings.Join(inv.Scope, ", "),
			Impact:  l("Paket yang diperlukan akan diunduh lagi.", "Packages will be downloaded again as needed."),
			Risk:    l("Penghapusan permanen pada aksi nyata. DEMO memakai fixture.", "Real actions permanently delete cache. DEMO uses fixtures."), EstimatedBytes: &inv.KnownBytes}, req.Language), true
	}

	switch req.Choice.Kind {
	case actions.PowerProfile, actions.TrimMount, actions.CleanAPT, actions.CleanPacman, actions.CleanDNF, actions.EnableTrimTimer, hardware.PowerLimit, hardware.CoreMax, hardware.VoltageOffset, actions.FirmwareUpdate, actions.FirmwareRefresh, actions.FilesystemRepair:
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
		var outcome workflow.Outcome
		if m.Demo {
			if !req.SimulateCleanup || req.Intent != assistant.Cleanup || req.Plan.Kind != actions.CleanAPT || req.Plan.ID != demoCleanupID || req.Choice.Value != demoCleanupID {
				return unavailableAction(m.Available(), req.Language), true
			}
			outcome = demoCleanupOutcome(ctx, req, m)
		} else {
			outcome, _ = m.Execute(ctx, *req.Plan, req.Choice.Value, req.Progress)
		}
		page := resultPage(outcome, req.Language, req.Intent)
		page.Snapshot = &workflow.Snapshot{Input: report.Input{At: outcome.Result.FinishedAt, Demo: m.Demo, Actions: []actions.Result{outcome.Result}}}
		return page, true
	default:
		return Page{}, false
	}
}

func demoCleanupOutcome(ctx context.Context, req Request, maintenance *workflow.Maintenance) workflow.Outcome {
	l := translator(req.Language)
	r := actions.Result{Plan: *req.Plan, FinishedAt: platform.DemoTime, Status: actions.NotStarted}
	before, _ := maintenance.Cleanup(ctx)
	if req.Plan.EstimatedBytes == nil || *req.Plan.EstimatedBytes != before.KnownBytes || req.Plan.Initial != strings.Join(before.Scope, ", ") {
		r.Message = l("DEMO · Cakupan fixture berubah. Tinjau ulang.", "DEMO · Fixture scope changed. Review it again.")
		return workflow.Outcome{Result: r}
	}
	for _, stage := range []actions.Stage{actions.Revalidating, actions.Executing, actions.Verifying} {
		if ctx.Err() != nil {
			r.CancellationRequested = true
			r.Message = l("Simulasi dihentikan.", "Simulation stopped.")
			return workflow.Outcome{Result: r}
		}
		if req.Progress != nil {
			req.Progress(stage)
		}
	}
	after := actions.Cleanup{Availability: domain.Available, Scope: before.Scope}
	r.Status, r.Started, r.Changed = actions.Verified, true, new(before.KnownFiles != after.KnownFiles)
	r.RemovedBytes, r.FreeSpaceDelta = new(before.KnownBytes-after.KnownBytes), new(int64(344<<20))
	r.Message = l("DEMO · Cakupan cache fixture diperiksa setelah simulasi.", "DEMO · Fixture cache scope was checked after simulation.")
	return workflow.Outcome{Result: r}
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

func actionLabel(kind, lang string) string {
	l := translator(lang)
	switch kind {
	case actions.CleanAPT, actions.CleanPacman, actions.CleanDNF:
		return l("Bersihkan cache paket", "Clean package cache")
	case actions.FirmwareUpdate:
		return l("Jadwalkan pembaruan firmware", "Schedule firmware update")
	case actions.FirmwareRefresh:
		return l("Unduh katalog LVFS", "Refresh LVFS catalog")
	case actions.FilesystemRepair:
		return l("Perbaiki partisi offline", "Repair offline partition")
	case hardware.PowerLimit:
		return l("Ubah batas daya GPU", "Change GPU power limit")
	case hardware.CoreMax:
		return l("Ubah clock maksimum GPU", "Change maximum GPU clock")
	case hardware.VoltageOffset:
		return l("Ubah offset voltase GPU", "Change GPU voltage offset")
	case hardware.RestorePowerLimit, hardware.RestoreCoreMax, hardware.RestoreVoltageOffset:
		return l("Pulihkan kontrol GPU", "Restore GPU control")
	case actions.EnableTrimTimer:
		return l("Aktifkan jadwal TRIM", "Enable TRIM schedule")
	case actions.RestoreTrimTimer:
		return l("Pulihkan jadwal TRIM", "Restore TRIM schedule")
	case actions.TrimMount:
		return l("Jalankan TRIM", "Run TRIM")
	case actions.PowerProfile:
		return l("Ganti profil daya", "Change power profile")
	case actions.RestorePower:
		return l("Pulihkan profil daya", "Restore power profile")
	default:
		return l("Jalankan tindakan", "Run action")
	}
}

func planPage(plan actions.Plan, lang string) Page {
	l := translator(lang)
	action := actionLabel(plan.Kind, lang)
	after := plan.Target
	permission := l("Administrator melalui kebijakan sistem", "Administrator through system policy")
	if plan.ID == demoCleanupID {
		permission = l("DEMO · izin sistem tidak diminta", "DEMO · no system authorization prompt")
	} else if plan.Privilege == actions.Sudo {
		permission = l("Administrator melalui sudo", "Administrator through sudo")
	}
	restore := l("Tidak bisa dipulihkan", "Cannot be undone")
	if plan.Rollback {
		restore = l("Dapat ditinjau untuk pemulihan setelah verifikasi", "Restore can be reviewed after verification")
	}
	switch plan.Kind {
	case actions.FirmwareUpdate:
		after = l("verifikasi versi setelah restart", "verify the version after restart")
	case actions.FirmwareRefresh:
		after = l("cache metadata diperbarui", "metadata cache refreshed")
	case actions.FilesystemRepair:
		after = l("metadata diperbaiki bila e2fsck menilainya aman; verifikasi ulang", "repair metadata when e2fsck can do so; recheck")
	case actions.CleanAPT, actions.CleanPacman, actions.CleanDNF:
		after = l("cache paket dalam cakupan dihapus permanen", "package cache in scope is permanently deleted")
	case actions.EnableTrimTimer:
		after = l("jadwal aktif", "schedule active")
	case actions.TrimMount:
		after = l("blok yang tidak terpakai diajukan untuk discard", "unused blocks submitted for discard")
	case actions.RestoreTrimTimer:
		after = l("jadwal kembali ke kondisi sebelumnya", "schedule restored to its previous state")
	}
	found := []string{action,
		l("Target", "Target") + " · " + plan.Target,
		l("Sebelum", "Before") + " · " + plan.Initial,
		l("Sesudah", "After") + " · " + after,
		l("Dampak", "Impact") + " · " + plan.Impact,
		l("Risiko", "Risk") + " · " + plan.Risk,
		l("Izin", "Permission") + " · " + permission,
		l("Pemulihan", "Recovery") + " · " + restore,
	}
	if !plan.ExpiresAt.IsZero() {
		found = append(found, l("Berlaku hingga", "Valid until")+" · "+plan.ExpiresAt.Format("15:04:05 MST"))
	}
	if plan.EstimatedBytes != nil {
		found = append(found, l("Estimasi teramati", "Observed estimate")+" · "+bytesText(*plan.EstimatedBytes))
	}
	return Page{Availability: domain.Available, Pose: mascot.Confirm, Plan: &plan, Found: found,
		Choices: []Choice{{l("Kembali", "Back"), "cancel-plan", ""}, {action, "apply", plan.ID}}}
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
	page.Found = []string{state + " · " + actionLabel(result.Plan.Kind, lang), result.Message}
	s := &ResultSummary{Headline: page.Found[0], At: result.FinishedAt, Primary: finishChoice(lang)}
	if result.Status == actions.Verified && intent == assistant.Cleanup {
		s.Headline = l("Pembersihan cache terverifikasi", "Cache cleanup verified")
	}
	page.Summary = s
	if result.Message != "" {
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Verifikasi", "Verification"), Value: result.Message, Availability: page.Availability})
	}
	if result.Status == actions.Verified &&
		(result.Plan.Kind == actions.PowerProfile || result.Plan.Kind == actions.RestorePower) {
		page.Found = append(page.Found, l("Profil aktif", "Active profile")+" · "+result.Plan.Target)
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Profil aktif", "Active profile"), Value: result.Plan.Target, Availability: domain.Available})
		page.Meaning = []string{l("Profil daemon terverifikasi. Kecepatan pekerjaan perlu diukur terpisah.", "Daemon profile verified. Workload speed requires separate measurement.")}
	}
	if result.CancellationRequested {
		page.Found = append(page.Found, l("Penghentian diminta.", "Cancellation requested."))
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Penghentian", "Cancellation"), Value: l("Diminta", "Requested"), Availability: domain.Available})
		if result.Status != actions.NotStarted {
			page.Pose = mascot.Cancelled
		}
	}
	if result.Changed != nil {
		changed := l("Keadaan awal masih teramati.", "Initial state is still observed.")
		if *result.Changed {
			changed = l("Perubahan teramati.", "Change observed.")
		}
		page.Found = append(page.Found, changed)
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Keadaan", "State"), Value: changed, Availability: domain.Available})
	}
	if v := result.Plan.EstimatedBytes; v != nil {
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Estimasi awal", "Initial estimate"), Value: bytesText(*v), Availability: domain.Available})
	}
	if result.PotentialDiscardBytes != nil {
		page.Found = append(page.Found, bytesText(*result.PotentialDiscardBytes)+" "+l("berpotensi diajukan untuk discard", "potentially submitted for discard"))
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Potensi discard", "Potential discard"), Value: bytesText(*result.PotentialDiscardBytes), Availability: domain.Available})
	}
	if result.RemovedBytes != nil {
		page.Found = append(page.Found, bytesText(*result.RemovedBytes)+" "+l("teramati terhapus", "observed removed"))
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Teramati terhapus", "Observed removed"), Value: bytesText(*result.RemovedBytes), Availability: domain.Available})
	}
	if result.FreeSpaceDelta != nil {
		page.Found = append(page.Found, fmt.Sprintf("%+d B · %s", *result.FreeSpaceDelta, l("perubahan ruang bebas filesystem arsip", "archive filesystem free-space change")))
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Selisih ruang bebas", "Free space change"), Value: fmt.Sprintf("%+d", *result.FreeSpaceDelta), Unit: "B", Availability: domain.Available})
	}
	if outcome.HistoryErr != nil {
		page.Advice = append(page.Advice, l("Riwayat gagal disimpan", "Could not save history")+" · "+domain.SafeText(outcome.HistoryErr.Error()))
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Riwayat", "History"), Value: outcome.HistoryErr.Error(), Availability: domain.Error})
	}
	if result.Status == actions.Unknown || result.Status == actions.Partial {
		page.Advice = append(page.Advice, l("Periksa bukti aktual sebelum mempertimbangkan tindakan baru.", "Check current evidence before considering another action."))
		s.Advice = page.Advice[len(page.Advice)-1]
	}
	page.Evidence = []string{"ID · " + result.Plan.ID, result.FinishedAt.Format("2006-01-02 15:04:05 MST")}
	page.Choices = []Choice{{Label: l("Kembali ke ringkasan", "Return to summary"), Kind: "back"}}
	if result.Plan.Kind == actions.FirmwareUpdate && result.Started && (result.Status == actions.Partial || result.Status == actions.Unknown) {
		choice := Choice{l("Periksa versi setelah restart", "Verify version after restart"), "firmware-verify", result.Plan.Target}
		if outcome.HistoryErr == nil {
			choice.Kind, choice.Value = "firmware-resume", result.Plan.ID
		}
		page.Choices = append([]Choice{choice}, page.Choices...)
		s.Primary = choice
	}
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
	s := &ResultSummary{Headline: l("Belum ada cache terukur untuk dibersihkan", "No measurable cache to clean yet"), Primary: finishChoice(lang)}
	page.Summary = s
	page.Found = []string{fmt.Sprintf("%s · %s", inv.Category, statusNames[page.Availability].Text(lang))}
	if err != nil {
		page.Found = append(page.Found, domain.SafeText(err.Error()))
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Inventaris", "Inventory"), Value: err.Error(), Availability: page.Availability})
	}
	if inv.Category != "" {
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Kategori", "Category"), Value: inv.Category, Availability: page.Availability})
	}
	if len(inv.Scope) > 0 {
		page.Pose = mascot.Broom
		page.Found = append(page.Found, fmt.Sprintf("%s · %d %s", bytesText(inv.KnownBytes), inv.KnownFiles, l("file teramati", "observed files")))
		s.Metrics = []resultMetric{
			{Label: l("Potensi cleanup", "Cleanup estimate"), Value: bytesText(inv.KnownBytes), Availability: page.Availability},
			{Label: l("Cakupan terukur", "Measured scope"), Value: fmt.Sprintf(l("%d file teramati · %d target", "%d observed files · %d targets"), inv.KnownFiles, len(inv.Scope)), Availability: page.Availability},
		}
		page.Meaning = []string{inv.Impact, l("Paket yang sudah terpasang tetap tersedia. Isi cache yang dihapus perlu diunduh ulang.", "Installed packages stay available. Removed cache contents may need to be downloaded again.")}
		page.Advice = []string{l("Seluruh kategori cache yang ditawarkan. Penghapusan permanen.", "Entire displayed cache category. Permanent deletion.")}
		s.Advice = l("Cache yang dihapus permanen akan diunduh lagi saat diperlukan.", "Permanently removed cache can be downloaded again as needed.")
		page.Evidence = append(page.Evidence, inv.Scope...)
	}
	if inv.KnownBytes > 0 && inv.KnownFiles > 0 {
		s.Headline = fmt.Sprintf(l("%s cache bisa ditinjau untuk dibersihkan", "%s of cache to review for cleanup"), bytesText(inv.KnownBytes))
	}
	if len(inv.Unknown) > 0 {
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Belum terukur", "Unmeasured"), Value: fmt.Sprintf(l("%d bagian cakupan", "%d scope entries"), len(inv.Unknown)), Availability: domain.Unknown})
		page.Evidence = append(page.Evidence, l("Cakupan belum terukur", "Unmeasured scope"))
		page.Evidence = append(page.Evidence, inv.Unknown...)
	}
	if inv.Detail != "" {
		page.Evidence = append(page.Evidence, inv.Detail)
		if len(inv.Scope) == 0 && err == nil {
			s.Statuses = append(s.Statuses, resultStatus{Label: l("Inventaris", "Inventory"), Value: inv.Detail, Availability: page.Availability})
		}
	}
	if (m.Demo || m.Available() == nil) && len(inv.Scope) > 0 && inv.KnownBytes > 0 && inv.KnownFiles > 0 && hasData(inv.Availability) {
		kind := inv.Kind
		if kind == "" {
			kind = actions.CleanAPT
		}
		s.Primary = Choice{Label: l("Tinjau pembersihan", "Review cleanup"), Kind: kind}
	}
	return page
}

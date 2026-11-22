package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/christiandoxa/whwmpc/internal/assistant"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/privread"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

var readScopes = map[privread.Operation]Words{
	privread.SMART:       {"SSD / SMART", "Drive health / SMART"},
	privread.TRIM:        {"TRIM dan bukti", "TRIM evidence"},
	privread.Power:       {"CPU dan daya", "CPU and power policy"},
	privread.Temperature: {"Sensor suhu CPU", "CPU temperature sensors"},
	privread.GPU:         {"Telemetri GPU", "GPU telemetry"},
	privread.Firmware:    {"Versi firmware lokal", "Local firmware versions"},
	privread.Filesystem:  {"Inventaris filesystem", "Filesystem inventory"},
}

type accessPurpose uint8

const (
	firstReads accessPurpose = iota
	changeReads
)

type readAccess struct {
	purpose accessPurpose
	scope   privread.Scope
	request Request
}

func readScope(intent assistant.Intent) privread.Scope {
	switch intent {
	case assistant.Overview, assistant.Report:
		return privread.Check
	case assistant.Slow:
		return privread.Observe
	case assistant.Storage:
		return privread.Scope(privread.SMART)
	case assistant.Trim:
		return privread.Scope(privread.TRIM)
	case assistant.Power, assistant.Recommend:
		return privread.Scope(privread.Power)
	case assistant.GPU:
		return privread.Scope(privread.GPU)
	case assistant.Firmware:
		return privread.Scope(privread.Firmware)
	case assistant.Filesystem:
		return privread.Scope(privread.Filesystem)
	default:
		return ""
	}
}

func startsInspection(req Request) bool {
	switch req.Choice.Kind {
	case "export":
		return req.Snapshot == nil
	case "", "rescan", "workload":
		if req.Intent == assistant.Recommend {
			return req.Workload != ""
		}
		return readScope(req.Intent) != "" || req.Intent == assistant.Cleanup || req.Intent == assistant.Daemon
	default:
		return false
	}
}

func (m *Model) privilegeChoices(flow readAccess) {
	l := translator(m.options.Language)
	title := l("Pilih akses baca", "Choose read access")
	ordinary, admin := l("Gunakan akses biasa", "Use ordinary access"), l("Gunakan izin administrator", "Use administrator access")
	var description []string
	if flow.purpose == firstReads {
		m.active = flow.request.Intent
		title = m.tr(menuNames[m.active])
		ordinary, admin = l("Mulai tanpa administrator", "Start without administrator"), l("Mulai dengan izin administrator", "Start with administrator access")
		switch m.active {
		case assistant.Overview:
			description = append(description, l("Lihat kondisi komponen dan saran perawatan.", "See component condition and maintenance priorities."),
				l("Cakupan CPU, memori, disk, SSD, TRIM, dan daya.", "Checks CPU, memory, disk, SSD, TRIM, and power."))
		case assistant.Slow:
			description = append(description, l("Amati CPU, memori, proses, dan daya.", "Observe CPU, memory, processes, and power."))
		case assistant.Cleanup:
			description = append(description, l("Lihat cache dan perkiraan ruang yang bisa dibersihkan.", "See cache contents and estimated space to reclaim."))
		}
	}
	var labels []string
	for _, op := range m.options.Privilege.Operations() {
		if flow.scope.Allows(op) {
			labels = append(labels, m.tr(readScopes[op]))
		}
	}
	choices := []Choice{{ordinary, "ordinary", ""}}
	switch {
	case len(labels) == 0:
		description = append(description, l("Tugas ini memakai akses biasa.", "This task uses ordinary access."))
	case m.options.Privilege.Capability() != nil:
		description = append(description, l("Izin administrator tidak tersedia di sini.", "Administrator access is unavailable here."))
	default:
		description = append(description, l("Akses admin baca hingga 10 menit", "Administrator reads for up to 10 minutes")+" · "+strings.Join(labels, ", "))
		choices = append([]Choice{{admin, "deep-authorize", ""}}, choices...)
		if flow.purpose == firstReads {
			description = append(description, l("Izin batal/ditolak → lanjut dengan akses biasa.", "Denied or cancelled? Continue with ordinary access."))
		}
	}
	m.screen, m.access, m.choice, m.scroll = taskQuestion, &flow, len(choices)-1, 0
	m.page = Page{Found: []string{title}, Meaning: description, Choices: choices,
		Advice: []string{l("Perubahan sistem ditinjau terpisah.", "System changes have a separate review.")}}
	m.show(mascot.Confirm, "", false)
}

type foregroundReads struct {
	ctx    context.Context
	access *privread.Session
	scope  privread.Scope
	stdin  *os.File
	stderr io.Writer
}

func (a *foregroundReads) Run() error {
	if a.stdin == nil {
		return privread.ErrUnavailable
	}
	return a.access.Enable(a.ctx, a.scope, a.stdin, a.stderr)
}
func (a *foregroundReads) SetStdin(in io.Reader)      { a.stdin, _ = in.(*os.File) }
func (a *foregroundReads) SetStdout(io.Writer)        {}
func (a *foregroundReads) SetStderr(stderr io.Writer) { a.stderr = stderr }

type privilegeMsg struct {
	job uint64
	err error
}

func (m *Model) authorizeReads() tea.Cmd {
	if m.access == nil || m.access.scope == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.job++
	job := m.job
	m.busy, m.cancelJob = true, cancel
	m.screen = scanning
	m.workName = m.tr(Words{"Menunggu izin administrator", "Waiting for administrator permission"})
	m.show(mascot.Confirm, "", false)
	return tea.Exec(&foregroundReads{ctx: ctx, access: m.options.Privilege, scope: m.access.scope}, func(err error) tea.Msg { return privilegeMsg{job, err} })
}

func (m *Model) finishReadAuthorization(err error) tea.Cmd {
	flow := m.access
	if flow == nil {
		return nil
	}
	m.access = nil
	m.accessChosen = true
	if flow.purpose == firstReads {
		m.readErr = err
		req := flow.request
		return m.launch(req, aimFor(req.Intent, req.Choice, req.Plan))
	}
	cmd := m.returnTo()
	if err != nil {
		m.toast = privilegeReason(privread.State{Err: err}, m.options.Language)
	}
	return cmd
}

func privilegeReason(s privread.State, lang string) string {
	l := translator(lang)
	switch {
	case errors.Is(s.Err, privread.ErrCancelled):
		return l("Autentikasi dibatalkan", "Authorization cancelled")
	case errors.Is(s.Err, privread.ErrExpired):
		return l("Izin baca kedaluwarsa", "Read access expired")
	case errors.Is(s.Err, privread.ErrUnavailable):
		return l("Autentikasi tidak tersedia", "Authorization unavailable")
	case errors.Is(s.Err, privread.ErrProtocol):
		return l("Komunikasi dengan helper gagal", "Read helper communication failed")
	case errors.Is(s.Err, context.DeadlineExceeded):
		return l("Waktu autentikasi habis", "Authorization timed out")
	default:
		return l("Izin baca belum diberikan", "Read access was not granted")
	}
}

func (m *Model) summarizeReads(s *ResultSummary) {
	if m.options.Privilege == nil || m.screen != results {
		return
	}
	switch m.active {
	case assistant.Overview, assistant.Slow, assistant.Storage, assistant.Power, assistant.Recommend,
		assistant.Trim, assistant.GPU, assistant.Firmware, assistant.Filesystem:
	default:
		return
	}
	l := translator(m.options.Language)
	st := m.options.Privilege.State()
	if m.readErr != nil && !st.Requested {
		st = privread.State{Requested: true, Availability: domain.PermissionRequired, Err: m.readErr}
	}
	if n := permissionChecks(m.page, m.active); n != 0 {
		s.Coverage += fmt.Sprintf(l(" · %d perlu izin", " · %d need permission"), n)
	}
	if st.Requested && !st.Active {
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Akses baca", "Read access"), Value: privilegeReason(st, m.options.Language), Availability: st.Availability})
		s.RetryRead = true
		m.page.Choices = appendChoice(m.page.Choices, m.retryReadChoice())
	}
}

func (m Model) retryReadChoice() Choice {
	return Choice{Label: m.tr(Words{"Coba izin baca lagi", "Retry read authorization"}), Kind: "deep-scope"}
}

func permissionChecks(page Page, intent assistant.Intent) int {
	needed := make(map[workflow.Check]bool)
	for _, check := range page.Coverage {
		if check.Status == domain.PermissionRequired {
			needed[check.ID] = true
		}
	}
	for _, f := range page.Findings {
		if f.Availability != domain.PermissionRequired {
			continue
		}
		var name workflow.Check
		switch f.Category {
		case "cpu", "memory", "temperature", "process", "thermal":
			name = workflow.SystemCheck
		case "ssd":
			name = workflow.SSDCheck
		case "trim":
			name = workflow.TRIMCheck
		case "power", "recommendation":
			name = workflow.PowerCheck
		default:
			name = workflow.Check(f.Category)
		}
		needed[name] = true
	}
	if page.Snapshot != nil {
		in := page.Snapshot.Input
		if o := in.Overview; o != nil && o.Temperature.Availability == domain.PermissionRequired {
			needed[workflow.SystemCheck] = true
		}
		if t := in.Trim; t != nil && t.Timer.Journal == domain.PermissionRequired {
			needed[workflow.TRIMCheck] = true
		}
		if p := in.Power; p != nil && slices.Contains([]domain.Availability{p.PowerStatus, p.Profiles.Status, p.CPUFreqStatus}, domain.PermissionRequired) {
			needed[workflow.PowerCheck] = true
		}
	}
	if len(needed) == 0 && page.Availability == domain.PermissionRequired {
		needed[workflow.Check(intent)] = true
	}
	return len(needed)
}

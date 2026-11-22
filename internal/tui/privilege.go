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

func (m *Model) privilegeChoices() {
	l := translator(m.options.Language)
	m.screenState = screenState{screen: taskQuestion, choice: 1, page: Page{
		Found: []string{l("Piko · Mau aku periksa komputermu lebih mendalam?", "Piko · Would you like me to check your PC more closely?")},
		Meaning: []string{l("Mode mendalam dapat membaca lebih banyak informasi hardware.", "Deep inspection can read more hardware information."),
			l("Perubahan konfigurasi tetap memerlukan persetujuanmu.", "Configuration changes still require your approval.")},
		Choices: []Choice{
			{l("Pemeriksaan mendalam · perlu izin administrator", "Deep inspection · administrator permission"), "deep-scope", ""},
			{l("Pemeriksaan biasa · tanpa izin administrator", "Ordinary inspection · standard access"), "ordinary", ""},
		},
	}}
}

func (m *Model) privilegeScope() {
	l := translator(m.options.Language)
	access := m.options.Privilege
	m.screenState = screenState{screen: taskQuestion, page: Page{
		Found: []string{l("Izinkan pemeriksaan mendalam?", "Allow deep inspection?")},
		Choices: []Choice{
			{l("Kembali", "Back"), "back", ""},
			{l("Izinkan akses baca", "Authorize read access"), "deep-authorize", ""},
		},
	}}
	if err := access.Capability(); err != nil {
		m.page.Meaning = []string{l("Autentikasi administrator tidak tersedia.", "Administrator authorization is unavailable."), domain.SafeText(err.Error())}
		m.page.Choices[1] = Choice{l("Lanjut dengan pemeriksaan biasa", "Continue with ordinary inspection"), "ordinary", ""}
		m.show(mascot.Unknown, "", false)
		return
	}
	var labels []string
	for _, op := range access.Operations() {
		labels = append(labels, m.tr(readScopes[op]))
	}
	for len(labels) > 0 {
		n := min(len(labels), 2)
		m.page.Meaning = append(m.page.Meaning, strings.Join(labels[:n], " · "))
		labels = labels[n:]
	}
	m.page.Advice = []string{l("Akses baca berlaku hingga 10 menit dalam sesi ini.", "Read access lasts up to 10 minutes in this session.")}
	m.show(mascot.Confirm, "", false)
}

type foregroundReads struct {
	ctx    context.Context
	access *privread.Session
	stdin  *os.File
	stderr io.Writer
}

func (a *foregroundReads) Run() error {
	if a.stdin == nil {
		return privread.ErrUnavailable
	}
	return a.access.Enable(a.ctx, privread.All, a.stdin, a.stderr)
}
func (a *foregroundReads) SetStdin(in io.Reader)      { a.stdin, _ = in.(*os.File) }
func (a *foregroundReads) SetStdout(io.Writer)        {}
func (a *foregroundReads) SetStderr(stderr io.Writer) { a.stderr = stderr }

type privilegeMsg struct {
	job uint64
	err error
}

func (m *Model) authorizeReads() tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.job++
	job := m.job
	m.busy, m.cancelJob = true, cancel
	m.screenState = screenState{screen: scanning}
	m.workName = m.tr(Words{"Menunggu izin administrator", "Waiting for administrator permission"})
	m.show(mascot.Confirm, "", false)
	return tea.Exec(&foregroundReads{ctx: ctx, access: m.options.Privilege}, func(err error) tea.Msg { return privilegeMsg{job, err} })
}

func (m *Model) privilegeHome() {
	m.back = nil
	m.overview = domain.Overview{}
	m.homePage()
	m.showReply()
	if st := m.options.Privilege.State(); st.Requested && !st.Active {
		m.show(mascot.Unknown, "", false)
	}
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
	if n := permissionChecks(m.page, m.active); n != 0 {
		s.Coverage += fmt.Sprintf(l(" · %d perlu izin", " · %d need permission"), n)
	}
	if st.Requested && !st.Active {
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Akses baca", "Read access"), Value: privilegeReason(st, m.options.Language), Availability: st.Availability})
		s.RetryRead = true
		m.page.Choices = appendChoice(m.page.Choices, m.retryReadChoice())
	}
	if st.Active && m.active == assistant.Overview && (st.Reads > m.readStart || m.options.Demo) {
		if s.Headline == l("Tidak ada peringatan pada sampel ini", "No warnings in this sample") {
			s.Headline = l("Pemeriksaan mendalam selesai", "Deep inspection complete")
		}
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

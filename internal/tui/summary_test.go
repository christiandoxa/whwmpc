package tui

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/christiandoxa/whwmpc/internal/actions"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/storage"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func TestComponentSummariesKeepIndependentMeasurements(t *testing.T) {
	at := platform.DemoTime
	ssd := storage.SMARTAudit{CapturedAt: at, Availability: domain.Partial, Drives: []storage.Drive{
		{Model: "Other drive", CapturedAt: at, Availability: domain.Available, Health: &storage.SMART{Passed: new(true)}},
		{Model: "Affected drive", CapturedAt: at, Availability: domain.Partial, Health: &storage.SMART{
			Passed: new(false), CriticalWarning: new(uint64(2)), PercentageUsed: new(uint64(127)), MediaErrors: "9007199254740993",
		}},
	}}
	m := New(t.Context(), platform.Demo{}, nil, Options{Language: "en", Motion: "off"})
	m.screen, m.active = results, assistant.Storage
	p := Page{Availability: ssd.Availability, Summary: ssdSummary(ssd, "en"), Findings: (diagnostics.Analyzer{Language: "en", Now: at}).SMART(ssd)}
	s := summarize(p, m.active, "en")
	m.page.Summary = &s
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"1 critical", "Affected drive", "0x02", "127%", "above 100%", "9007199254740993", "Other drives", "View critical findings"} {
		if !strings.Contains(view, want) {
			t.Fatalf("SSD missing %q in\n%s", want, view)
		}
	}

	trim := storage.TrimAudit{CapturedAt: at, Availability: domain.Available, Mounts: []storage.Mount{{Point: "/", Discard: domain.Unsupported}},
		Timer: storage.Timer{Availability: domain.Available, ActiveState: "active", Journal: domain.PermissionRequired}}
	rows := trimSummary(trim, "en").Statuses
	if !strings.Contains(rows[0].Value, "0/1") || !strings.Contains(rows[1].Value, "active") || rows[2].Availability != domain.PermissionRequired {
		t.Fatalf("TRIM independently reported states: %+v", rows)
	}
	trim.Mounts[0].LastTrim = &storage.TrimRecord{RecordedAt: at.Add(-2 * time.Hour), PotentialDiscardBytes: 460 << 20}
	trim.Timer.LastServiceExit, trim.Timer.ServiceResult = "2026-11-22 09:41:00 UTC", "exit-code"
	rows = trimSummary(trim, "en").Statuses
	if rows[2].Severity != diagnostics.Warning || !strings.Contains(rows[2].Value, "exit-code") {
		t.Fatalf("older mount evidence hid latest failed service: %+v", rows)
	}
	native := storage.TrimAudit{Source: storage.WindowsTRIMSource, Availability: domain.Available, Policy: []storage.TrimPolicy{{Scope: "NTFS", Enabled: true}}}
	rows = trimSummary(native, "en").Statuses
	if rows[1].Value != "NTFS enabled" || rows[0].Availability != domain.Unknown || rows[2].Availability != domain.Unknown {
		t.Fatalf("native policy inferred support or completion: %+v", rows)
	}

	powerRows := powerSummary(power.Snapshot{PowerStatus: domain.Available, PowerSource: "ac", CPUFreqStatus: domain.Available,
		Profiles: power.Profiles{Status: domain.Available, Active: "balanced"}, Policies: []power.Policy{{Name: "policy0", ObservedKHz: new(uint64(833500)),
			AverageKHz: new(uint64(909000)), DriverKHz: new(uint64(2500000)), ConfigMaxKHz: new(uint64(3500000))}}}, "en")
	if powerRows.Metrics[0].Value != "balanced" || powerRows.Metrics[2].Label != "Observed clock" || powerRows.Metrics[2].Value != "833.5" ||
		powerRows.Metrics[3].Label != "Average clock" || powerRows.Metrics[4].Label != "Driver clock" || !strings.Contains(powerRows.Statuses[0].Value, "3500 MHz") {
		t.Fatalf("power summary confused types of clocks: %+v", powerRows)
	}
}

func TestCleanupOutcomeKeepsEstimateObservedBytesAndDelta(t *testing.T) {
	p := resultPage(workflow.Outcome{Result: actions.Result{
		Plan:   actions.Plan{Kind: actions.CleanAPT, EstimatedBytes: new(uint64(19 << 20))},
		Status: actions.Partial, RemovedBytes: new(uint64(2 << 20)), FreeSpaceDelta: new(int64(-100)), CancellationRequested: true,
	}}, "en", assistant.Cleanup)
	s := p.Summary
	if len(s.Metrics) != 3 || s.Metrics[0].Value != "19 MiB" || s.Metrics[1].Value != "2 MiB" || s.Metrics[2].Value != "-100" ||
		s.Metrics[2].Unit != "B" || s.Statuses[0].Value != "Requested" || !strings.Contains(s.Headline, "Partially verified") {
		t.Fatalf("cleanup result lost a measurement or claimed success: %+v", s)
	}
}

func TestOverviewMetricsPreserveMeasurements(t *testing.T) {
	const gib = 1 << 30
	at := platform.DemoTime
	o := domain.Overview{
		OS: "darwin", CPU: domain.Percent{Availability: domain.Available, Samples: []float64{35, 21, 0}, StartedAt: at.Add(-780 * time.Millisecond), CapturedAt: at},
		Memory: domain.Memory{Availability: domain.Partial, TotalBytes: 16 * gib, FreeBytes: 3 * gib, Use: domain.Percent{Availability: domain.Partial, Samples: []float64{81.25}}},
		Disks: []domain.Disk{
			{Availability: domain.Available, TotalBytes: 20 * gib, FreeBytes: 18 * gib, UsedPercent: 10},
			{Root: true, Availability: domain.Available, TotalBytes: 100 * gib, FreeBytes: 10 * gib, UsedPercent: 94},
		},
		Temperature: domain.Temperature{Availability: domain.Available, Celsius: new(math.Inf(1))},
	}
	rows := overviewMetrics(o, "en")
	if rows[0].Usage == nil || rows[0].Value != "0" || rows[0].Note != "3 samples / 780ms · 0–35%" {
		t.Fatalf("CPU = %+v", rows[0])
	}
	if rows[1].Value != "81.2" || rows[1].Availability != domain.Partial || !strings.Contains(rows[1].Note, "3.0 GiB / 16.0 GiB free, lower bound") {
		t.Fatalf("RAM = %+v", rows[1])
	}
	if rows[2].Value != "94" || rows[2].Note != "10.0 GiB available" || rows[3].Value != "" || rows[3].Availability != domain.Error {
		t.Fatalf("disk and temperature = %+v", rows[2:])
	}
	o.CPU.Samples[2] = math.NaN()
	o.Memory.Use = domain.Percent{Availability: domain.Available}
	o.Disks[1].FreeBytes = 101 * gib
	bad := overviewMetrics(o, "en")
	for i, status := range []domain.Availability{domain.Error, domain.Unknown, domain.Error} {
		if bad[i].Availability != status || bad[i].Usage != nil || bad[i].Value != "" {
			t.Fatalf("malformed metric %d = %+v", i, bad[i])
		}
	}
}

func TestCoverageAndCriticalSummary(t *testing.T) {
	page := Page{Coverage: []workflow.Coverage{{ID: "system", Status: domain.Available}, {ID: "system", Status: domain.Available},
		{ID: "ssd", Status: domain.PermissionRequired}, {ID: "power", Status: domain.Partial}, {ID: "disk", Status: domain.Error}, {ID: "trim"}}}
	for range 5 {
		page.Findings = append(page.Findings, diagnostics.Finding{Category: "ssd", Availability: domain.Available, Severity: diagnostics.Critical})
	}
	got := summarize(page, assistant.Overview, "en")
	if got.Coverage != "1 complete · 1 partial · 1 failed · 1 unavailable · 1 not run" || !strings.Contains(got.Headline, "5 critical") ||
		len(got.Highlights) != 3 || got.Primary.Kind != "details" {
		t.Fatalf("unexpected summary %+v", got)
	}
	for _, status := range []domain.Availability{domain.Unknown, domain.DependencyMissing, domain.PermissionRequired, domain.Partial, domain.Stale, domain.Error} {
		page.Coverage = []workflow.Coverage{{ID: "ssd", Status: status}}
		page.Findings = []diagnostics.Finding{{Category: "ssd", Availability: status}}
		got = summarize(page, assistant.Overview, "en")
		if strings.Contains(got.Headline, "No warnings") || !strings.Contains(got.Highlights[0], statusNames[status].EN) {
			t.Fatalf("%s produced %+v", status, got)
		}
	}
}

func TestQuickScanThenDetailsKeepsResultAndSelection(t *testing.T) {
	ctx := context.Background()
	scanner := inspection.New(true)
	store := state.Store{}
	handler := Inspector(scanner, store, workflow.NewMaintenance(store, true))
	calls := 0
	m := New(ctx, scanner, func(ctx context.Context, req Request) Page { calls++; return handler(ctx, req) }, Options{Demo: true, Motion: "off"})
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.screen != scanning {
		t.Fatal("not scanning")
	}
	m = step(m, cmd())
	if m.screen != results || len(m.page.Coverage) != 5 || !strings.Contains(m.View().Content, "Lihat semua hasil") {
		t.Fatalf("quick results missing: %+v", m.page.Summary)
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 36}} {
		m = step(m, size)
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"5 selesai", "22 Nov 09:41", "24%", "8 sampel / 800ms · 13–38%", "56%", "7.0 GiB / 16.0 GiB", "71%", "148.0 GiB", "54 °C", "SMART lolos 1/1", "Jadwal aktif", "balanced · AC", "Lihat semua hasil"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%dx%d missing %q in\n%s", size.Width, size.Height, want, view)
			}
		}
	}
	s := m.page.Summary
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen != detail {
		t.Fatal("detail expected")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.screen != results || m.page.Summary != s || m.choice != 0 || calls != 1 {
		t.Fatal("back changed snapshot or selection")
	}
}

func TestAdvancedMenuShowsDaemonStatusInDemo(t *testing.T) {
	scanner := inspection.New(true)
	store := state.Store{}
	m := New(t.Context(), scanner, Inspector(scanner, store, workflow.NewMaintenance(store, true)),
		Options{Demo: true, Motion: "off"})
	m.start(assistant.Overview, Choice{Kind: "more"})
	m.start(assistant.Overview, Choice{Kind: "advanced"})
	for _, choice := range m.page.Choices {
		if choice.Value == string(assistant.Daemon) {
			cmd := m.start(assistant.Overview, choice)
			m = step(m, cmd())
			if !strings.Contains(m.View().Content, "DEMO · status host tidak dibaca") {
				t.Fatalf("daemon demo status missing: %s", m.View().Content)
			}
			return
		}
	}
	t.Fatal("daemon choice missing in advanced menu")
}

func TestCriticalStatusRequiresCompleteRecheck(t *testing.T) {
	m := New(t.Context(), inspection.New(true), nil, Options{Language: "en", Motion: "off"})
	m.page.Findings = []diagnostics.Finding{
		{Category: "ssd", Severity: diagnostics.Critical},
		{Category: "disk", Severity: diagnostics.Critical},
	}
	m.updateCritical()
	m.page = Page{Coverage: []workflow.Coverage{
		{ID: workflow.DiskCheck, Status: domain.Available},
		{ID: workflow.SSDCheck, Status: domain.PermissionRequired},
	}}
	m.updateCritical()
	if m.criticalDisk || !m.criticalSSD || !strings.Contains(m.View().Content, "CRITICAL") {
		t.Fatal("critical SMART notice must survive an inaccessible recheck")
	}
	m.page.Coverage = []workflow.Coverage{{ID: workflow.SSDCheck, Status: domain.Available}}
	m.updateCritical()
	if m.criticalSSD || strings.Contains(m.View().Content, "CRITICAL") {
		t.Fatal("complete SMART recheck should clear the previous finding")
	}
}

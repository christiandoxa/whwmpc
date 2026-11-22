package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

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

package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
)

func TestGPUNumericEntryCreatesOnlyPreviewRequest(t *testing.T) {
	m := New(context.Background(), platform.Demo{}, func(_ context.Context, r Request) Page {
		if r.Choice.Kind != hardware.PowerLimit || r.Choice.Value != "GPU-example=100000" || r.Plan != nil {
			t.Fatalf("unexpected request %+v", r)
		}
		return Page{Found: []string{"fixture preview"}}
	}, Options{})
	m.active, m.screen = assistant.GPU, results
	m.page = Page{Choices: []Choice{{"100000–170000 mW", "gpu-value", hardware.PowerLimit + " GPU-example"}}}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen != fileInput {
		t.Fatal("missing explicit value screen")
	}
	m.input.SetValue("100000")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.screen != scanning || cmd == nil {
		t.Fatal("entry did not request preview")
	}
	m = step(m, cmd())
	if m.page.Found[0] != "fixture preview" {
		t.Fatal(m.page.Found)
	}
}

func TestGPUEvidenceKeepsPowerScopesAndLimitsDistinct(t *testing.T) {
	snapshot := hardware.Snapshot{Devices: []hardware.Device{
		{PCI: "0000:01:00.0", Metrics: hardware.Metrics{PowerScope: "board", PowerMilliW: new(int64(15000)), EnforcedMilliW: new(int64(160000))},
			Controls: []hardware.Control{{Kind: hardware.PowerLimit, Current: 170000, Min: 100000, Max: 170000, Unit: "mW"}}},
		{PCI: "0000:08:00.0", Metrics: hardware.Metrics{PowerScope: "soc", PowerMilliW: new(int64(5000))}},
	}}
	evidence := strings.Join(gpuInventoryPage(snapshot, false, "en").Evidence, "\n")
	for _, want := range []string{"GPU board power 15000 mW", "Enforced power limit 160000 mW", "requested 170000 mW", "SoC power 5000 mW"} {
		if !strings.Contains(evidence, want) {
			t.Errorf("GPU evidence missing %q:\n%s", want, evidence)
		}
	}
}

func TestInvalidGPUValueShowsErrorAndFocusesBack(t *testing.T) {
	m := New(t.Context(), platform.Demo{}, nil, Options{Language: "en"})
	m.active, m.screen, m.choice = assistant.GPU, results, 1
	m.page = Page{Choices: []Choice{
		{Label: "Another GPU control"},
		{"100000–170000 mW", "gpu-value", hardware.PowerLimit + " GPU-example"},
	}}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.input.SetValue("150W")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.busy || m.page.Availability != domain.Error || m.visual.pose != mascot.Error || m.choice != 0 ||
		!strings.Contains(m.View().Content, "Invalid integer value.") {
		t.Fatal("invalid GPU input skipped the error pose or left Back unfocused")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.choice != 1 || m.choices()[m.choice].Kind != "gpu-value" {
		t.Fatal("Back did not restore the selected GPU control")
	}
}

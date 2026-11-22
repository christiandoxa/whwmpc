package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/platform"
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

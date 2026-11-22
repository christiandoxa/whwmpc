package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func TestExportPickerUsesExplicitPathAndPiko(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := state.Store{ConfigDir: filepath.Join(dir, "config"), StateDir: filepath.Join(dir, "state")}
	scanner := inspection.New(true)
	inspector := Inspector(scanner, store, workflow.NewMaintenance(store, true))
	m := New(ctx, scanner, inspector, Options{Demo: true, Language: "en", Motion: "off"})
	m = step(m, overviewMsg{scanner.Overview(ctx)})
	m.start(assistant.Report, Choice{})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen != fileInput || m.exportType != "json" {
		t.Fatal("export did not request a user-reviewed path")
	}
	path := filepath.Join(dir, "fixture.json")
	m.input.SetValue(path)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.busy {
		t.Fatal("export did not spawn an async command")
	}
	m = step(m, printingMsg{m.job})
	if m.visual.pose != mascot.Printing {
		t.Fatal("printing stage did not update pose")
	}
	m = step(m, cmd())
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"source": "demo"`) || m.page.Pose != mascot.Printing ||
		!strings.Contains(strings.Join(m.page.Found, " "), path) {
		t.Fatalf("export state = %+v, read err = %v", m.page, err)
	}
}

func TestSaveDisplayedSnapshotWithoutScanner(t *testing.T) {
	ctx := context.Background()
	scanner := inspection.New(true)
	snapshot, err := workflow.Collect(ctx, scanner, scanner.Overview(ctx), "en", true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	page := exportReport(ctx, nil, state.Store{}, Request{Snapshot: &snapshot, Output: path, Choice: Choice{Value: "json"}}, true)
	data, err := os.ReadFile(path)
	if err != nil || page.Pose != mascot.Printing || strings.Contains(string(data), `"smart":`) {
		t.Fatalf("snapshot changed coverage or exported incorrectly %v", err)
	}
}

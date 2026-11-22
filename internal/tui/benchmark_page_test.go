package tui

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
)

func TestBenchmarkWithUnreadableFileHasNoMeasurement(t *testing.T) {
	store := state.Store{StateDir: filepath.Join(t.TempDir(), "state")}
	page := runBenchmark(t.Context(), inspection.New(true), store, Request{
		Language: "en", Choice: Choice{Kind: "benchmark-run", Value: "read"},
		Output: filepath.Join(t.TempDir(), "missing.bin"),
	}, false)
	if page.Availability != domain.Partial || !slices.Contains(page.Found, "No workload sample was measured.") {
		t.Fatalf("unexpected benchmark outcome: %+v", page)
	}
	entries, err := store.History()
	if err != nil || len(entries) > 0 {
		t.Fatalf("benchmark created a history entry without a measurement: %v, %v", entries, err)
	}
}

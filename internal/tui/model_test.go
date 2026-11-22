package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
)

func keyPress(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: []rune(s)[0], Text: s} }

func step(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestInputFocusAndClarification(t *testing.T) {
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{Demo: true, Theme: "terminal", Motion: "off"})
	m = step(m, overviewMsg{platform.Demo{}.Overview(ctx)})
	m = step(m, keyPress("/"))
	for _, r := range "ssd trim q?d" {
		m = step(m, keyPress(string(r)))
	}
	if got := m.input.Value(); got != "ssd trim q?d" || m.help || m.detail {
		t.Fatalf("input = %q, model=%+v", got, m)
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.focus != pageFocus || len(m.page.Choices) != 2 || m.page.Choices[0].Value != string(assistant.Storage) {
		t.Fatalf("clarification = %+v", m.page)
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyRight})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.active != assistant.Trim {
		t.Fatalf("active = %s", m.active)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{Demo: true, Theme: "terminal", Motion: "off"})
	m = step(m, overviewMsg{platform.Demo{}.Overview(ctx)})
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 36}, {Width: 80, Height: 24}, {Width: 60, Height: 18}, {Width: 22, Height: 7}} {
		m = step(m, size)
		view := m.View()
		lines := strings.Split(view.Content, "\n")
		for _, line := range lines {
			if ansi.StringWidth(line) > size.Width {
				t.Errorf("line exceeds %d columns: %q", size.Width, line)
			}
		}
		if !view.AltScreen || len(lines) > size.Height {
			t.Fatalf("invalid view at %v", size)
		}
		if size.Width >= 80 && (!strings.Contains(view.Content, "Hai!") || !strings.Contains(lines[len(lines)-1], "Enter")) {
			t.Fatalf("missing greeting or shortcuts at %v:\n%s", size, view.Content)
		}
	}
}

func TestMotionUsesLatestGeneration(t *testing.T) {
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{Demo: true})
	fake := platform.DemoTime
	m.now = func() time.Time { return fake }
	m.busy = false
	m.show(mascot.Greeting, mascot.Idle, false)
	old := m.visual.generation
	m = step(m, keyPress("j"))
	fake = fake.Add(motion.FrameInterval)
	m = step(m, motion.Tick{Generation: old, At: fake})
	if m.visual.pose != mascot.LookRight || m.visual.frame != 0 {
		t.Fatalf("obsolete tick advanced %+v", m.visual)
	}
	m = step(m, motion.Tick{Generation: m.visual.generation, At: fake})
	if m.visual.pose != mascot.Idle || m.visual.wakeAt.IsZero() {
		t.Fatalf("look did not settle: %+v", m.visual)
	}
	m.show(mascot.Scanning, "", true)
	m = step(m, tea.WindowSizeMsg{Width: 20, Height: 8})
	fake = fake.Add(time.Second)
	m = step(m, motion.Tick{Generation: m.visual.generation, At: fake})
	if m.visual.frame != 0 {
		t.Fatal("hidden mascot advanced")
	}
	m = step(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.setMotion(motion.Off)
	frame := m.visual.frame
	m = step(m, motion.Tick{Generation: m.visual.generation, At: fake.Add(time.Second)})
	if m.visual.frame != frame {
		t.Fatal("off mode advanced animation")
	}
}

func TestGalleryCriticalAndVerifiedFlow(t *testing.T) {
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{Demo: true, Gallery: true})
	m = step(m, overviewMsg{platform.Demo{}.Overview(ctx)})
	m.galleryChoice(Choice{Kind: "gallery-flow", Value: "Critical"})
	for range 2 {
		m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	if m.visual.pose != mascot.Critical || m.page.Choices[0].Kind != "gallery-home" {
		t.Fatalf("critical state = %s, next = %+v", m.visual.pose, m.page.Choices)
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m.galleryChoice(Choice{Kind: "gallery-flow", Value: "Normal"})
	for i := 1; i < len(demoFlow); i++ {
		if m.visual.pose == mascot.Success {
			t.Fatal("gallery celebrated before verification")
		}
		m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	if m.visual.pose != mascot.Success {
		t.Fatalf("verified state = %s", m.visual.pose)
	}
	m = step(m, keyPress("n"))
	if w, _ := m.bounds(); w != 60 {
		t.Fatalf("narrow simulation width = %d", w)
	}
}

func TestMotionPreferencesCoalesceRepeatedPresses(t *testing.T) {
	var saved []motion.Mode
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{SaveMotion: func(mode motion.Mode) error {
		saved = append(saved, mode)
		return nil
	}})
	first := m.setMotion(motion.Reduced)
	m.setMotion(motion.Off)
	m.setMotion(motion.Full)
	next, last := m.Update(first())
	m = next.(Model)
	if last == nil {
		t.Fatal("latest preference was not queued")
	}
	m = step(m, last())
	if !slices.Equal(saved, []motion.Mode{motion.Reduced, motion.Full}) || m.options.Motion != motion.Full || m.motionSaving {
		t.Fatalf("saved modes = %v, state = %s", saved, m.options.Motion)
	}
}

func TestStalePageShowsAgeWithoutChangingData(t *testing.T) {
	ctx := context.Background()
	o := platform.Demo{}.Overview(ctx)
	m := New(ctx, platform.Demo{}, nil, Options{Language: "en", Motion: "off"})
	now := o.CapturedAt
	m.now = func() time.Time { return now }
	m = step(m, overviewMsg{o})
	m.active = assistant.Storage
	m.page = Page{Findings: []diagnostics.Finding{{At: o.CapturedAt}}, Found: []string{"SSD fixture"}}
	if m.stalePage() {
		t.Fatal("fresh result was marked stale")
	}
	now = now.Add(31 * time.Minute)
	if !m.stalePage() || !strings.Contains(m.View().Content, "Stale data") {
		t.Fatal("retained findings did not show staleness")
	}
	if m.page.Found[0] != "SSD fixture" {
		t.Fatal("actual measurement was changed to animate its age")
	}
}

func TestOverviewChoiceRescansAndCanCancel(t *testing.T) {
	ctx := context.Background()
	provider := platform.Demo{}
	m := New(ctx, provider, nil, Options{Demo: true, Motion: "off"})
	m = step(m, overviewMsg{provider.Overview(ctx)})
	m.focus = pageFocus
	if len(m.page.Choices) == 0 || m.page.Choices[0].Kind != "rescan" {
		t.Fatal("overview has no displayed rescan")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || !m.busy || m.cancelJob == nil {
		t.Fatal("overview Enter did not start a cancellable scan")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m = step(m, cmd())
	if m.busy || m.page.Pose != mascot.Cancelled {
		t.Fatal("overview cancellation was not reported")
	}
}

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
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
)

func keyPress(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: []rune(s)[0], Text: s} }

func step(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestOverviewShowsProcessorGroupScope(t *testing.T) {
	o := platform.Demo{}.Overview(t.Context())
	o.OS, o.Distro, o.LogicalCPUs = "windows", "Windows", 128
	o.CPU.Scope, o.CPU.Availability = domain.PrimaryProcessorGroup, domain.Partial
	page := overviewPage(o, "en")
	if !slices.Contains(page.Found, "Primary group CPU 24% · 8 samples") ||
		!strings.Contains(strings.Join(page.Evidence, "\n"), "host CPU 14.0%") {
		t.Fatalf("details conflate the CPU metrics: %+v", page)
	}
}

func TestHomeAndQuestionReturn(t *testing.T) {
	m := New(context.Background(), platform.Demo{}, nil, Options{Demo: true, Motion: motion.Off})
	v := m.View().Content
	if !strings.Contains(v, "Mau dibantu apa?") || !strings.Contains(v, "Cek kondisi PC") || strings.Contains(v, "Kernel") || m.Init() != nil {
		t.Fatalf("unexpected Home %s", v)
	}
	for range 3 {
		m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen != more {
		t.Fatal("More did not open")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.screen != home || m.choice != 3 {
		t.Fatal("Back did not restore Home selection")
	}
	m = step(m, keyPress("/"))
	for _, r := range "ssd trim q?d" {
		m = step(m, keyPress(string(r)))
	}
	if m.input.Value() != "ssd trim q?d" {
		t.Fatal("shortcut intercepted text")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen != taskQuestion || len(m.page.Choices) != 2 {
		t.Fatal("missing clarification")
	}
}

func TestViewFitsTerminal(t *testing.T) {
	m := New(context.Background(), platform.Demo{}, nil, Options{Demo: true})
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 36}, {Width: 46, Height: 14}} {
		m = step(m, size)
		for _, line := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("too wide: %q", line)
			}
		}
		if !strings.Contains(m.View().Content, "Lainnya") {
			t.Fatal("Home lost secondary path")
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
	if m.visual.pose != mascot.LookRight || m.visual.frame != 0 || !strings.Contains(m.View().Content, "» ") {
		t.Fatalf("obsolete tick advanced %+v", m.visual)
	}
	fake = fake.Add(time.Second)
	m = step(m, motion.Tick{Generation: m.visual.generation, At: fake})
	if m.visual.pose != mascot.Idle || m.visual.wakeAt.IsZero() || strings.Contains(m.View().Content, "» ") {
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

func TestQuestionAcknowledgementStartsWorkAndYieldsToLatestState(t *testing.T) {
	m := New(context.Background(), platform.Demo{}, func(context.Context, Request) Page {
		return Page{Availability: domain.Available, Found: []string{"Hasil SSD"}}
	}, Options{Demo: true})
	at := platform.DemoTime
	m.now = func() time.Time { return at }
	m = step(m, keyPress("/"))
	generation := m.visual.generation
	for _, r := range "cek SSD" {
		m = step(m, keyPress(string(r)))
	}
	if m.visual.pose != mascot.Listening || m.visual.generation != generation || m.input.Value() != "cek SSD" {
		t.Fatal("typing replaced the listening pose or dropped input")
	}
	next, run := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if run == nil || !m.busy || m.visual.pose != mascot.Nod || !strings.Contains(m.View().Content, "Pertanyaan · cek SSD") {
		t.Fatal("submission delayed work or omitted the question and nod")
	}
	generation = m.visual.generation
	at = at.Add(3 * motion.FrameInterval)
	m = step(m, motion.Tick{Generation: generation, At: at})
	if m.visual.pose != mascot.SSD || !m.busy || !strings.Contains(m.View().Content, "J[=]") {
		t.Fatal("live inspection lost its compact accessory after the nod")
	}
	m = step(m, run())
	if m.busy || m.visual.pose != mascot.Idle || !strings.Contains(m.View().Content, "Hasil SSD") {
		t.Fatal("backend result did not replace the scanning pose")
	}
}

func TestGalleryFullArtAndPanelAccentFitWithoutShifting(t *testing.T) {
	m := New(context.Background(), platform.Demo{}, nil, Options{Demo: true, Gallery: true, ASCII: true})
	at := time.Now()
	m.now = func() time.Time { return at }
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 36})
	m.accent(false, true)
	full := strings.Split(m.View().Content, "\n")
	if !strings.Contains(full[1], "---") || !strings.Contains(m.View().Content, ".--------.") {
		t.Fatal("wide gallery did not show the panel accent and full artwork")
	}
	at = at.Add(motion.PanelDuration / 2)
	m = step(m, motion.Tick{Generation: m.visual.generation, At: at})
	if middle := strings.Split(m.View().Content, "\n")[1]; !strings.Contains(middle, "-----") || strings.Contains(middle, "------") {
		t.Fatal("panel accent did not expand in its reserved row")
	}
	at = at.Add(motion.PanelDuration)
	m = step(m, motion.Tick{Generation: m.visual.generation, At: at})
	settled := strings.Split(m.View().Content, "\n")
	if len(full) != len(settled) || strings.Contains(settled[1], "---") || full[2] != settled[2] {
		t.Fatal("panel reveal stayed active or shifted content")
	}
	m = step(m, keyPress("n"))
	if strings.Contains(m.View().Content, ".--------.") {
		t.Fatal("narrow gallery consumed space with the full artwork")
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
	if w, _ := m.bounds(); w != 54 {
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
	m.screen = results
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
	m := New(context.Background(), platform.Demo{}, nil, Options{Demo: true, Motion: "off"})
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || m.screen != scanning {
		t.Fatal("Enter did not start checking")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m = step(m, cmd())
	if m.busy || m.screen != home {
		t.Fatal("cancel did not return to Home")
	}
}

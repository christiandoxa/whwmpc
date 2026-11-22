package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/platform"
)

func keyPress(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: []rune(s)[0], Text: s} }

func step(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestInputFocusAndClarification(t *testing.T) {
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{Demo: true, Theme: "terminal", ReduceMotion: true})
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
	m := New(ctx, platform.Demo{}, nil, Options{Demo: true, Theme: "terminal", ReduceMotion: true})
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

package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/privread"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

type activityFixture struct {
	platform.Demo
	observation, baseline int
	interval              time.Duration
}

func (f *activityFixture) Overview(ctx context.Context) domain.Overview {
	f.baseline++
	return f.Demo.Overview(ctx)
}

func (f *activityFixture) Observe(ctx context.Context, interval time.Duration) domain.Overview {
	f.observation++
	f.interval = interval
	return f.Demo.Overview(ctx)
}

func TestSlowdownAnswerDeterminesReadAfterAuthorization(t *testing.T) {
	for _, watch := range []bool{true, false} {
		fixture := &activityFixture{}
		scanner := inspection.New(true)
		scanner.Provider = fixture
		access := privread.New(privread.Backend{}, true)
		defer access.Disable()
		inspect := Inspector(scanner, state.Store{}, workflow.NewMaintenance(state.Store{}, true))
		m := New(t.Context(), scanner, inspect, Options{Demo: true, Language: "en", Motion: "off", Privilege: access})
		enter := tea.KeyPressMsg{Code: tea.KeyEnter}
		m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
		m = step(m, enter)
		if m.access != nil || m.screen != taskQuestion || m.active != assistant.Slow {
			t.Fatal("the symptom question did not precede access choice")
		}
		if !watch {
			m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
		}
		m = step(m, enter)
		if m.access == nil || m.access.scope != privread.Observe || (m.access.request.ObserveFor > 0) != watch {
			t.Fatal("access choice did not retain the observation request")
		}
		m = step(m, tea.KeyPressMsg{Code: tea.KeyUp})
		m = step(m, enter)
		auth := foregroundReads{ctx: t.Context(), access: access, scope: m.access.scope, stdin: os.Stdin}
		next, run := m.Update(privilegeMsg{m.job, auth.Run()})
		m = next.(Model)
		if run == nil || m.active != assistant.Slow || m.screen != scanning {
			t.Fatal("authorization did not start the selected slow check")
		}
		m = step(m, run())
		if m.page.Snapshot.Input.SMART != nil || m.page.Snapshot.Input.Trim != nil {
			t.Fatal("slow check ran unrelated drive probes")
		}
		if watch {
			if fixture.observation != 1 || fixture.baseline != 0 || fixture.interval != 4*time.Second || m.page.Snapshot.ObservedFor == 0 {
				t.Fatalf("wrong observation window or source: %+v", fixture)
			}
		} else if fixture.observation != 0 || fixture.baseline != 1 || m.page.Snapshot.ObservedFor != 0 ||
			!strings.Contains(m.page.Summary.Advice, "earlier slowdown remains undetermined") || m.page.Summary.Primary.Kind != "slow-observe" {
			t.Fatal("baseline presented itself as an investigation of the earlier slowdown")
		}
	}
}

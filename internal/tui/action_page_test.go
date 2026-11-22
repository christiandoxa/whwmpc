package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func TestApplyRequiresDisplayedPlanAndConfirmation(t *testing.T) {
	ctx := context.Background()
	plan := actions.Plan{ID: "visible", Kind: actions.CleanAPT, Target: "fixture", Initial: "one entry", Risk: "permanent", Privilege: actions.Polkit}
	calls := 0
	run := func(ctx context.Context, req Request) Page {
		calls++
		if req.Plan == nil || req.Plan.ID != plan.ID || req.Choice.Kind != "apply" || req.Choice.Value != plan.ID {
			t.Fatalf("unexpected apply request %+v", req)
		}
		if ctx.Err() == nil {
			t.Fatal("executor did not receive cancellation")
		}
		return resultPage(workflow.Outcome{Result: actions.Result{Plan: plan, Status: actions.Unknown,
			Started: true, CancellationRequested: true, Message: "Pending evidence"}}, "en", req.Intent)
	}
	m := New(ctx, platform.Demo{}, run, Options{Language: "en", Motion: "off"})
	m = step(m, overviewMsg{platform.Demo{}.Overview(ctx)})
	m.active, m.screen, m.page = assistant.Cleanup, reviewPlan, planPage(plan, "en")
	if m.choices()[m.choice].Kind != "cancel-plan" {
		t.Fatal("review defaults to apply")
	}
	if cmd := m.start(assistant.Cleanup, Choice{Kind: "apply", Value: plan.ID}); cmd != nil {
		t.Fatal("apply was not selected")
	}
	if cmd := m.start(assistant.Cleanup, Choice{Kind: "apply", Value: "different"}); cmd != nil {
		t.Fatal("different ID was accepted")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = step(m, tea.WindowSizeMsg{Width: 22, Height: 7})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.busy {
		t.Fatal("an invisible confirmation could apply")
	}
	m = step(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.busy || m.activity == mascot.Cleaning {
		t.Fatal("apply did not start revalidation stage")
	}
	m = step(m, stageMsg{m.job - 1, actions.Executing})
	if m.activity == mascot.Cleaning {
		t.Fatal("obsolete stage moved to cleanup")
	}
	m = step(m, stageMsg{m.job, actions.Executing})
	if m.activity != mascot.Cleaning {
		t.Fatal("cleanup pose missing while backend executes")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.busy || m.visual.pose != mascot.Cancelled || !m.cancelling {
		t.Fatal("cancel prematurely ended the job")
	}
	m = step(m, cmd())
	if calls != 1 || m.busy || m.page.Availability != domain.Unknown || m.page.Pose == mascot.Success ||
		!strings.Contains(strings.Join(m.page.Found, " "), "Outcome unknown") {
		t.Fatalf("cancelled result obscured unknown status: %+v", m.page)
	}
}

func TestPikoCelebratesOnlyVerifiedProfileAndPreservesCriticalNotice(t *testing.T) {
	ctx := context.Background()
	o := workflow.Outcome{Result: actions.Result{Plan: actions.Plan{Kind: actions.PowerProfile, Target: "performance"}, Status: actions.Partial}}
	if page := resultPage(o, "en", assistant.Power); page.Pose == mascot.Performance {
		t.Fatal("celebrated partial profile change")
	}
	o.Result.Status, o.Result.Changed, o.Result.FinishedAt = actions.Verified, new(true), time.Now()
	page := resultPage(o, "en", assistant.Power)
	if page.Pose != mascot.Performance {
		t.Fatal("verified profile lacked its pose")
	}
	m := New(ctx, platform.Demo{}, nil, Options{Language: "en"})
	m = step(m, overviewMsg{platform.Demo{}.Overview(ctx)})
	m.criticalSSD, m.page = true, page
	m.showReply()
	if !strings.Contains(m.View().Content, "CRITICAL") {
		t.Fatal("verification hid an unrelated critical finding")
	}
}

func TestLongReviewBlocksApplyUntilRead(t *testing.T) {
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{Language: "en"})
	m.remember()
	plan := actions.Plan{ID: "review", Kind: actions.CleanAPT, Initial: strings.Repeat("/var/cache/apt/archives ", 28), Risk: "Permanent deletion", Privilege: actions.Sudo}
	m.screen, m.page = reviewPlan, planPage(plan, "en")
	m = step(m, tea.WindowSizeMsg{Width: 48, Height: 16})
	if m.reviewReady() || !strings.Contains(m.View().Content, "review first") {
		t.Fatal("did not gate long review")
	}
	for i := 0; i < 200 && !m.reviewReady(); i++ {
		m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if !m.reviewReady() || m.choice != 0 || !strings.Contains(m.View().Content, "Permanent deletion") {
		t.Fatalf("review did not show risk %s", m.View().Content)
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.busy && m.screen == executing {
		t.Fatal("repeated Enter caused apply")
	}
}

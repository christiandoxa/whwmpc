package tui

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func TestDemoCleanupReviewExercisesVerificationWithoutSystemAccess(t *testing.T) {
	store := state.Store{StateDir: t.TempDir()}
	scanner := inspection.New(true)
	inspect := Inspector(scanner, store, workflow.NewMaintenance(store, true))
	var stages []actions.Stage
	m := New(t.Context(), scanner, inspect, Options{Demo: true, DemoCleanupResult: true, Language: "en", Motion: "off",
		Authenticate: func(context.Context, actions.Plan, string, io.Reader, io.Writer, io.Writer) error {
			t.Fatal("demo tried to authenticate a system action")
			return nil
		}, Send: func(msg tea.Msg) {
			if stage, ok := msg.(stageMsg); ok {
				stages = append(stages, stage.stage)
			}
		}})
	cmd := m.start(assistant.Cleanup, Choice{})
	m = step(m, cmd())
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	next, cmd := m.Update(enter)
	m = step(next.(Model), cmd())
	if m.screen != reviewPlan || m.choice != 0 || m.page.Plan.ID != demoCleanupID {
		t.Fatal("review did not open with Back selected")
	}
	m = step(m, enter)
	if m.screen != results || len(stages) != 0 {
		t.Fatal("leaving the preview ran cleanup")
	}
	next, cmd = m.Update(enter)
	m = step(next.(Model), cmd())
	m = step(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyDown})
	next, cmd = m.Update(enter)
	m = step(next.(Model), cmd())
	r := m.page.Snapshot.Input.Actions[0]
	if m.screen != outcome || !m.page.Snapshot.Input.Demo || r.Status != actions.Verified || r.RemovedBytes == nil || *r.RemovedBytes != 376<<20 ||
		r.FreeSpaceDelta == nil || *r.FreeSpaceDelta == int64(*r.RemovedBytes) ||
		!slices.Equal(stages, []actions.Stage{actions.Revalidating, actions.Executing, actions.Verifying}) {
		t.Fatalf("demo verification or accounting did not match the fixture: %+v", r)
	}
	m = step(m, enter)
	if m.screen != home || !strings.Contains(m.lastConclusion, "Cache cleanup verified") {
		t.Fatal("completing cleanup lost the verified outcome")
	}
}

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

func TestFirmwareHandoffOnlyCelebratesVerifiedOutcome(t *testing.T) {
	for _, test := range []struct {
		state        string
		availability domain.Availability
		label        string
		pose         mascot.Pose
	}{
		{"reboot_required", domain.Partial, "Firmware awaiting restart", mascot.Warning},
		{"failed", domain.Error, "Firmware update failed", mascot.Error},
		{"unknown", domain.PermissionRequired, "Permission required", mascot.Unknown},
		{"verified", domain.Available, "Firmware verified", mascot.Success},
	} {
		t.Run(test.state, func(t *testing.T) {
			v := firmware.Verification{State: test.state, Availability: test.availability, Installed: "0.1.51"}
			page := firmwareVerificationPage(v, "en")
			if got := replyPose(page); got != test.pose || !strings.Contains(strings.Join(page.Found, " "), test.label) ||
				page.Found[len(page.Found)-1] != "Active version · 0.1.51" {
				t.Fatalf("firmware resume: got pose %s, copy %v", got, page.Found)
			}
		})
	}
}

func TestFirmwareResumeReturnsToUpdatedHistory(t *testing.T) {
	planID := strings.Repeat("1", 40)
	inspect := func(ctx context.Context, req Request) Page {
		if req.Intent != assistant.History {
			t.Fatalf("unexpected intent %s", req.Intent)
		}
		switch req.Choice.Kind {
		case "firmware-resume":
			if req.Choice.Value != planID {
				t.Fatalf("wrong handoff %s", req.Choice.Value)
			}
			return Page{Availability: domain.Available, Pose: mascot.Success, refreshHistory: true}
		case "rescan":
			return Page{Availability: domain.Available, Found: []string{"firmware-update · verified"}}
		default:
			t.Fatalf("unexpected request %+v", req.Choice)
			return Page{}
		}
	}
	m := New(t.Context(), platform.Demo{}, inspect, Options{Language: "en", Motion: "off"})
	m.active, m.screen, m.page = assistant.History, results, Page{Choices: []Choice{{"Resume", "firmware-resume", planID}}}
	started, verify := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = started.(Model)
	if verify == nil || !m.runningAction || m.workName != "Verifying the fwupd result" {
		t.Fatal("resume did not start firmware verification")
	}
	m = step(m, verify())
	returned, reload := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if reload == nil {
		t.Fatal("back restored a stale history page")
	}
	m = step(returned.(Model), reload())
	if m.active != assistant.History || m.screen != results || !strings.Contains(m.View().Content, "firmware-update · verified") {
		t.Fatalf("unexpected history after back: %+v", m.page)
	}
}

func TestCancelledFirmwareResumeShowsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	page := firmwarePage(ctx, inspection.New(false), &workflow.Maintenance{}, Request{Language: "en", Choice: Choice{Kind: "firmware-resume"}})
	if page.Pose != mascot.Cancelled || page.Availability != domain.Unknown || page.refreshHistory {
		t.Fatalf("unexpected cancellation result: %+v", page)
	}
}

func TestFirmwareInventoryUnavailableKeepsHonestStatus(t *testing.T) {
	for _, test := range []struct {
		status domain.Availability
		label  string
		detail string
	}{
		{domain.DependencyMissing, "Missing dependency", "fwupdmgr"},
		{domain.Unsupported, "Unsupported", "Linux"},
	} {
		t.Run(string(test.status), func(t *testing.T) {
			page := firmwareInventoryPage(firmware.Snapshot{Availability: test.status}, "en", true, true)
			if page.Availability != test.status || len(page.Found) != 2 || page.Found[1] != test.label || len(page.Choices) != 0 ||
				!strings.Contains(strings.Join(page.Advice, " "), test.detail) || replyPose(page) != mascot.Unknown {
				t.Fatalf("unexpected unavailable firmware page: %+v", page)
			}
		})
	}
	page := firmwareInventoryPage(firmware.Snapshot{Availability: domain.Available}, "en", false, true)
	if page.Found[1] != "0 · Available" || len(page.Choices) != 2 || page.Choices[1].Kind != actions.FirmwareRefresh {
		t.Fatalf("empty inventory lost a valid refresh choice: %+v", page)
	}
}

func TestLongReviewBlocksApplyUntilRead(t *testing.T) {
	ctx := context.Background()
	m := New(ctx, platform.Demo{}, nil, Options{Language: "en"})
	m.remember()
	plan := actions.Plan{ID: "review", Kind: actions.CleanAPT, Initial: strings.Repeat("/var/cache/apt/archives ", 28), Risk: "Permanent deletion", Privilege: actions.Sudo}
	m.screen, m.page = reviewPlan, planPage(plan, "en")
	m = step(m, tea.WindowSizeMsg{Width: 48, Height: 18})
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

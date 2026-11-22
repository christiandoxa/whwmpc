package tui

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/privread"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func TestFirstGoalStartsWithOrdinaryCheck(t *testing.T) {
	access := privread.New(privread.Backend{Start: func(context.Context, privread.Scope, *os.File, io.Writer) (io.ReadWriteCloser, error) {
		t.Fatal("the demo tried to authorize a native process")
		return nil, nil
	}}, true)
	defer access.Disable()
	calls := 0
	m := New(t.Context(), platform.Demo{}, func(_ context.Context, req Request) Page {
		calls++
		if req.Intent != assistant.Overview || req.Plan != nil || req.Choice.Kind != "" {
			t.Fatalf("inspection selection launched an action: %+v", req)
		}
		return Page{Availability: domain.Available}
	}, Options{Demo: true, Language: "en", Motion: "off", Privilege: access})
	if m.screen != home || m.choice != 0 || !strings.Contains(m.View().Content, "Understand your PC") {
		t.Fatal("startup did not show the primary goal and purpose")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.choice != 1 || m.choices()[1].Kind != "ordinary" {
		t.Fatal("ordinary reads were not the default")
	}
	next, scan := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.screen != scanning || scan == nil || access.State().Requested {
		t.Fatal("ordinary access did not begin the task")
	}
	m = step(m, scan())
	if m.screen != results || m.active != assistant.Overview || calls != 1 || len(m.back) != 1 {
		t.Fatal("goal was not checked in two confirmations")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.screen != home || calls != 1 {
		t.Fatal("back did not return to goals")
	}
}

func TestInitialGrantContinuesSelectedCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		info string
	}{
		{"granted", nil, "No warnings in this sample"},
		{"denied", privread.ErrDenied, "Read access was not granted"},
		{"cancelled", privread.ErrCancelled, "Authorization cancelled"},
		{"timeout", context.DeadlineExceeded, "Authorization timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := privread.New(privread.Backend{Operations: []privread.Operation{privread.SMART},
				Start: func(_ context.Context, scope privread.Scope, _ *os.File, _ io.Writer) (io.ReadWriteCloser, error) {
					if scope != privread.Check {
						t.Fatalf("unexpected scope %q", scope)
					}
					return nil, tc.err
				}}, tc.err == nil)
			defer access.Disable()
			calls := 0
			m := New(t.Context(), platform.Demo{}, func(_ context.Context, req Request) Page {
				calls++
				if req.Intent != assistant.Overview || access.State().Active != (tc.err == nil) {
					t.Fatal("check ran with the wrong task or access")
				}
				return Page{Availability: domain.Available}
			}, Options{Demo: true, Language: "en", Motion: "off", Privilege: access})
			m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if !strings.Contains(m.View().Content, "SMART") || access.State().Requested {
				t.Fatal("scope must be visible before consent")
			}
			m = step(m, tea.KeyPressMsg{Code: tea.KeyUp})
			next, grant := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(Model)
			if grant == nil || !m.busy || calls != 0 {
				t.Fatal("grant did not open foreground auth before scanning")
			}
			exec := foregroundReads{ctx: t.Context(), access: access, stdin: os.Stdin, scope: m.access.scope}
			next, scan := m.Update(privilegeMsg{m.job, exec.Run()})
			m = next.(Model)
			if scan == nil || m.screen != scanning || m.access != nil {
				t.Fatal("auth result did not resume the original request")
			}
			m = step(m, scan())
			if m.screen != results || calls != 1 || !strings.Contains(m.View().Content, tc.info) {
				t.Fatalf("check did not retain the result and access status: %s", m.View().Content)
			}
		})
	}
}

func TestFailedAuthorizationKeepsCoverageAndRetry(t *testing.T) {
	access := privread.New(privread.Backend{Operations: []privread.Operation{privread.SMART},
		Start: func(context.Context, privread.Scope, *os.File, io.Writer) (io.ReadWriteCloser, error) {
			return nil, privread.ErrCancelled
		}}, false)
	defer access.Disable()
	if err := access.Enable(t.Context(), privread.All, nil, io.Discard); err == nil {
		t.Fatal("cancel fixture granted access")
	}
	m := New(t.Context(), platform.Demo{}, nil, Options{Language: "en", Motion: "off", Privilege: access})
	m.screen, m.active = results, assistant.Overview
	m.request = &Request{Intent: assistant.Overview}
	m.page = Page{Availability: domain.Partial, Coverage: []workflow.Coverage{
		{ID: workflow.SSDCheck, Status: domain.PermissionRequired}, {ID: workflow.SystemCheck, Status: domain.Available},
	}, Findings: []diagnostics.Finding{{Category: "ssd", Availability: domain.PermissionRequired}, {Category: "ssd", Availability: domain.PermissionRequired}}}
	summary := summarize(m.page, m.active, "en")
	m.summarizeReads(&summary)
	m.page.Summary = &summary
	got := m.View().Content
	for _, want := range []string{"1 need permission", "Authorization cancelled", "Complete the SSD check", "Ordinary"} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q: %s", want, got)
		}
	}
}

func TestSSDRetryKeepsGoalAndOtherReadings(t *testing.T) {
	access := privread.New(privread.Backend{}, true)
	defer access.Disable()
	scanner := inspection.New(true)
	scanner.Privilege, scanner.DemoSMARTPermission = access, true
	store := state.Store{}
	inspect := Inspector(scanner, store, workflow.NewMaintenance(store, true))
	var requests []Request
	m := New(t.Context(), scanner, func(ctx context.Context, req Request) Page {
		requests = append(requests, req)
		return inspect(ctx, req)
	}, Options{Demo: true, Language: "en", Motion: "off", Privilege: access})
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	m = step(m, enter)
	next, scan := m.Update(enter)
	m = step(next.(Model), scan())
	old := m.page.Snapshot
	if old.Input.SMART.Availability != domain.PermissionRequired || m.page.Summary.RetryRead != privread.SMART {
		t.Fatal("ordinary access did not offer to complete unread SMART")
	}
	for i, choice := range m.choices() {
		if choice.Kind == "complete-read" {
			m.choice = i
			break
		}
	}
	choice, back := m.choice, len(m.back)
	for _, failure := range []error{privread.ErrCancelled, context.DeadlineExceeded, privread.ErrExpired, nil} {
		m = step(m, enter)
		if m.access == nil || m.access.scope != privread.Scope(privread.SMART) || m.choices()[m.choice].Kind != "back" {
			t.Fatal("retry did not ask for the missing read only")
		}
		m = step(m, tea.KeyPressMsg{Code: tea.KeyUp})
		m = step(m, enter)
		job := m.job
		m = step(m, privilegeMsg{job - 1, privread.ErrDenied})
		if !m.busy || len(requests) != 1 {
			t.Fatal("obsolete reply interrupted authorization")
		}
		if failure == nil {
			exec := foregroundReads{ctx: t.Context(), access: access, stdin: os.Stdin, scope: m.access.scope}
			if err := exec.Run(); err != nil {
				t.Fatal(err)
			}
		}
		next, scan = m.Update(privilegeMsg{job, failure})
		m = next.(Model)
		if failure != nil {
			if scan != nil || m.page.Snapshot != old || m.choice != choice || len(m.back) != back || m.readErr != failure {
				t.Fatal("failed authorization altered the task or the displayed reading")
			}
			continue
		}
		if scan == nil || m.request.Snapshot != old || m.request.Complete != privread.SMART || m.request.Plan != nil {
			t.Fatal("authorization did not resume the requested component")
		}
		m = step(m, scan())
	}
	in := m.page.Snapshot.Input
	if m.screen != results || m.active != assistant.Overview || in.SMART.Availability != domain.Available ||
		in.Overview != old.Input.Overview || in.Power != old.Input.Power || in.Trim != old.Input.Trim || len(requests) != 2 {
		t.Fatal("completing SMART changed unrelated readings or navigated away")
	}
	newSnapshot := m.page.Snapshot
	m.start(assistant.Settings, Choice{})
	m.start(assistant.Settings, Choice{Kind: "check-level"})
	m = step(m, enter)
	if m.page.Snapshot != newSnapshot || m.active != assistant.Overview || access.State().Active || len(requests) != 2 {
		t.Fatal("ordinary mode did not restore the same result without reading again")
	}
}

func TestRepeatReadingFeedbackAndRollback(t *testing.T) {
	scanner := inspection.New(true)
	inspect := Inspector(scanner, state.Store{}, workflow.NewMaintenance(state.Store{}, true))
	m := New(t.Context(), scanner, inspect, Options{Demo: true, Language: "en", Motion: "off"})
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	next, scan := m.Update(enter)
	m = step(next.(Model), scan())
	old := m.page.Snapshot
	rescan := Choice{Kind: "rescan"}
	for _, cancel := range []bool{true, false} {
		scan = m.start(assistant.Overview, rescan)
		if !m.busy || scan == nil || !strings.Contains(m.workName, "new measurements") {
			t.Fatal("repeat check gave no feedback")
		}
		if cancel {
			m = step(m, tea.KeyPressMsg{Code: tea.KeyEsc})
		}
		m = step(m, scan())
		if cancel && (m.page.Snapshot != old || m.toast != "Check stopped") {
			t.Fatal("cancellation lost the previous result")
		}
	}
	if m.page.Snapshot == old || m.toast != "Repeat check finished" {
		t.Fatal("the same readings hid the new attempt")
	}
}

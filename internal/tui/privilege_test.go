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
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/privread"
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
	m.page = Page{Availability: domain.Partial, Coverage: []workflow.Coverage{
		{ID: workflow.SSDCheck, Status: domain.PermissionRequired}, {ID: workflow.SystemCheck, Status: domain.Available},
	}, Findings: []diagnostics.Finding{{Category: "ssd", Availability: domain.PermissionRequired}, {Category: "ssd", Availability: domain.PermissionRequired}}}
	summary := summarize(m.page, m.active, "en")
	m.summarizeReads(&summary)
	m.page.Summary = &summary
	got := m.View().Content
	for _, want := range []string{"1 need permission", "Authorization cancelled", "Retry read authorization", "Ordinary"} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q: %s", want, got)
		}
	}
}

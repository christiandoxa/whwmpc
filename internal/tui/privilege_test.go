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

func TestInspectionLevelDefaultsToOrdinaryAndChangesSafely(t *testing.T) {
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
	keys := func(code rune) { m = step(m, tea.KeyPressMsg{Code: code}) }
	if m.choice != 1 || m.choices()[1].Kind != "ordinary" {
		t.Fatal("ordinary reads were not the default")
	}
	keys(tea.KeyEnter)
	if m.screen != home || access.State().Requested || calls != 0 {
		t.Fatal("ordinary startup did more than show the home screen")
	}
	m.start(assistant.Settings, Choice{Kind: "check-level"})
	keys(tea.KeyUp)
	keys(tea.KeyEnter)
	if !strings.Contains(m.View().Content, "SMART") || access.State().Requested || m.choice != 0 {
		t.Fatal("read scope and confirmation were bypassed")
	}
	keys(tea.KeyDown)
	next, grant := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if grant == nil || !m.busy || access.State().Active {
		t.Fatal("confirmation did not hand off to foreground authorization")
	}
	exec := foregroundReads{ctx: t.Context(), access: access, stdin: os.Stdin}
	m = step(m, privilegeMsg{m.job, exec.Run()})
	if m.screen != home || !access.State().Active || calls != 0 {
		t.Fatal("grant triggered extra work or did not reach home")
	}
	next, scan := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = step(next.(Model), scan())
	if m.screen != results || calls != 1 || !strings.Contains(m.View().Content, "Deep inspection complete") {
		t.Fatal("the selected task did not use the deep demo mode")
	}
	m.start(assistant.Settings, Choice{Kind: "check-level"})
	keys(tea.KeyEnter)
	if access.State().Requested || m.screen != home || len(m.back) != 0 {
		t.Fatal("ordinary selection retained the scope or the deep result stack")
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

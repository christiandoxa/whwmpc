package actions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakePlatform struct {
	current string
	target  string
	runs    int
	run     func(context.Context) attempt
	verify  func(context.Context, attempt) Result
}

func (f *fakePlatform) prepare(_ context.Context, kind, target string) (prepared, error) {
	f.target = target
	return prepared{
		plan: Plan{Kind: kind, Target: target, Initial: f.current, Identity: "system/" + f.current,
			Operation: []string{"/usr/bin/tool", target}, Privilege: Polkit, Rollback: kind == PowerProfile},
		run: func(ctx context.Context) attempt {
			f.runs++
			if f.run != nil {
				return f.run(ctx)
			}
			f.current = target
			return attempt{started: true}
		},
		verify: func(ctx context.Context, step attempt) Result {
			if f.verify != nil {
				return f.verify(ctx, step)
			}
			changed := true
			return Result{Status: Verified, Changed: &changed, AfterIdentity: "system/" + f.current}
		},
	}, nil
}

func (*fakePlatform) authenticate(context.Context, io.Reader, io.Writer, io.Writer) error { return nil }
func (*fakePlatform) trimTargets(context.Context) ([]TrimTarget, error)                   { return nil, nil }
func (*fakePlatform) cleanup(context.Context) (Cleanup, error)                            { return Cleanup{}, nil }

func fixtureService(p *fakePlatform) (*Service, *time.Time) {
	now := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	s := NewWithKey([32]byte{1})
	s.platform = p
	s.now = func() time.Time { return now }
	return s, &now
}

func previewPower(t *testing.T, s *Service) Plan {
	t.Helper()
	p, err := s.Preview(context.Background(), PowerProfile, "power-saver")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlanBindsConfirmationAndRevalidates(t *testing.T) {
	ctx := t.Context()
	p := &fakePlatform{current: "balanced"}
	s, now := fixtureService(p)
	plan := previewPower(t, s)
	if len(plan.ID) != 40 || p.runs != 0 {
		t.Fatalf("preview changed state or lacks confirmation: %+v, runs %d", plan, p.runs)
	}
	if _, err := s.Execute(ctx, plan, ""); err == nil {
		t.Fatal("accepted empty confirmation")
	}
	tampered := plan
	tampered.Operation = []string{"/usr/bin/unapproved"}
	if _, err := s.Execute(ctx, tampered, plan.ID); err == nil {
		t.Fatal("accepted an unsigned operation")
	}
	tampered = plan
	tampered.Target = "performance"
	if _, err := s.Execute(ctx, tampered, plan.ID); err == nil {
		t.Fatal("accepted a different target")
	}
	p.current = "performance"
	if _, err := s.Execute(ctx, plan, plan.ID); err == nil {
		t.Fatal("accepted changed state")
	}
	p.current = "balanced"
	*now = plan.ExpiresAt
	if _, err := s.Execute(ctx, plan, plan.ID); err == nil {
		t.Fatal("accepted expired plan")
	}
	if p.runs != 0 {
		t.Fatalf("started %d unapproved actions", p.runs)
	}
}

func TestRestoreRequiresSignedResultAndCurrentState(t *testing.T) {
	ctx := t.Context()
	p := &fakePlatform{current: "balanced"}
	s, _ := fixtureService(p)
	plan := previewPower(t, s)
	result, err := s.Execute(ctx, plan, plan.ID)
	if err != nil || result.Status != Verified || result.Receipt == "" {
		t.Fatalf("result %+v, err %v", result, err)
	}
	s, _ = fixtureService(p)
	invalid := result
	invalid.Plan.Initial = "performance"
	if _, err := s.PreviewRestore(ctx, invalid); err == nil {
		t.Fatal("accepted tampered receipt")
	}
	p.current = "performance"
	if _, err := s.PreviewRestore(ctx, result); err == nil {
		t.Fatal("restoring would overwrite external change")
	}
	p.current = plan.Target
	restore, err := s.PreviewRestore(ctx, result)
	if err != nil || restore.Kind != RestorePower || restore.Target != plan.Initial || restore.Restores != plan.ID {
		t.Fatalf("restore %+v, err %v", restore, err)
	}
	if _, err := s.Execute(ctx, restore, plan.ID); err == nil {
		t.Fatal("old confirmation authorized restore")
	}
	result, err = s.Execute(ctx, restore, restore.ID)
	if err != nil || result.Status != Verified || p.current != plan.Initial {
		t.Fatalf("restore result %+v, current %s, err %v", result, p.current, err)
	}
}

func TestCancellationStillVerifiesBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p := &fakePlatform{
		current: "balanced",
		run: func(context.Context) attempt {
			cancel()
			return attempt{started: true, stopped: true, err: context.Canceled}
		},
		verify: func(verifyCtx context.Context, step attempt) Result {
			if verifyCtx.Err() != nil || !step.stopped {
				t.Fatal("post-action check did not get a fresh context")
			}
			if _, hasDeadline := verifyCtx.Deadline(); !hasDeadline {
				t.Fatal("verification lacks timeout")
			}
			changed := true
			return Result{Status: Partial, Changed: &changed, Message: "verified\x1b\nchange"}
		},
	}
	s, _ := fixtureService(p)
	plan := previewPower(t, s)
	result, err := s.Execute(ctx, plan, plan.ID)
	if err != nil || result.Status != Partial || !result.Started || !result.CancellationRequested ||
		result.Changed == nil || !*result.Changed || result.Receipt != "" || result.Message != "verified change" {
		t.Fatalf("incorrect interrupted result %+v, err %v", result, err)
	}
	if result, err := s.Execute(t.Context(), plan, plan.ID); err == nil || result.Status != NotStarted {
		t.Fatalf("interrupted plan was replayed: %+v, %v", result, err)
	}
}

func TestFailedToStartDoesNotClaimMutation(t *testing.T) {
	p := &fakePlatform{current: "balanced", run: func(context.Context) attempt {
		return attempt{err: errors.New("sudo denied")}
	}, verify: func(context.Context, attempt) Result {
		t.Fatal("verification called for action that did not start")
		return Result{}
	}}
	s, _ := fixtureService(p)
	plan := previewPower(t, s)
	result, err := s.Execute(t.Context(), plan, plan.ID)
	if result.Status != NotStarted || result.Started || err == nil {
		t.Fatalf("not started result %+v, err %v", result, err)
	}
}

func TestConcurrentExecutionRejected(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	p := &fakePlatform{current: "balanced", run: func(context.Context) attempt {
		close(started)
		<-release
		return attempt{started: true}
	}}
	s, _ := fixtureService(p)
	plan := previewPower(t, s)
	outcome := make(chan error, 1)
	go func() {
		_, err := s.Execute(t.Context(), plan, plan.ID)
		outcome <- err
	}()
	<-started
	result, err := s.Execute(t.Context(), plan, plan.ID)
	close(release)
	if err == nil || result.Status != NotStarted {
		t.Fatalf("parallel result %+v, err %v", result, err)
	}
	if err := <-outcome; err != nil {
		t.Fatal(err)
	}
}

func TestReadPlanStrictRoundTrip(t *testing.T) {
	s, _ := fixtureService(&fakePlatform{current: "balanced"})
	plan := previewPower(t, s)
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadPlan(strings.NewReader(string(raw)))
	if err != nil || s.validate(loaded, plan.ID) != nil {
		t.Fatalf("round trip failed: %+v, %v", loaded, err)
	}
	for _, data := range []string{
		strings.Repeat(" ", 32<<10) + string(raw),
		string(raw) + " {}",
		`{"shell_command":"anything"}`,
	} {
		if _, err := ReadPlan(strings.NewReader(data)); err == nil {
			t.Fatal("accepted invalid plan document")
		}
	}
}

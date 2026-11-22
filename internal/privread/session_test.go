package privread

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

func fakeBackend(collect func(context.Context, Operation) any) Backend {
	ops := []Operation{SMART, TRIM}
	return Backend{Operations: ops, Start: func(ctx context.Context, scope Scope, _ *os.File, _ io.Writer) (io.ReadWriteCloser, error) {
		ui, helper := net.Pipe()
		go func() {
			defer helper.Close()
			_ = serve(context.WithoutCancel(ctx), helper, scope, ops, collect)
		}()
		return ui, nil
	}}
}

func TestSessionDenialCancellationAndDisable(t *testing.T) {
	for _, want := range []error{ErrDenied, ErrCancelled, ErrUnavailable} {
		s := New(Backend{Operations: []Operation{SMART}, Start: func(context.Context, Scope, *os.File, io.Writer) (io.ReadWriteCloser, error) {
			return nil, want
		}}, false)
		if err := s.Enable(t.Context(), Check, nil, nil); !errors.Is(err, want) || s.State().Active || !s.State().Requested {
			t.Fatalf("authorization %v returned %v, %+v", want, err, s.State())
		}
		s.Disable()
		if s.State().Requested || s.Read(t.Context(), SMART, new(storage.SMARTAudit)) {
			t.Fatal("switching to ordinary mode retained read authorization")
		}
	}
	if s := New(Backend{}, false); !errors.Is(s.Capability(), ErrUnavailable) {
		t.Fatal("missing backend was reported as available")
	}
}

func TestDemoDoesNotStartHelper(t *testing.T) {
	s := New(Backend{Operations: []Operation{SMART}, Start: func(context.Context, Scope, *os.File, io.Writer) (io.ReadWriteCloser, error) {
		t.Fatal("demo tried to start a privileged helper")
		return nil, nil
	}}, true)
	defer s.Disable()
	if err := s.Enable(t.Context(), All, nil, nil); err != nil || !s.State().Active {
		t.Fatalf("demo authorization failed: %v", err)
	}
}

func TestSingleOperationScope(t *testing.T) {
	s := New(fakeBackend(func(_ context.Context, op Operation) any {
		if op != SMART {
			t.Errorf("unauthorized operation reached helper: %s", op)
		}
		return storage.SMARTAudit{CapturedAt: time.Now(), Availability: domain.Available}
	}), false)
	defer s.Disable()
	if err := s.Enable(t.Context(), Scope(SMART), nil, nil); err != nil {
		t.Fatal(err)
	}
	if s.Read(t.Context(), TRIM, new(TrimReading)) || !s.Read(t.Context(), SMART, new(storage.SMARTAudit)) ||
		!Observe.Allows(Power) || Observe.Allows(SMART) || Scope("trim,smart").valid() {
		t.Fatal("task scope was not enforced")
	}
}

func TestDeniedScopeChangeKeepsExistingReadSession(t *testing.T) {
	backend := fakeBackend(func(_ context.Context, op Operation) any {
		if op != SMART {
			t.Errorf("unexpected operation %q", op)
		}
		return storage.SMARTAudit{CapturedAt: time.Now(), Availability: domain.Available}
	})
	start := backend.Start
	backend.Start = func(ctx context.Context, scope Scope, terminal *os.File, stderr io.Writer) (io.ReadWriteCloser, error) {
		if scope != Scope(SMART) {
			return nil, ErrDenied
		}
		return start(ctx, scope, terminal, stderr)
	}
	s := New(backend, false)
	defer s.Disable()
	if err := s.Enable(t.Context(), Scope(SMART), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Enable(t.Context(), Check, nil, nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected denied scope change, got %v", err)
	}
	if !s.CanRead(SMART) || s.CanRead(TRIM) || !s.Read(t.Context(), SMART, new(storage.SMARTAudit)) {
		t.Fatal("a failed scope change revoked or widened the previous authorization")
	}
}

func TestScopedReadsPartialDataAndExpiry(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	s := New(fakeBackend(func(ctx context.Context, op Operation) any {
		if op == SMART {
			return storage.SMARTAudit{CapturedAt: time.Now(), Availability: domain.Partial, Drives: []storage.Drive{
				{Name: "test0", Availability: domain.Available}, {Name: "test1", Availability: domain.PermissionRequired},
			}}
		}
		close(entered)
		<-ctx.Done()
		close(stopped)
		return TrimReading{}
	}), false)
	defer s.Disable()
	if err := s.Enable(t.Context(), Check, nil, nil); err != nil {
		t.Fatal(err)
	}
	var audit storage.SMARTAudit
	if !s.Read(t.Context(), SMART, &audit) || audit.Availability != domain.Partial || len(audit.Drives) != 2 || audit.Drives[1].Availability != domain.PermissionRequired {
		t.Fatalf("partial measurements lost: %+v", audit)
	}
	if s.Read(t.Context(), Firmware, nil) || !s.State().Active || s.State().Reads != 1 {
		t.Fatal("a request outside CLI scope was dispatched")
	}
	read := make(chan bool)
	go func() { read <- s.Read(t.Context(), TRIM, new(TrimReading)) }()
	<-entered
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	s.fail(c, ErrExpired)
	if <-read || !errors.Is(s.State().Err, ErrExpired) || s.State().Reads != 1 {
		t.Fatalf("expired read was accepted: %+v", s.State())
	}
	<-stopped
}

func TestHelperRejectsRequestsOutsideReadContract(t *testing.T) {
	for _, value := range []string{
		`{"id":1,"op":"cleanup"}`,
		`{"id":1,"op":"smart","target":"/dev/sda; cat /etc/shadow"}`,
		`{"id":1,"op":"smart","args":["--test=long"]}`,
		`{"id":1,"op":"firmware"}`,
		`{"id":2,"op":"smart"}`,
		`{"id":1,"op":"smart"} {"op":"apply"}`,
		strings.Repeat(" ", 512),
	} {
		ui, helper := net.Pipe()
		called := make(chan struct{}, 1)
		result := make(chan error, 1)
		go func() {
			defer helper.Close()
			result <- serve(t.Context(), helper, Check, []Operation{SMART, Firmware}, func(context.Context, Operation) any {
				called <- struct{}{}
				return nil
			})
		}()
		if _, err := bufio.NewReader(ui).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(ui, value+"\n")
		if err := <-result; !errors.Is(err, ErrProtocol) || len(called) != 0 {
			t.Fatalf("dispatched %q: %v", value, err)
		}
		ui.Close()
	}
}

func TestMalformedTypedReplyDisablesSession(t *testing.T) {
	s := New(fakeBackend(func(context.Context, Operation) any {
		return TrimReading{Audit: storage.TrimAudit{Availability: domain.Available, CapturedAt: time.Now()}, Paths: []TrimPath{{ID: 7}}}
	}), false)
	defer s.Disable()
	if err := s.Enable(t.Context(), Check, nil, nil); err != nil {
		t.Fatal(err)
	}
	if s.Read(t.Context(), TRIM, new(TrimReading)) || !errors.Is(s.State().Err, ErrProtocol) || s.State().Active {
		t.Fatal("mismatched mount table was accepted")
	}
}

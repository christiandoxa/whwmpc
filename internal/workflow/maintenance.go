// Package workflow connects native actions with private plan state and history.
package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"runtime"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type Maintenance struct {
	Demo  bool
	Store state.Store
	key   [32]byte
}

func NewMaintenance(store state.Store, demo bool) *Maintenance {
	m := &Maintenance{Store: store, Demo: demo}
	_, _ = rand.Read(m.key[:])
	return m
}

func (m *Maintenance) Available() error {
	switch {
	case m.Demo:
		return errors.New("DEMO: tindakan sistem terkunci")
	case runtime.GOOS != "linux":
		return errors.New("tindakan ini tersedia di Linux")
	case os.Geteuid() == 0:
		return errors.New("jalankan whwmpc sebagai user biasa; autentikasi hanya untuk tindakan terpilih")
	default:
		return nil
	}
}

func (m *Maintenance) signer(persist bool) (*actions.Service, [32]byte, error) {
	key, err := m.Store.ActionKey(false)
	if errors.Is(err, fs.ErrNotExist) {
		key, err = m.key, nil
		if persist {
			key, err = m.Store.EnsureActionKey(key)
		}
	}
	if err != nil {
		return nil, key, err
	}
	return actions.NewWithKey(key), key, nil
}

func (m *Maintenance) Cleanup(ctx context.Context) (actions.Cleanup, error) {
	if m.Demo {
		return actions.Cleanup{
			Kind: actions.CleanAPT, Availability: domain.Available, Category: "APT (fixture)",
			Scope:      []string{"/var/cache/apt/archives", "/var/cache/apt/archives/partial", "/var/lib/apt/lists/partial", "pkgcache.bin", "srcpkgcache.bin"},
			KnownBytes: 376 << 20, KnownFiles: 19, Impact: "Fixture cache removal may require downloading packages again.",
		}, nil
	}
	return actions.New().Cleanup(ctx)
}

func (m *Maintenance) Preview(ctx context.Context, kind, target string, persist bool) (actions.Plan, error) {
	if err := m.Available(); err != nil {
		return actions.Plan{}, err
	}
	svc, key, err := m.signer(false)
	if err != nil {
		return actions.Plan{}, err
	}
	plan, err := svc.Preview(ctx, kind, target)
	if err != nil || !persist {
		return plan, err
	}
	stored, err := m.Store.EnsureActionKey(key)
	if err != nil {
		return actions.Plan{}, err
	}
	if stored != key {
		return actions.Plan{}, errors.New("kunci rencana berubah, tinjau ulang tindakan")
	}
	return plan, nil
}

func (m *Maintenance) Restore(ctx context.Context, id string) (actions.Plan, error) {
	if err := m.Available(); err != nil {
		return actions.Plan{}, err
	}
	svc, _, err := m.signer(false)
	if err != nil {
		return actions.Plan{}, err
	}
	receipt, err := m.Store.RollbackReceipt(id)
	if err != nil {
		return actions.Plan{}, err
	}
	return svc.PreviewRestore(ctx, receipt)
}

func (m *Maintenance) Authenticate(ctx context.Context, plan actions.Plan, id string, in io.Reader, out, stderr io.Writer) error {
	if err := m.Available(); err != nil {
		return err
	}
	svc, _, err := m.signer(false)
	if err != nil {
		return err
	}
	if plan.Restores != "" {
		if err := svc.Validate(plan, id); err != nil {
			return err
		}
		if _, err := m.Store.RollbackReceipt(plan.Restores); err != nil {
			return err
		}
	}
	return svc.Authenticate(ctx, plan, id, in, out, stderr)
}

type Outcome struct {
	Result     actions.Result
	HistoryErr error
}

type FirmwareOutcome struct {
	Verification firmware.Verification
	HistoryErr   error
}

func (m *Maintenance) ResumeFirmware(ctx context.Context, id string) (FirmwareOutcome, error) {
	if m.Demo {
		return FirmwareOutcome{}, errors.New("DEMO: akses firmware terkunci")
	}
	if err := ctx.Err(); err != nil {
		return FirmwareOutcome{}, err
	}
	release, err := m.Store.LockActions()
	if err != nil {
		return FirmwareOutcome{}, err
	}
	defer release()
	handoff, err := m.Store.FirmwareHandoff(id)
	if err != nil {
		return FirmwareOutcome{}, err
	}
	verification := firmware.OutcomeSince(ctx, sysrun.Native{}, handoff.Target, handoff.CreatedAt)
	if err := ctx.Err(); err != nil {
		return FirmwareOutcome{}, err
	}
	return FirmwareOutcome{Verification: verification, HistoryErr: m.Store.RecordFirmwareVerification(handoff, verification, time.Now())}, nil
}

func (m *Maintenance) Execute(ctx context.Context, plan actions.Plan, id string, notify func(actions.Stage)) (Outcome, error) {
	rejected := func(err error) (Outcome, error) {
		return Outcome{Result: actions.Result{Plan: plan, Status: actions.NotStarted, Message: err.Error(),
			CancellationRequested: ctx.Err() != nil, FinishedAt: time.Now().UTC()}}, err
	}
	if err := m.Available(); err != nil {
		return rejected(err)
	}
	if err := ctx.Err(); err != nil {
		return rejected(err)
	}
	svc, key, err := m.signer(false)
	if err != nil {
		return rejected(err)
	}
	if err := svc.Validate(plan, id); err != nil {
		return rejected(err)
	}
	stored, err := m.Store.EnsureActionKey(key)
	if err != nil {
		return rejected(err)
	}
	if stored != key {
		return rejected(errors.New("kunci rencana berubah, tinjau ulang tindakan"))
	}
	release, err := m.Store.BeginAction(id, plan.ExpiresAt)
	if err != nil {
		return rejected(err)
	}
	defer release()
	if plan.Restores != "" {
		if _, err := m.Store.RollbackReceipt(plan.Restores); err != nil {
			return rejected(err)
		}
	}
	result, err := svc.ExecuteWithProgress(ctx, plan, id, notify)
	return Outcome{Result: result, HistoryErr: m.Store.RecordAction(result)}, err
}

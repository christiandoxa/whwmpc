// Package actions previews, revalidates, executes, and verifies allowlisted maintenance.
package actions

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/hardware"
)

const (
	PowerProfile     = "power-profile"
	TrimMount        = "trim-mount"
	EnableTrimTimer  = "enable-trim-timer"
	CleanAPT         = "clean-apt"
	RestorePower     = "restore-power-profile"
	RestoreTrimTimer = "restore-trim-timer"

	Sudo   = "sudo"
	Polkit = "polkit"
	Root   = "root"

	planVersion = 1
	planTTL     = 10 * time.Minute
)

type Plan struct {
	ID             string    `json:"id"`
	Version        int       `json:"version"`
	Kind           string    `json:"kind"`
	Target         string    `json:"target"`
	Initial        string    `json:"initial"`
	Identity       string    `json:"identity"`
	Operation      []string  `json:"operation"`
	Privilege      string    `json:"privilege"`
	Impact         string    `json:"impact"`
	Risk           string    `json:"risk"`
	EstimatedBytes *uint64   `json:"estimated_bytes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Rollback       bool      `json:"rollback"`
	Restores       string    `json:"restores,omitempty"`
	Nonce          string    `json:"nonce"`
}

type Status string

const (
	NotStarted Status = "not_started"
	Verified   Status = "verified"
	Failed     Status = "failed"
	Partial    Status = "partial"
	Unknown    Status = "unknown"
)

type Stage string

const (
	Revalidating Stage = "revalidate"
	Executing    Stage = "execute"
	Verifying    Stage = "verify"
)

type Result struct {
	Plan                  Plan      `json:"plan"`
	Status                Status    `json:"status"`
	Message               string    `json:"message"`
	Started               bool      `json:"started"`
	CancellationRequested bool      `json:"cancellation_requested"`
	Changed               *bool     `json:"changed,omitempty"`
	PotentialDiscardBytes *uint64   `json:"potential_discard_bytes,omitempty"`
	RemovedBytes          *uint64   `json:"removed_bytes,omitempty"`
	FreeSpaceDelta        *int64    `json:"free_space_delta,omitempty"`
	FinishedAt            time.Time `json:"finished_at"`
	AfterIdentity         string    `json:"after_identity,omitempty"`
	Receipt               string    `json:"receipt,omitempty"`
}

type Cleanup struct {
	Availability domain.Availability `json:"availability"`
	Category     string              `json:"category"`
	Scope        []string            `json:"scope,omitempty"`
	Unknown      []string            `json:"unknown,omitempty"`
	KnownBytes   uint64              `json:"known_bytes"`
	KnownFiles   int                 `json:"known_files"`
	Impact       string              `json:"impact,omitempty"`
	Detail       string              `json:"detail,omitempty"`
	Recoverable  bool                `json:"recoverable"`
}

type TrimTarget struct {
	MountPoint string
	Filesystem string
	DeviceID   string
}

type Service struct {
	platform platform
	key      [32]byte
	now      func() time.Time
	mu       sync.Mutex
	used     map[string]time.Time
}

type platform interface {
	prepare(context.Context, string, string) (prepared, error)
	cleanup(context.Context) (Cleanup, error)
	trimTargets(context.Context) ([]TrimTarget, error)
	authenticate(context.Context, io.Reader, io.Writer, io.Writer) error
}

type prepared struct {
	plan   Plan
	run    func(context.Context) attempt
	verify func(context.Context, attempt) Result
}

type attempt struct {
	output  []byte
	started bool
	stopped bool
	err     error
}

func New() *Service {
	var key [32]byte
	_, _ = rand.Read(key[:])
	return NewWithKey(key)
}

// NewWithKey shares plan and receipt validation across invocations with a private, random key.
func NewWithKey(key [32]byte) *Service {
	return &Service{platform: newPlatform(), key: key, now: time.Now, used: make(map[string]time.Time)}
}

func (s *Service) Cleanup(ctx context.Context) (Cleanup, error) {
	return s.platform.cleanup(ctx)
}

func (s *Service) TrimTargets(ctx context.Context) ([]TrimTarget, error) {
	return s.platform.trimTargets(ctx)
}

func (s *Service) Preview(ctx context.Context, kind, target string) (Plan, error) {
	switch kind {
	case PowerProfile, TrimMount, EnableTrimTimer, CleanAPT, hardware.PowerLimit, hardware.CoreMax, hardware.VoltageOffset:
		return s.preview(ctx, kind, target, "", "")
	default:
		return Plan{}, errors.New("tindakan tidak dikenal")
	}
}

func (s *Service) PreviewRestore(ctx context.Context, result Result) (Plan, error) {
	if !result.Plan.Rollback || result.Status != Verified || !result.Started ||
		result.Changed == nil || !*result.Changed || result.AfterIdentity == "" {
		return Plan{}, errors.New("riwayat tidak memiliki perubahan terverifikasi untuk dipulihkan")
	}
	receipt := result.Receipt
	result.Receipt = ""
	if receipt == "" || !hmac.Equal([]byte(receipt), []byte(s.sign("receipt", result))) {
		return Plan{}, errors.New("bukti pemulihan tidak sah")
	}
	switch result.Plan.Kind {
	case PowerProfile:
		return s.preview(ctx, RestorePower, result.Plan.Initial, result.Plan.ID, result.AfterIdentity)
	case EnableTrimTimer:
		return s.preview(ctx, RestoreTrimTimer, result.Plan.Target, result.Plan.ID, result.AfterIdentity)
	case hardware.PowerLimit:
		return s.preview(ctx, hardware.RestorePowerLimit, result.Plan.Initial, result.Plan.ID, result.AfterIdentity)
	case hardware.CoreMax:
		return s.preview(ctx, hardware.RestoreCoreMax, result.Plan.Initial, result.Plan.ID, result.AfterIdentity)
	case hardware.VoltageOffset:
		return s.preview(ctx, hardware.RestoreVoltageOffset, result.Plan.Initial, result.Plan.ID, result.AfterIdentity)
	default:
		return Plan{}, errors.New("pemulihan tidak tersedia untuk tindakan ini")
	}
}

func (s *Service) preview(ctx context.Context, kind, target, restores, requiredIdentity string) (Plan, error) {
	action, err := s.platform.prepare(ctx, kind, target)
	if err != nil {
		return Plan{}, err
	}
	if requiredIdentity != "" && requiredIdentity != action.plan.Identity {
		return Plan{}, errors.New("keadaan sudah berubah sejak tindakan sebelumnya")
	}
	var nonce [12]byte
	_, _ = rand.Read(nonce[:])
	plan := action.plan
	plan.Version = planVersion
	plan.Restores = restores
	plan.CreatedAt = s.now().UTC()
	plan.ExpiresAt = plan.CreatedAt.Add(planTTL)
	plan.Nonce = hex.EncodeToString(nonce[:])
	plan.ID = s.sign("plan", plan)
	return plan, nil
}

// Authenticate lets sudo validate its credentials on a foreground terminal before revalidation.
func (s *Service) Authenticate(ctx context.Context, plan Plan, confirmedID string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := s.validate(plan, confirmedID); err != nil {
		return err
	}
	if plan.Privilege != Sudo {
		return nil
	}
	return s.platform.authenticate(ctx, stdin, stdout, stderr)
}

func (s *Service) Execute(ctx context.Context, plan Plan, confirmedID string) (Result, error) {
	return s.ExecuteWithProgress(ctx, plan, confirmedID, nil)
}

// Validate checks a confirmation before external callers reserve it for execution.
func (s *Service) Validate(plan Plan, confirmedID string) error {
	return s.validate(plan, confirmedID)
}

func (s *Service) ExecuteWithProgress(ctx context.Context, plan Plan, confirmedID string, notify func(Stage)) (Result, error) {
	progress := func(stage Stage) {
		if notify != nil {
			notify(stage)
		}
	}
	failure := func(err error) (Result, error) {
		return Result{Plan: plan, Status: NotStarted, Message: safeText(err.Error()), CancellationRequested: ctx.Err() != nil, FinishedAt: s.now().UTC()}, err
	}
	if !s.mu.TryLock() {
		return failure(errors.New("tindakan lain masih berlangsung"))
	}
	defer s.mu.Unlock()
	if err := s.validate(plan, confirmedID); err != nil {
		return failure(err)
	}
	now := s.now()
	for id, expiry := range s.used {
		if !now.Before(expiry) {
			delete(s.used, id)
		}
	}
	if _, used := s.used[plan.ID]; used {
		return failure(errors.New("rencana sudah digunakan, buat preview baru"))
	}
	progress(Revalidating)
	action, err := s.platform.prepare(ctx, plan.Kind, plan.Target)
	if err != nil {
		return failure(fmt.Errorf("pemeriksaan ulang gagal, buat preview baru: %w", err))
	}
	current := action.plan
	current.Version, current.Restores = plan.Version, plan.Restores
	current.CreatedAt, current.ExpiresAt, current.Nonce = plan.CreatedAt, plan.ExpiresAt, plan.Nonce
	if s.sign("plan", current) != plan.ID {
		return failure(errors.New("target atau kondisi awal berubah, buat preview baru"))
	}
	if err := s.validate(plan, confirmedID); err != nil {
		return failure(err)
	}
	if err := ctx.Err(); err != nil {
		return failure(err)
	}
	progress(Executing)
	step := action.run(ctx)
	if !step.started {
		if step.err == nil {
			step.err = errors.New("proses tidak dimulai")
		}
		return failure(step.err)
	}
	s.used[plan.ID] = plan.ExpiresAt
	progress(Verifying)
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	result := action.verify(verifyCtx, step)
	result.Plan = plan
	result.Started = true
	result.CancellationRequested = step.stopped || ctx.Err() != nil
	result.Message = safeText(result.Message)
	result.FinishedAt = s.now().UTC()
	if plan.Rollback && result.Status == Verified && result.Changed != nil && *result.Changed && result.AfterIdentity != "" {
		result.Receipt = s.sign("receipt", result)
	}
	return result, nil
}

func (s *Service) validate(plan Plan, confirmedID string) error {
	switch plan.Kind {
	case PowerProfile, TrimMount, EnableTrimTimer, CleanAPT, hardware.PowerLimit, hardware.CoreMax, hardware.VoltageOffset:
		if plan.Restores != "" {
			return errors.New("jenis rencana tidak valid untuk pemulihan")
		}
	case RestorePower, RestoreTrimTimer, hardware.RestorePowerLimit, hardware.RestoreCoreMax, hardware.RestoreVoltageOffset:
		if plan.Restores == "" {
			return errors.New("rencana pemulihan memerlukan bukti perubahan")
		}
	default:
		return errors.New("jenis rencana tidak dikenal")
	}
	id := plan.ID
	plan.ID = ""
	if plan.Version != planVersion || len(plan.Nonce) != 24 || len(id) != 40 || len(plan.Operation) == 0 ||
		!hmac.Equal([]byte(id), []byte(confirmedID)) || !hmac.Equal([]byte(id), []byte(s.sign("plan", plan))) {
		return errors.New("ID konfirmasi atau isi rencana tidak sah")
	}
	now := s.now()
	if plan.CreatedAt.After(now) || !plan.ExpiresAt.Equal(plan.CreatedAt.Add(planTTL)) || !now.Before(plan.ExpiresAt) {
		return errors.New("rencana kedaluwarsa atau waktu tidak valid, buat preview baru")
	}
	return nil
}

func ReadPlan(r io.Reader) (Plan, error) {
	const limit = 32 << 10
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || len(data) > limit {
		return Plan{}, errors.New("ukuran atau isi rencana tidak valid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var plan Plan
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("JSON rencana tidak valid: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Plan{}, errors.New("JSON harus berisi satu rencana")
	}
	return plan, nil
}

func (s *Service) sign(purpose string, value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(purpose + "\x00"))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)[:20])
}

func safeText(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

package privread

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Operation string

const (
	SMART       Operation = "smart"
	TRIM        Operation = "trim"
	Power       Operation = "power"
	Temperature Operation = "temperature"
	GPU         Operation = "gpu"
	Firmware    Operation = "firmware"
	Filesystem  Operation = "filesystem"
)

type Scope string

const (
	All     Scope = "all"
	Check   Scope = "check"
	Observe Scope = "observe"
)

var operations = []Operation{SMART, TRIM, Power, Temperature, GPU, Firmware, Filesystem}

const Lifetime = 10 * time.Minute

var (
	ErrUnavailable = errors.New("native authorization is unavailable")
	ErrDenied      = errors.New("administrator authorization was denied or unavailable")
	ErrCancelled   = errors.New("administrator authorization was cancelled")
	ErrExpired     = errors.New("the authorized read session expired")
	ErrProtocol    = errors.New("invalid response from the authorized read helper")
)

type State struct {
	Requested, Active bool
	Scope             Scope
	Availability      domain.Availability
	Err               error
	Reads             uint64
}

type Backend struct {
	Operations []Operation
	Check      func() error
	Start      func(context.Context, Scope, *os.File, io.Writer) (io.ReadWriteCloser, error)
}

type Session struct {
	backend Backend
	demo    bool
	mu      sync.Mutex
	state   State
	client  *client
	cancel  context.CancelFunc
	expiry  *time.Timer
	epoch   uint64
}

func New(backend Backend, demo bool) *Session { return &Session{backend: backend, demo: demo} }

func (s *Session) Operations() []Operation {
	if s.demo {
		return []Operation{SMART, TRIM, Power, Temperature}
	}
	return append([]Operation(nil), s.backend.Operations...)
}

func (s *Session) Capability() error {
	if s.demo {
		return nil
	}
	if s.backend.Start == nil || len(s.backend.Operations) == 0 {
		return ErrUnavailable
	}
	if s.backend.Check != nil {
		return s.backend.Check()
	}
	return nil
}

func (s *Session) Enable(ctx context.Context, scope Scope, terminal *os.File, stderr io.Writer) error {
	if !scope.valid() {
		return ErrProtocol
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	s.mu.Lock()
	s.epoch++
	if s.cancel != nil {
		s.cancel()
	}
	epoch := s.epoch
	s.cancel = cancel
	s.mu.Unlock()

	var connected *client
	err := s.Capability()
	if err == nil && !s.demo {
		var pipe io.ReadWriteCloser
		pipe, err = s.backend.Start(ctx, scope, terminal, stderr)
		if err == nil {
			connected, err = connect(ctx, pipe, scope)
		}
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		err = ErrCancelled
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch || err != nil {
		if connected != nil {
			connected.close()
		}
		if s.epoch != epoch {
			return ErrCancelled
		}
		s.cancel = nil
		if !s.state.Active {
			s.state = disabled(err)
		}
		return err
	}
	s.closeClient()
	s.cancel, s.client = nil, connected
	s.state = State{Requested: true, Active: true, Scope: scope, Availability: domain.Available}
	if connected != nil {
		s.expiry = time.AfterFunc(time.Until(connected.until), func() { s.fail(connected, ErrExpired) })
	}
	return nil
}

func (s *Session) Interrupt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel == nil {
		return false
	}
	s.cancel()
	return true
}

func (s *Session) Disable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
}

func (s *Session) reset() {
	s.epoch++
	if s.cancel != nil {
		s.cancel()
	}
	s.closeClient()
	s.cancel, s.state = nil, State{}
}

func (s *Session) closeClient() {
	if s.client != nil {
		s.client.close()
		s.client = nil
	}
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
}

func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) CanRead(op Operation) bool {
	state := s.State()
	return state.Active && state.Scope.Allows(op) && slices.Contains(s.Operations(), op)
}

func (s *Session) Read(ctx context.Context, op Operation, out any) bool {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil || !c.scope.Allows(op) || !slices.Contains(s.backend.Operations, op) {
		return false
	}
	err := c.read(ctx, op, out)
	if err == nil && !validResult(op, out) {
		err = ErrProtocol
	}
	if err != nil {
		s.fail(c, err)
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != c {
		return false
	}
	s.state.Reads++
	return true
}

func (s *Session) fail(c *client, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == c {
		c.close()
		if s.expiry != nil {
			s.expiry.Stop()
			s.expiry = nil
		}
		reads := s.state.Reads
		s.client, s.state = nil, disabled(err)
		s.state.Reads = reads
	}
}

func disabled(err error) State {
	status := domain.PermissionRequired
	switch {
	case errors.Is(err, ErrUnavailable):
		status = domain.Unsupported
	case errors.Is(err, ErrExpired):
		status = domain.Stale
	case errors.Is(err, ErrProtocol):
		status = domain.Error
	}
	return State{Requested: true, Availability: status, Err: err}
}

func (s Scope) valid() bool {
	return s == All || s == Check || s == Observe || slices.Contains(operations, Operation(s))
}

func (s Scope) Allows(op Operation) bool {
	return slices.Contains(operations, op) && (s == All || s == Scope(op) ||
		s == Check && (op == SMART || op == TRIM || op == Power || op == Temperature) ||
		s == Observe && (op == Power || op == Temperature))
}

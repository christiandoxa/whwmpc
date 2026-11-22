package motion

import (
	"context"
	"time"
)

type Mode string

const (
	Full    Mode = "full"
	Reduced Mode = "reduced"
	Off     Mode = "off"
)

func (m Mode) Valid() bool { return m == Full || m == Reduced || m == Off }

func (m Mode) Next() Mode {
	switch m {
	case Full:
		return Reduced
	case Reduced:
		return Off
	default:
		return Full
	}
}

const (
	FrameInterval = 110 * time.Millisecond
	FocusDuration = 130 * time.Millisecond
	PanelDuration = 190 * time.Millisecond
	IdleInterval  = 9 * time.Second
)

type Tick struct {
	Generation uint64
	At         time.Time
}

type schedule struct {
	generation uint64
	next       time.Time
}

// Scheduler has a single replaceable deadline. Only the event loop calls Set.
type Scheduler struct{ requests chan schedule }

func NewScheduler() *Scheduler { return &Scheduler{requests: make(chan schedule, 1)} }

func (s *Scheduler) Set(generation uint64, next time.Time) {
	select {
	case <-s.requests:
	default:
	}
	s.requests <- schedule{generation, next}
}

func (s *Scheduler) Run(ctx context.Context, send func(Tick)) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var tick <-chan time.Time
	var request schedule
	for {
		select {
		case <-ctx.Done():
			return
		case request = <-s.requests:
			timer.Stop()
			tick = nil
			if !request.next.IsZero() {
				timer.Reset(max(0, time.Until(request.next)))
				tick = timer.C
			}
		case now := <-tick:
			tick = nil
			send(Tick{request.generation, now})
		}
	}
}

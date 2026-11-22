package tui

import (
	"time"

	"charm.land/bubbles/v2/spinner"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
)

type presentation struct {
	pose, rest mascot.Pose
	since      time.Time
	focusUntil time.Time
	panelUntil time.Time
	wakeAt     time.Time
	frame      int
	loop       bool
	generation uint64
}

func (m *Model) show(pose, rest mascot.Pose, loop bool) {
	at := m.now()
	p := &m.visual
	m.spinFor(pose)
	p.pose, p.rest, p.loop, p.since, p.wakeAt, p.frame = pose, rest, loop, at, time.Time{}, 0
	if m.options.Motion != motion.Full {
		p.frame = mascot.Frames(pose) - 1
		if pose == mascot.Greeting || pose == mascot.Farewell {
			p.frame = min(p.frame, 3)
		}
	}
	if m.options.Motion == motion.Full && pose == mascot.Idle && !m.options.Gallery {
		p.wakeAt = at.Add(motion.IdleInterval)
	}
	m.scheduleMotion(at)
}

func (m *Model) accent(focus, panel bool) {
	at := m.now()
	if m.options.Motion == motion.Full {
		if focus {
			m.visual.focusUntil = at.Add(motion.FocusDuration)
		}
		if panel {
			m.visual.panelUntil = at.Add(motion.PanelDuration)
		}
	}
	m.scheduleMotion(at)
}

func (m *Model) scheduleMotion(at time.Time) {
	p := &m.visual
	p.generation++
	if !p.focusUntil.After(at) {
		p.focusUntil = time.Time{}
	}
	if !p.panelUntil.After(at) {
		p.panelUntil = time.Time{}
	}
	next := time.Time{}
	if m.options.Motion == motion.Full {
		take := func(candidate time.Time) {
			if candidate.After(at) && (next.IsZero() || candidate.Before(next)) {
				next = candidate
			}
		}
		if m.canAnimate() && (p.loop || p.rest != "" || p.frame < mascot.Frames(p.pose)-1) {
			scheduled := p.since.Add((time.Duration(p.frame) + 1) * motion.FrameInterval)
			if !scheduled.After(at) {
				scheduled = at.Add(motion.FrameInterval)
			}
			take(scheduled)
		}
		if m.canAnimate() {
			if !p.wakeAt.IsZero() && !p.wakeAt.After(at) {
				take(at.Add(motion.FrameInterval))
			}
			take(p.wakeAt)
			take(p.focusUntil)
			take(p.panelUntil)
		}
	}
	if m.scheduler != nil {
		m.scheduler.Set(p.generation, next)
	}
}

func (m Model) canAnimate() bool { return m.width >= 43 && m.height >= 14 }

func (m *Model) motionTick(msg motion.Tick) {
	p := &m.visual
	if msg.Generation != p.generation || m.options.Motion != motion.Full || !m.canAnimate() {
		return
	}
	if now := m.now(); now.After(msg.At) {
		msg.At = now
	}
	if !p.wakeAt.IsZero() && !msg.At.Before(p.wakeAt) {
		m.show(mascot.Blink, mascot.Idle, false)
		return
	}
	if !msg.At.Before(p.focusUntil) {
		p.focusUntil = time.Time{}
	}
	if !msg.At.Before(p.panelUntil) {
		p.panelUntil = time.Time{}
	}
	if m.busy {
		m.spinner, _ = m.spinner.Update(spinner.TickMsg{Time: msg.At})
	}
	if p.loop || p.rest != "" || mascot.Frames(p.pose) > 1 {
		frame := int(msg.At.Sub(p.since) / motion.FrameInterval)
		switch {
		case frame < mascot.Frames(p.pose) || p.loop:
			p.frame = max(0, frame)
		case p.rest != "":
			rest := p.rest
			p.pose, p.rest, p.loop, p.since, p.frame = rest, "", m.busy && rest == m.activity, msg.At, 0
			m.spinFor(rest)
			if rest == mascot.Idle && !m.options.Gallery {
				p.wakeAt = msg.At.Add(motion.IdleInterval)
			}
		default:
			p.frame = mascot.Frames(p.pose) - 1
		}
	}
	m.scheduleMotion(msg.At)
}

func (m *Model) showingFocus() {
	switch m.focus {
	case inputFocus:
		m.show(mascot.Listening, "", false)
	case menuFocus:
		pose := mascot.LookLeft
		if m.selected%2 == 1 {
			pose = mascot.LookRight
		}
		if m.busy {
			m.show(pose, m.activity, false)
		} else {
			m.show(pose, replyPose(m.page), false)
		}
	default:
		if m.busy {
			m.show(m.activity, "", true)
		} else {
			m.show(replyPose(m.page), "", false)
		}
	}
	m.accent(true, false)
}

func poseFor(intent assistant.Intent) mascot.Pose {
	switch intent {
	case assistant.Storage:
		return mascot.SSD
	case assistant.Trim:
		return mascot.Trim
	case assistant.Power:
		return mascot.CPU
	case assistant.Cleanup:
		return mascot.Broom
	default:
		return mascot.Scanning
	}
}

func replyPose(page Page) mascot.Pose {
	if page.Pose != "" {
		return page.Pose
	}
	switch page.Availability {
	case domain.Error:
		return mascot.Error
	case domain.DependencyMissing, domain.PermissionRequired, domain.Unsupported, domain.Unknown, domain.Stale:
		return mascot.Unknown
	case domain.Partial:
		return mascot.Warning
	default:
		return mascot.Idle
	}
}

func (m *Model) showReply() {
	pose := replyPose(m.page)
	m.show(pose, "", false)
	m.accent(false, true)
}

func (m *Model) spinFor(pose mascot.Pose) {
	m.spinner = spinner.New(spinner.WithSpinner(spinner.Points))
	if m.options.ASCII {
		m.spinner.Spinner = spinner.Line
		return
	}
	switch pose {
	case mascot.Cleaning:
		m.spinner.Spinner = spinner.Spinner{Frames: []string{" / ", " /·", "·/ "}, FPS: motion.FrameInterval}
	case mascot.Applying, mascot.Restore:
		m.spinner.Spinner = spinner.Spinner{Frames: []string{"(+)", "(×)"}, FPS: motion.FrameInterval}
	case mascot.SSD, mascot.Scanning:
		m.spinner.Spinner = spinner.Spinner{Frames: []string{"◉─ ", " ◉─"}, FPS: motion.FrameInterval}
	}
}

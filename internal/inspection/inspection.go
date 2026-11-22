package inspection

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type Scanner struct {
	platform.Provider
	runner sysrun.Runner
	demo   bool
	cache  sync.Mutex
	smart  storage.SMARTAudit
}

func New(demo bool) *Scanner {
	var provider platform.Provider = platform.Native{}
	if demo {
		provider = platform.Demo{}
	}
	return &Scanner{Provider: provider, runner: sysrun.Native{}, demo: demo}
}

func (s *Scanner) Now() time.Time {
	if s.demo {
		return platform.DemoTime
	}
	return time.Now().UTC()
}

func (s *Scanner) SMART(ctx context.Context, fresh bool) storage.SMARTAudit {
	if s.demo {
		return demoSMART()
	}
	s.cache.Lock()
	defer s.cache.Unlock()
	const smartInterval = 2 * time.Minute
	if !fresh && !s.smart.CapturedAt.IsZero() && time.Since(s.smart.CapturedAt) < smartInterval {
		return s.smart
	}
	audit := (storage.Collector{Runner: s.runner}).InspectSMART(ctx)
	if ctx.Err() == nil {
		s.smart = audit
	}
	return audit
}

func (s *Scanner) TRIM(ctx context.Context) storage.TrimAudit {
	if s.demo {
		return demoTrim()
	}
	timeout, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return (storage.Collector{Runner: s.runner}).InspectTrim(timeout)
}

func (s *Scanner) Power(ctx context.Context) power.Snapshot {
	if s.demo {
		return demoPower()
	}
	timeout, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return power.Scan(timeout, os.DirFS("/"), s.runner)
}

type Doctor struct {
	Demo         bool                           `json:"demo"`
	Overview     domain.Overview                `json:"overview"`
	Tools        map[string]domain.Availability `json:"tools"`
	Capabilities map[string]domain.Availability `json:"capabilities"`
}

func (s *Scanner) Doctor(ctx context.Context) Doctor {
	o := s.Overview(ctx)
	d := Doctor{Demo: s.demo, Overview: o, Tools: map[string]domain.Availability{}, Capabilities: map[string]domain.Availability{}}
	for _, tool := range []string{"smartctl", "fstrim", "systemctl", "journalctl", "busctl", "apt-get", "apt-config", "sudo"} {
		status := domain.DependencyMissing
		switch {
		case s.demo:
			status = domain.Available
		case o.OS != "linux":
			status = domain.Unsupported
		default:
			for _, name := range []string{tool, filepath.Join("/usr/sbin", tool), filepath.Join("/sbin", tool)} {
				if _, err := s.runner.LookPath(name); err == nil {
					status = domain.Available
					break
				}
			}
		}
		d.Tools[tool] = status
	}
	trim, power := s.TRIM(ctx), s.Power(ctx)
	d.Capabilities["smart"] = s.SMART(ctx, false).Availability
	d.Capabilities["trim"] = trim.Availability
	d.Capabilities["trim_schedule"] = trim.Timer.Availability
	d.Capabilities["cpufreq"] = power.CPUFreqStatus
	d.Capabilities["power_profiles"] = power.Profiles.Status
	return d
}

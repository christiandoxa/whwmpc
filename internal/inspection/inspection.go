package inspection

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/privread"
	"github.com/christiandoxa/whwmpc/internal/storage"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type Scanner struct {
	platform.Provider
	Privilege           *privread.Session
	DemoSMARTPermission bool
	runner              sysrun.Runner
	demo                bool
	cache               sync.Mutex
	smart               storage.SMARTAudit
}

func (s *Scanner) Overview(ctx context.Context) domain.Overview {
	return s.withTemperature(ctx, s.Provider.Overview(ctx))
}

func (s *Scanner) Observe(ctx context.Context, interval time.Duration) domain.Overview {
	return s.withTemperature(ctx, s.Provider.Observe(ctx, interval))
}

func (s *Scanner) withTemperature(ctx context.Context, o domain.Overview) domain.Overview {
	if o.Temperature.Celsius == nil || o.Temperature.Availability != domain.Available {
		if t, ok := s.PrivilegedTemperature(ctx); ok && (t.Celsius != nil || o.Temperature.Celsius == nil) {
			o.Temperature = t
		}
	}
	return o
}

func (s *Scanner) PrivilegedTemperature(ctx context.Context) (domain.Temperature, bool) {
	var reading domain.Temperature
	ok := s.read(ctx, privread.Temperature, &reading)
	return reading, ok
}

func (s *Scanner) read(ctx context.Context, op privread.Operation, out any) bool {
	return !s.demo && s.Privilege != nil && ctx.Err() == nil && s.Privilege.Read(ctx, op, out)
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

func (s *Scanner) CPUTemperatureProbe() func(context.Context) *float64 {
	source, ok := s.Provider.(interface {
		CPUTemperatureProbe() func(context.Context) *float64
	})
	if !ok || s.demo {
		return nil
	}
	return source.CPUTemperatureProbe()
}

func (s *Scanner) SMART(ctx context.Context, fresh bool) storage.SMARTAudit {
	if s.demo {
		audit := demoSMART()
		if s.DemoSMARTPermission && (s.Privilege == nil || !s.Privilege.CanRead(privread.SMART)) {
			audit.Availability = domain.PermissionRequired
			audit.Drives[0].Availability = domain.PermissionRequired
			audit.Drives[0].Health = nil
		}
		return audit
	}
	var privileged storage.SMARTAudit
	if s.read(ctx, privread.SMART, &privileged) {
		return privileged
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
	var privileged privread.TrimReading
	if s.read(ctx, privread.TRIM, &privileged) {
		for i, path := range privileged.Paths {
			m := &privileged.Audit.Mounts[i]
			m.Point, m.Source, m.Root, m.MountOptions = path.Point, path.Source, path.Root, path.Options
		}
		return privileged.Audit
	}
	timeout, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return (storage.Collector{Runner: s.runner}).InspectTrim(timeout)
}

func (s *Scanner) Power(ctx context.Context) power.Snapshot {
	if s.demo {
		return demoPower()
	}
	var privileged privread.PowerReading
	if s.read(ctx, privread.Power, &privileged) {
		privileged.Snapshot.OS = runtime.GOOS
		privileged.Snapshot.Profiles.Target, privileged.Snapshot.Profiles.API = privileged.Target, privileged.API
		return privileged.Snapshot
	}
	timeout, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return power.Scan(timeout, os.DirFS("/"), s.runner)
}

func (s *Scanner) GPU(ctx context.Context) hardware.Snapshot {
	var privileged privread.GPUReading
	if s.read(ctx, privread.GPU, &privileged) {
		for i, id := range privileged.IDs {
			privileged.Snapshot.Devices[i].ID = id
		}
		return privileged.Snapshot
	}
	return hardware.Scan(ctx, s.runner)
}

func (s *Scanner) Firmware(ctx context.Context, localUpdates bool) firmware.Snapshot {
	var privileged firmware.Snapshot
	if !localUpdates && s.read(ctx, privread.Firmware, &privileged) {
		return privileged
	}
	return firmware.Collect(ctx, s.runner, localUpdates)
}

func (s *Scanner) Filesystem(ctx context.Context) filesystem.Snapshot {
	var privileged filesystem.Snapshot
	if s.read(ctx, privread.Filesystem, &privileged) {
		return privileged
	}
	return filesystem.Discover(ctx, s.runner)
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
	for _, tool := range []string{"smartctl", "fstrim", "busybox", "systemctl", "journalctl", "busctl", "apt-get", "apt-config", "sudo"} {
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

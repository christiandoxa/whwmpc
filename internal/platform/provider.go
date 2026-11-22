package platform

import (
	"context"
	"runtime"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Provider interface {
	Overview(context.Context) domain.Overview
}

type PowerReading struct {
	OnAC          *bool
	Percent       *float64
	BatteryStatus string
	Availability  domain.Availability
}

func baseOverview(now time.Time, status domain.Availability) domain.Overview {
	return domain.Overview{
		Availability: status, CapturedAt: now.UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		LogicalCPUs: runtime.NumCPU(), CPU: domain.Percent{Availability: status},
		Memory:      domain.Memory{Availability: status, SwapStatus: status, Use: domain.Percent{Availability: status}},
		Temperature: domain.Temperature{Availability: status}, DiskStatus: status,
		ProcessStatus: status, PowerStatus: status,
	}
}

type Demo struct{}

var DemoTime = time.Date(2026, time.November, 22, 9, 41, 0, 0, time.UTC)

func (Demo) Overview(context.Context) domain.Overview {
	up, battery, temp, ac := 56240.0, 78.0, 54.0, true
	o := baseOverview(DemoTime, domain.Available)
	o.OS, o.Arch, o.Distro, o.DistroID = "linux", "amd64", "Ubuntu 24.04 LTS", "ubuntu"
	o.Kernel, o.Environment, o.Systemd = "6.8.0-demo", "physical", true
	o.UptimeSeconds, o.CPUModel, o.LogicalCPUs = &up, "Demo CPU", 12
	o.CPU = domain.Percent{Availability: domain.Available, StartedAt: DemoTime.Add(-800 * time.Millisecond),
		CapturedAt: DemoTime, Samples: []float64{13, 17, 15, 26, 22, 38, 34, 24}}
	o.Memory = domain.Memory{Availability: domain.Available, TotalBytes: 16 << 30, FreeBytes: 7 << 30,
		SwapStatus: domain.Available, SwapBytes: 4 << 30, Use: domain.Percent{Availability: domain.Available, StartedAt: o.CPU.StartedAt,
			CapturedAt: DemoTime, Samples: []float64{54, 55, 56}}}
	o.Disks = []domain.Disk{{Mount: "/", Root: true, FSType: "ext4", Availability: domain.Available,
		TotalBytes: 512 << 30, FreeBytes: 148 << 30, UsedPercent: 71}}
	o.Temperature = domain.Temperature{Availability: domain.Available, Celsius: &temp, Source: "coretemp"}
	o.OnAC, o.Battery = &ac, &battery
	o.Processes = []domain.Process{
		{Name: "build-worker", CPU: 14, RSS: 720 << 20},
		{Name: "browser", CPU: 5, RSS: 1024 << 20},
	}
	return o
}

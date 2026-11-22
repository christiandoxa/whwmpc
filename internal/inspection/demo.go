package inspection

import (
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

func demoSMART() storage.SMARTAudit {
	return storage.SMARTAudit{
		Availability: domain.Available, CapturedAt: platform.DemoTime,
		Drives: []storage.Drive{{
			Availability: domain.Available, CapturedAt: platform.DemoTime, Name: "nvme0n1",
			DeviceID: "259:0", Model: "Demo NVMe 512 GB", Transport: "NVMe", Protocol: "NVMe", MountIDs: []uint64{24},
			Health: &storage.SMART{Passed: new(true), CriticalWarning: new(uint64(0)), TemperatureC: new(int64(39)),
				Spare: new(uint64(100)), SpareThreshold: new(uint64(10)), PercentageUsed: new(uint64(9)),
				MediaErrors: "0", UnsafeShutdowns: "2", PowerOnHours: "1802", DataUnitsRead: "41823800", DataUnitsWritten: "11751820"},
		}},
	}
}

func demoTrim() storage.TrimAudit {
	return storage.TrimAudit{
		Availability: domain.Available, CapturedAt: platform.DemoTime, Manual: domain.Available,
		Mounts: []storage.Mount{{
			ID: 24, DeviceID: "259:1", Point: "/", Root: "/", Source: "/dev/nvme0n1p1", FSType: "ext4",
			Discard: domain.Available, Evidence: "Fixture queue DISC-MAX > 0",
			Layers:   []storage.BlockLayer{{Name: "nvme0n1", DeviceID: "259:0", MaxDiscardBytes: new(uint64(2147483648))}},
			LastTrim: &storage.TrimRecord{RecordedAt: platform.DemoTime.Add(-48 * time.Hour), PotentialDiscardBytes: 460 << 20},
		}},
		Timer: storage.Timer{Availability: domain.Available, Journal: domain.Available, Scheduler: "systemd",
			LoadState: "loaded", UnitFileState: "enabled", ActiveState: "active",
			LastTrigger: "2026-11-20 09:41 UTC", NextElapse: "2026-11-27 09:41 UTC",
			LastServiceExit: "2026-11-20 09:43 UTC", ServiceResult: "success", ServiceExitCode: new(0),
		},
	}
}

func demoPower() power.Snapshot {
	return power.Snapshot{
		At: platform.DemoTime, CPUFreqStatus: domain.Available, PowerStatus: domain.Available, PowerSource: "ac",
		Boost: new(true), BoostSource: "fixture", PlatformProfile: "balanced", PlatformChoices: []string{"low-power", "balanced", "performance"},
		Batteries: []power.Battery{{Name: "BAT0", Percent: new(uint64(78)), Status: "Charging"}},
		Policies: []power.Policy{{Name: "policy0", CPUs: []string{"0"}, Driver: "intel_pstate", Governor: "powersave",
			EPP: "balance_performance", ObservedKHz: new(uint64(1680000)), DriverKHz: new(uint64(2133000)),
			ConfigMinKHz: new(uint64(400000)), ConfigMaxKHz: new(uint64(4600000)),
			HardwareMinKHz: new(uint64(400000)), HardwareMaxKHz: new(uint64(4800000)),
		}},
		Profiles: power.Profiles{Status: domain.Available, ManagerStatus: domain.Available,
			Active: "balanced", Available: []string{"power-saver", "balanced", "performance"},
			Backend: "power-profiles-daemon", Holds: new(0), Managers: []string{"power-profiles-daemon"}},
	}
}

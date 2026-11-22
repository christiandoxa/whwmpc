package platform

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"golang.org/x/sys/unix"
)

type Native struct{}

func (Native) Overview(ctx context.Context) domain.Overview {
	runner := sysrun.Native{}
	o := baseOverview(time.Now(), domain.Unknown)
	o.Distro, o.DistroID = "macOS", "darwin"
	if version, err := unix.Sysctl("kern.osproductversion"); err == nil {
		o.Distro += " " + domain.SafeText(version)
	}
	kernel, _ := unix.Sysctl("kern.osrelease")
	cpu, _ := unix.Sysctl("machdep.cpu.brand_string")
	o.Kernel, o.CPUModel = domain.SafeText(kernel), domain.SafeText(cpu)
	if boot, err := unix.SysctlTimeval("kern.boottime"); err == nil && boot.Sec > 0 {
		seconds := time.Since(time.Unix(boot.Sec, 0)).Seconds()
		if seconds >= 0 {
			o.UptimeSeconds = &seconds
		}
	}

	o.CPU.StartedAt = time.Now().UTC()
	sample := runner.Run(ctx, "/usr/bin/top", "-l", "4", "-s", "1", "-n", "0", "-F", "-R")
	o.CPU.CapturedAt = time.Now().UTC()
	o.CPU.Availability = runAvailability(sample)
	if o.CPU.Availability == domain.Available {
		o.CPU.Samples = darwinCPUSamples(string(sample.Output))
		switch len(o.CPU.Samples) {
		case 0:
			o.CPU.Availability = domain.Unknown
		case 1, 2:
			o.CPU.Availability = domain.Partial
		}
	}
	o.Memory = darwinMemory(ctx, runner)
	o.Disks, o.DiskStatus = darwinDisks()
	o.OnAC, o.Battery, o.PowerStatus = ReadPower(ctx, runner)

	processes := runner.Run(ctx, "/bin/ps", "-A", "-r", "-o", "pcpu=", "-o", "rss=", "-o", "comm=")
	o.ProcessStatus = runAvailability(processes)
	if o.ProcessStatus == domain.Available {
		o.Processes = darwinPS(string(processes.Output), o.LogicalCPUs)
		if len(o.Processes) != 0 {
			o.ProcessStatus = domain.Partial // ps reports a decaying average, not a sampled interval.
		} else {
			o.ProcessStatus = domain.Unknown
		}
	}
	o.Temperature.Availability = domain.Unsupported
	o.CapturedAt = time.Now().UTC()
	o.Availability = domain.Partial
	return o
}

func darwinMemory(ctx context.Context, runner sysrun.Runner) domain.Memory {
	m := domain.Memory{Availability: domain.Unknown, SwapStatus: domain.Unknown,
		Use: domain.Percent{Availability: domain.Unknown, StartedAt: time.Now().UTC()}}
	out := runner.Run(ctx, "/usr/bin/vm_stat")
	m.Use.CapturedAt = time.Now().UTC()
	total, err := unix.SysctlUint64("hw.memsize")
	availability := runAvailability(out)
	if err != nil {
		m.Availability = nativeAvailability(err)
	} else if availability != domain.Available {
		m.Availability = availability
	} else if free, ok := darwinFreeMemory(string(out.Output)); ok && total != 0 && free <= total {
		// Free pages are a lower bound. This is not macOS memory pressure.
		m.Availability, m.TotalBytes, m.FreeBytes = domain.Partial, total, free
		m.Use.Availability, m.Use.Samples = domain.Partial, percentSample(free, total)
	}
	if swap, err := unix.SysctlRaw("vm.swapusage"); err == nil && len(swap) >= 24 {
		total, used := binary.LittleEndian.Uint64(swap), binary.LittleEndian.Uint64(swap[16:])
		if used <= total {
			m.SwapStatus, m.SwapBytes, m.SwapUsed = domain.Available, total, used
		}
	}
	return m
}

func darwinDisks() ([]domain.Disk, domain.Availability) {
	// The APFS system and data volumes can share a container. Do not sum their capacities.
	var stat unix.Statfs_t
	if err := unix.Statfs("/", &stat); err != nil {
		return nil, nativeAvailability(err)
	}
	var total, free uint64
	if stat.Bsize != 0 && stat.Blocks <= math.MaxUint64/uint64(stat.Bsize) && stat.Bavail <= stat.Blocks {
		total, free = stat.Blocks*uint64(stat.Bsize), stat.Bavail*uint64(stat.Bsize)
	}
	filesystem := unix.ByteSliceToString(stat.Fstypename[:])
	volume := nativeDisk("/", filesystem, true, total, free)
	return []domain.Disk{volume}, volume.Availability
}

func ReadPower(ctx context.Context, runner sysrun.Runner) (*bool, *float64, domain.Availability) {
	if runner == nil {
		runner = sysrun.Native{}
	}
	result := runner.Run(ctx, "/usr/bin/pmset", "-g", "batt")
	if status := runAvailability(result); status != domain.Available {
		return nil, nil, status
	}
	return darwinPower(string(result.Output))
}

func runAvailability(out sysrun.Result) domain.Availability {
	if out.Err != nil {
		return nativeAvailability(out.Err)
	}
	switch out.ExitCode {
	case 0:
		return domain.Available
	case 126:
		return domain.PermissionRequired
	case 127:
		return nativeAvailability(os.ErrNotExist)
	default:
		return domain.Error
	}
}

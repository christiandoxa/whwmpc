package platform

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type Native struct{}

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	getSystemTimes       = kernel32.NewProc("GetSystemTimes")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	getSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
	getTickCount64       = kernel32.NewProc("GetTickCount64")
	getProcessMemoryInfo = kernel32.NewProc("K32GetProcessMemoryInfo")
)

func (Native) Overview(ctx context.Context) domain.Overview {
	start := time.Now().UTC()
	o := baseOverview(start, domain.Unknown)
	version := windows.RtlGetVersion()
	o.DistroID = "windows"
	o.Kernel = fmt.Sprintf("%d.%d.%d", version.MajorVersion, version.MinorVersion, version.BuildNumber)
	o.Distro = "Windows " + o.Kernel
	if cpu, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE); err == nil {
		name, _, _ := cpu.GetStringValue("ProcessorNameString")
		o.CPUModel = domain.SafeText(name)
		cpu.Close()
	}

	if ticks, _, _ := getTickCount64.Call(); ticks != 0 {
		seconds := float64(ticks) / 1000
		o.UptimeSeconds = &seconds
	}
	o.Disks, o.DiskStatus = windowsDisks()
	power := ReadPower(ctx, nil)
	o.OnAC, o.Battery, o.PowerStatus = power.OnAC, power.Percent, power.Availability
	o.Temperature.Availability = domain.Unsupported

	o.CPU.StartedAt = time.Now().UTC()
	o.Memory.Use.StartedAt = o.CPU.StartedAt
	firstProc := windowsProcessSnapshot(ctx)
	before, beforeOK := systemTicks()
	lastProc := firstProc
	lastProcAt := time.Now()
loop:
	for range 3 {
		timer := time.NewTimer(240 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			break loop
		case <-timer.C:
		}
		after, ok := systemTicks()
		if busy, valid := windowsBusy(before, after); beforeOK && ok && valid {
			o.CPU.Samples = append(o.CPU.Samples, busy)
		}
		before, beforeOK = after, ok
		o.CPU.CapturedAt = time.Now().UTC()
		if total, free, status := windowsMemory(); status == domain.Available {
			o.Memory.Availability, o.Memory.TotalBytes, o.Memory.FreeBytes = status, total, free
			o.Memory.Use.Samples = append(o.Memory.Use.Samples, percentSample(free, total)...)
			o.Memory.Use.CapturedAt = time.Now().UTC()
		} else if len(o.Memory.Use.Samples) == 0 {
			o.Memory.Availability = status
		}
	}
	o.CPU.Availability = sampleAvailability(len(o.CPU.Samples))
	// GetSystemTimes covers the current processor group on machines with more than 64 logical CPUs.
	if o.LogicalCPUs > 64 && o.CPU.Availability == domain.Available {
		o.CPU.Availability = domain.Partial
	}
	o.Memory.Use.Availability = sampleAvailability(len(o.Memory.Use.Samples))
	if len(o.Memory.Use.Samples) == 0 {
		o.Memory.Use.Availability = o.Memory.Availability
	}
	if ctx.Err() == nil {
		lastProc = windowsProcessSnapshot(ctx)
	}
	o.Processes = windowsProcesses(firstProc, lastProc, time.Since(lastProcAt), o.LogicalCPUs)
	if len(o.Processes) > 0 {
		o.ProcessStatus = domain.Partial // Protected processes and processes exiting during the window are omitted.
	}
	o.Availability = domain.Partial
	o.CapturedAt = time.Now().UTC()
	return o
}

type tickCounts struct{ idle, kernel, user uint64 }

func systemTicks() (tickCounts, bool) {
	var idle, kernel, user windows.Filetime
	ok, _, _ := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	return tickCounts{ticks(idle), ticks(kernel), ticks(user)}, ok != 0
}

func ticks(t windows.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }

func windowsBusy(before, after tickCounts) (float64, bool) {
	if after.idle < before.idle || after.kernel < before.kernel || after.user < before.user {
		return 0, false
	}
	idle := after.idle - before.idle
	total := (after.kernel - before.kernel) + (after.user - before.user)
	if total == 0 || idle > total {
		return 0, false
	}
	return 100 * float64(total-idle) / float64(total), true
}

func sampleAvailability(samples int) domain.Availability {
	switch samples {
	case 0:
		return domain.Unknown
	case 1, 2:
		return domain.Partial
	default:
		return domain.Available
	}
}

type memoryStatusEx struct {
	length, load                                   uint32
	totalPhys, availPhys, totalCommit, availCommit uint64
	totalVirtual, availVirtual, availExtended      uint64
}

func windowsMemory() (uint64, uint64, domain.Availability) {
	var state memoryStatusEx
	state.length = uint32(unsafe.Sizeof(state))
	ok, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&state)))
	if ok == 0 {
		return 0, 0, nativeAvailability(err)
	}
	if state.totalPhys == 0 || state.availPhys > state.totalPhys {
		return 0, 0, domain.Unknown
	}
	return state.totalPhys, state.availPhys, domain.Available
}

func windowsDisks() ([]domain.Disk, domain.Availability) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, nativeAvailability(err)
	}
	system, _ := windows.GetSystemWindowsDirectory()
	system = filepath.VolumeName(system)
	var drives []domain.Disk
	status := domain.Available
	for i := range 26 {
		if mask&(1<<i) == 0 {
			continue
		}
		letter := string(rune('A' + i))
		mount := letter + `:\`
		p := windows.StringToUTF16Ptr(mount)
		if windows.GetDriveType(p) != windows.DRIVE_FIXED {
			continue
		}
		root := strings.EqualFold(system, letter+":")
		var total, free uint64
		var name [64]uint16
		err := windows.GetDiskFreeSpaceEx(p, &free, &total, nil)
		_ = windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &name[0], uint32(len(name)))
		drive := nativeDisk(mount, windows.UTF16ToString(name[:]), root, total, free)
		if err != nil {
			drive.Availability = nativeAvailability(err)
		}
		if drive.Availability != domain.Available {
			status = domain.Partial
		}
		drives = append(drives, drive)
	}
	if len(drives) == 0 {
		status = domain.Unknown
	}
	return drives, status
}

type powerStatus struct {
	acLine, batteryFlags, batteryPercent, systemStatus uint8
	batteryLifeTime, batteryFullLifeTime               uint32
}

func ReadPower(_ context.Context, _ sysrun.Runner) PowerReading {
	var state powerStatus
	ok, _, err := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&state)))
	if ok == 0 {
		return PowerReading{Availability: nativeAvailability(err)}
	}
	return windowsPower(state)
}

func windowsPower(state powerStatus) PowerReading {
	const (
		charging  = 8
		noBattery = 128
		unknown   = 255
	)
	reading := PowerReading{Availability: domain.Unknown}
	if state.acLine <= 1 {
		reading.OnAC = new(state.acLine == 1)
	}
	if state.batteryFlags == unknown || state.batteryFlags&noBattery == 0 {
		if state.batteryPercent <= 100 {
			reading.Percent = new(float64(state.batteryPercent))
		}
		if state.batteryFlags != unknown && state.batteryFlags&charging != 0 {
			reading.BatteryStatus = "Charging"
		}
	}
	if reading.OnAC != nil || reading.Percent != nil || reading.BatteryStatus != "" {
		reading.Availability = domain.Available
	}
	return reading
}

type memoryCounters struct {
	size, faults                                                                         uint32
	peakWorking, rss                                                                     uintptr
	quotaPeakPaged, quotaPaged, quotaPeakNonPaged, quotaNonPaged, pagefile, peakPagefile uintptr
}

type windowsProc struct {
	created, total, rss uint64
	name                string
}

func windowsProcessSnapshot(ctx context.Context) map[uint32]windowsProc {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	out := make(map[uint32]windowsProc)
	for err := windows.Process32First(snap, &entry); err == nil && ctx.Err() == nil; err = windows.Process32Next(snap, &entry) {
		if entry.ProcessID == 0 {
			continue
		}
		if data, ok := windowsProcess(entry); ok {
			out[entry.ProcessID] = data
		}
	}
	return out
}

func windowsProcess(entry windows.ProcessEntry32) (windowsProc, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
	if err != nil {
		return windowsProc{}, false
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return windowsProc{}, false
	}
	var memory memoryCounters
	memory.size = uint32(unsafe.Sizeof(memory))
	ok, _, _ := getProcessMemoryInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&memory)), uintptr(memory.size))
	if ok == 0 {
		return windowsProc{}, false
	}
	return windowsProc{ticks(created), ticks(kernel) + ticks(user), uint64(memory.rss), domain.SafeText(windows.UTF16ToString(entry.ExeFile[:]))}, true
}

func windowsProcesses(before, after map[uint32]windowsProc, elapsed time.Duration, cpus int) []domain.Process {
	if elapsed <= 0 || cpus <= 0 {
		return nil
	}
	var out []domain.Process
	capacity := float64(elapsed.Nanoseconds()) / 100 * float64(cpus)
	for pid, now := range after {
		old, ok := before[pid]
		if !ok || now.created != old.created || now.total < old.total || now.name == "" {
			continue
		}
		cpu := float64(now.total-old.total) * 100 / capacity
		if goodPercent(cpu) {
			out = append(out, domain.Process{Name: now.name, CPU: cpu, RSS: now.rss})
		}
	}
	return topNativeProcesses(out)
}

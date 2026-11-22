package report

import (
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

var kernelVersion = regexp.MustCompile(`^[0-9]{1,4}(?:\.[0-9]{1,4}){1,3}(?:-[0-9]{1,5})?(?:-(?:generic|amd64|arm64|lowlatency))?`)
var releaseVersion = regexp.MustCompile(`^[0-9]{1,4}(?:\.[0-9]{1,4}){0,3}\b`)
var blockNumber = regexp.MustCompile(`^[0-9]{1,10}:[0-9]{1,10}$`)
var counter = regexp.MustCompile(`^[0-9]{1,39}$`)
var policyNumber = regexp.MustCompile(`^policy[0-9]{1,8}$`)
var cpuNumber = regexp.MustCompile(`^cpu[0-9]{1,8}$`)
var cpuIndex = regexp.MustCompile(`^[0-9]{1,8}$`)
var scheduleStamp = regexp.MustCompile(`^(?:[A-Z][a-z]{2} )?[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?: [A-Z0-9+:-]{1,6})?$|^[0-9]{1,20}$`)

func allowed(value string, values ...string) string {
	if slices.Contains(values, value) {
		return value
	}
	return ""
}

func choices(values []string, permits ...string) []string {
	var out []string
	for _, value := range values {
		if allowed(value, permits...) != "" {
			out = append(out, value)
		}
	}
	return out
}

func checked(value string, pattern *regexp.Regexp) string {
	if pattern.MatchString(value) {
		return value
	}
	return ""
}

func availability(status domain.Availability) domain.Availability {
	switch status {
	case domain.Available, domain.Partial, domain.Unsupported, domain.PermissionRequired, domain.DependencyMissing, domain.Error, domain.Stale:
		return status
	default:
		return domain.Unknown
	}
}

func fsType(value string) string {
	return allowed(value, "ext2", "ext3", "ext4", "xfs", "btrfs", "vfat", "exfat", "f2fs", "ntfs", "ntfs3",
		"apfs", "hfs", "refs", "zfs", "bcachefs", "overlay", "tmpfs", "ramfs", "squashfs", "erofs", "fuseblk", "apfs", "nfs", "nfs4", "cifs", "9p", "virtiofs")
}

func numeric(value *float64, low, high float64) *float64 {
	if value == nil || math.IsNaN(*value) || *value < low || *value > high {
		return nil
	}
	return new(*value)
}

func percent(value domain.Percent) domain.Percent {
	out := domain.Percent{Availability: availability(value.Availability), StartedAt: value.StartedAt.UTC(), CapturedAt: value.CapturedAt.UTC()}
	if value.Scope == domain.PrimaryProcessorGroup {
		out.Scope = value.Scope
	}
	for _, sample := range value.Samples {
		if sample >= 0 && sample <= 100 {
			out.Samples = append(out.Samples, sample)
		}
	}
	if len(out.Samples) != len(value.Samples) && out.Availability == domain.Available {
		out.Availability = domain.Partial
	}
	return out
}

func projectOverview(o domain.Overview, private map[string]bool) *domain.Overview {
	addPrivate(private, o.CPUModel)
	distros := map[string]string{
		"darwin": "macOS", "windows": "Windows",
		"ubuntu": "Ubuntu", "debian": "Debian GNU/Linux", "zorin": "Zorin OS", "linuxmint": "Linux Mint",
		"fedora": "Fedora Linux", "rhel": "Red Hat Enterprise Linux", "rocky": "Rocky Linux", "almalinux": "AlmaLinux",
		"arch": "Arch Linux", "manjaro": "Manjaro", "endeavouros": "EndeavourOS", "opensuse-tumbleweed": "openSUSE Tumbleweed",
		"opensuse-leap": "openSUSE Leap", "alpine": "Alpine Linux", "nixos": "NixOS",
	}
	id, distro := o.DistroID, distros[o.DistroID]
	if distro == "" {
		id = ""
		addPrivate(private, o.Distro, o.DistroID)
	} else if suffix, ok := strings.CutPrefix(o.Distro, distro+" "); ok {
		if version := releaseVersion.FindString(suffix); version != "" {
			distro += " " + version
		}
	}
	out := &domain.Overview{
		Availability: availability(o.Availability), CapturedAt: o.CapturedAt.UTC(), OS: allowed(o.OS, "linux", "darwin", "windows"),
		Distro: distro, DistroID: id, Kernel: kernelVersion.FindString(o.Kernel), Arch: allowed(o.Arch, "amd64", "arm64", "386", "arm", "riscv64"),
		Environment: allowed(o.Environment, "physical", "container", "wsl", "virtual_machine", "undetermined"), Systemd: o.Systemd,
		UptimeSeconds: numeric(o.UptimeSeconds, 0, math.MaxFloat64), LogicalCPUs: o.LogicalCPUs, CPU: percent(o.CPU),
		Memory: domain.Memory{Availability: availability(o.Memory.Availability), TotalBytes: o.Memory.TotalBytes,
			FreeBytes: o.Memory.FreeBytes, SwapStatus: availability(o.Memory.SwapStatus), SwapBytes: o.Memory.SwapBytes, SwapUsed: o.Memory.SwapUsed, Use: percent(o.Memory.Use)},
		DiskStatus: availability(o.DiskStatus),
		Temperature: domain.Temperature{Availability: availability(o.Temperature.Availability), Celsius: numeric(o.Temperature.Celsius, -30, 150),
			Source: allowed(o.Temperature.Source, "coretemp", "k10temp", "zenpower", "k8temp", "cpu_thermal", "x86_pkg_temp", "soc_thermal")},
		PowerStatus: availability(o.PowerStatus), OnAC: o.OnAC, Battery: numeric(o.Battery, 0, 100),
		ProcessStatus: availability(o.ProcessStatus),
	}
	for _, d := range o.Disks {
		addPrivate(private, d.Mount)
		safe := domain.Disk{FSType: fsType(d.FSType), Root: d.Root, Availability: availability(d.Availability),
			TotalBytes: d.TotalBytes, FreeBytes: d.FreeBytes}
		if d.UsedPercent >= 0 && d.UsedPercent <= 100 {
			safe.UsedPercent = d.UsedPercent
		} else {
			safe.Availability = domain.Partial
		}
		out.Disks = append(out.Disks, safe)
	}
	for _, p := range o.Processes {
		addPrivate(private, p.Name)
		if p.CPU >= 0 && p.CPU <= 100 {
			out.Processes = append(out.Processes, domain.Process{CPU: p.CPU, RSS: p.RSS})
		}
	}
	return out
}

func projectSMART(audit storage.SMARTAudit, private map[string]bool) *storage.SMARTAudit {
	addPrivate(private, audit.Detail)
	out := &storage.SMARTAudit{Source: allowed(audit.Source, storage.DiskUtilitySource, storage.WindowsStorageSource), Availability: availability(audit.Availability), CapturedAt: audit.CapturedAt.UTC()}
	for _, drive := range audit.Drives {
		addPrivate(private, drive.Name, drive.Model, drive.Detail)
		item := storage.Drive{
			Availability: availability(drive.Availability), CapturedAt: drive.CapturedAt.UTC(), DeviceID: checked(drive.DeviceID, blockNumber),
			Transport: allowed(drive.Transport, "NVMe", "USB", "SATA", "SAS", "ATA", "SCSI", "Virtual", "MMC", "PCIe"),
			Protocol:  allowed(drive.Protocol, "NVMe", "ATA", "SCSI"), HealthStatus: allowed(drive.HealthStatus, "healthy", "warning", "unhealthy"), Rotational: drive.Rotational, MountIDs: slices.Clone(drive.MountIDs),
		}
		if h := drive.Health; h != nil {
			item.Health = &storage.SMART{
				Passed: h.Passed, CriticalWarning: h.CriticalWarning, TemperatureC: h.TemperatureC, Spare: h.Spare,
				SpareThreshold: h.SpareThreshold, PercentageUsed: h.PercentageUsed, MediaErrors: checked(h.MediaErrors, counter),
				UnsafeShutdowns: checked(h.UnsafeShutdowns, counter), PowerOnHours: checked(h.PowerOnHours, counter),
				ReadErrorsUncorrected: checked(h.ReadErrorsUncorrected, counter), WriteErrorsUncorrected: checked(h.WriteErrorsUncorrected, counter),
				DataUnitsRead: checked(h.DataUnitsRead, counter), DataUnitsWritten: checked(h.DataUnitsWritten, counter), ExitCode: h.ExitCode,
				ExitFlags: choices(h.ExitFlags, "invalid_arguments", "device_open_or_identification_failed", "smart_command_or_checksum_failed",
					"disk_failing", "current_prefailure_threshold", "historical_attribute_threshold", "error_log_records", "self_test_log_errors"),
			}
			for _, attr := range h.ATAAttributes {
				addPrivate(private, attr.Name)
				item.Health.ATAAttributes = append(item.Health.ATAAttributes, storage.ATAAttribute{
					ID: attr.ID, Normalized: attr.Normalized, Threshold: attr.Threshold, Raw: checked(attr.Raw, counter),
					WhenFailed: allowed(attr.WhenFailed, "-", "now", "past", "FAILING_NOW", "In_the_past"),
				})
			}
		}
		out.Drives = append(out.Drives, item)
	}
	return out
}

func projectTrim(audit storage.TrimAudit, private map[string]bool) *storage.TrimAudit {
	t := audit.Timer
	addPrivate(private, audit.Detail, t.Detail, t.FragmentPath, t.ServiceFragmentPath, t.DropInPaths, t.ServiceDropInPaths, t.ServiceExecStart)
	unit := func(value string) string {
		return allowed(value, "loaded", "not-found", "bad-setting", "error", "masked", "enabled", "enabled-runtime", "disabled", "static",
			"masked-runtime", "generated", "indirect", "linked", "linked-runtime", "active", "inactive", "reloading", "activating", "deactivating",
			"failed", "maintenance", "waiting", "elapsed", "dead", "running", "exited", "start", "stop")
	}
	out := &storage.TrimAudit{
		Source:       allowed(audit.Source, storage.DarwinTRIMSource, storage.WindowsTRIMSource),
		Availability: availability(audit.Availability), CapturedAt: audit.CapturedAt.UTC(), Manual: availability(audit.Manual),
		Timer: storage.Timer{
			Availability: availability(t.Availability), Journal: availability(t.Journal), Scheduler: allowed(t.Scheduler, "systemd", "cron", "unknown"),
			LoadState: unit(t.LoadState), UnitFileState: unit(t.UnitFileState), ActiveState: unit(t.ActiveState), SubState: unit(t.SubState),
			NextElapse: checked(t.NextElapse, scheduleStamp), NextMonotonic: checked(t.NextMonotonic, scheduleStamp), LastTrigger: checked(t.LastTrigger, scheduleStamp),
			ServiceLoadState: unit(t.ServiceLoadState), ServiceActiveState: unit(t.ServiceActiveState), ServiceExitCode: t.ServiceExitCode,
			ServiceResult: allowed(t.ServiceResult, "success", "resources", "protocol", "timeout", "exit-code", "signal", "core-dump",
				"watchdog", "start-limit-hit", "oom-kill"), LastServiceStart: checked(t.LastServiceStart, scheduleStamp),
			LastServiceExit: checked(t.LastServiceExit, scheduleStamp), NeedDaemonReload: t.NeedDaemonReload,
		},
	}
	for _, policy := range audit.Policy {
		if scope := allowed(policy.Scope, "nvme", "sata", "ntfs", "refs"); scope != "" {
			out.Policy = append(out.Policy, storage.TrimPolicy{Scope: scope, Enabled: policy.Enabled})
		}
	}
	for _, m := range audit.Mounts {
		addPrivate(private, m.Root, m.Point, m.Source, m.Evidence)
		if fsType(m.FSType) == "" {
			addPrivate(private, m.FSType)
		}
		mount := storage.Mount{
			ID: m.ID, DeviceID: checked(m.DeviceID, blockNumber), FSType: fsType(m.FSType), ReadOnly: m.ReadOnly,
			Discard: availability(m.Discard), DiscardMode: allowed(m.DiscardMode, "discard", "discard=async", "discard=sync", "nodiscard"), LastTrim: m.LastTrim,
		}
		for _, layer := range m.Layers {
			addPrivate(private, layer.Name)
			for _, backing := range layer.BackingDevices {
				addPrivate(private, backing)
			}
			mount.Layers = append(mount.Layers, storage.BlockLayer{DeviceID: checked(layer.DeviceID, blockNumber), MaxDiscardBytes: layer.MaxDiscardBytes})
		}
		out.Mounts = append(out.Mounts, mount)
	}
	return out
}

func projectPower(p power.Snapshot, private map[string]bool) *power.Snapshot {
	profiles := []string{"power-saver", "balanced", "performance"}
	managers := []string{"power-profiles-daemon", "TLP", "TuneD", "auto-cpufreq"}
	platformProfiles := []string{"low-power", "quiet", "cool", "balanced", "balanced-performance", "performance", "custom"}
	restrictions := []string{"lap-detected", "high-operating-temperature"}
	driverNames := []string{"intel_pstate", "intel_cpufreq", "amd-pstate", "amd-pstate-epp", "amd_pstate", "acpi-cpufreq",
		"cppc_cpufreq", "cpufreq-dt", "pcc-cpufreq", "qcom-cpufreq-hw", "scmi-cpufreq", "powernv-cpufreq",
		"platform_profile", "placeholder", "fake", "multiple", "none"}
	governors := []string{"performance", "powersave", "schedutil", "ondemand", "conservative", "userspace"}
	epps := []string{"default", "performance", "balance_performance", "balance_power", "power"}
	q := p.Profiles
	addPrivate(private, q.Target, q.Detail)
	if allowed(p.PlatformProfile, platformProfiles...) == "" {
		addPrivate(private, p.PlatformProfile)
	}
	out := &power.Snapshot{
		OS: allowed(p.OS, "linux", "darwin", "windows"),
		At: p.At.UTC(), CPUFreqStatus: availability(p.CPUFreqStatus), Boost: p.Boost,
		PlatformProfile: allowed(p.PlatformProfile, platformProfiles...), PlatformChoices: choices(p.PlatformChoices, platformProfiles...),
		PowerStatus: availability(p.PowerStatus), PowerSource: allowed(p.PowerSource, "ac", "battery", "unknown"),
		Profiles: power.Profiles{
			Status: availability(q.Status), ManagerStatus: availability(q.ManagerStatus), Backend: allowed(q.Backend, managers...),
			Active: allowed(q.Active, profiles...), Available: choices(q.Available, profiles...),
			Degraded: allowed(q.Degraded, restrictions...), Inhibited: allowed(q.Inhibited, restrictions...), Holds: q.Holds,
			Managers: choices(q.Managers, managers...), Conflicts: choices(q.Conflicts, managers...),
		},
	}
	switch p.BoostSource {
	case "sys/devices/system/cpu/intel_pstate/no_turbo", "sys/devices/system/cpu/cpufreq/boost":
		out.BoostSource = p.BoostSource
	}
	for _, v := range p.Policies {
		for _, pair := range []struct {
			value   string
			permits []string
		}{{v.Driver, driverNames}, {v.Governor, governors}, {v.EPP, epps}} {
			if allowed(pair.value, pair.permits...) == "" {
				addPrivate(private, pair.value)
			}
		}
		item := power.Policy{
			Name: checked(v.Name, policyNumber), Driver: allowed(v.Driver, driverNames...), Governor: allowed(v.Governor, governors...),
			Governors: choices(v.Governors, governors...), EPP: allowed(v.EPP, epps...), EPPChoices: choices(v.EPPChoices, epps...),
			ObservedKHz: v.ObservedKHz, AverageKHz: v.AverageKHz, DriverKHz: v.DriverKHz,
			ConfigMinKHz: v.ConfigMinKHz, ConfigMaxKHz: v.ConfigMaxKHz, HardwareMinKHz: v.HardwareMinKHz, HardwareMaxKHz: v.HardwareMaxKHz,
			FirmwareCapKHz: v.FirmwareCapKHz, Boost: v.Boost,
		}
		for _, cpu := range v.CPUs {
			if index := checked(cpu, cpuIndex); index != "" {
				item.CPUs = append(item.CPUs, index)
			}
		}
		out.Policies = append(out.Policies, item)
	}
	for _, energy := range p.EnergyBias {
		if cpu := checked(energy.CPU, cpuNumber); cpu != "" && energy.Value <= 15 {
			out.EnergyBias = append(out.EnergyBias, power.EnergyBias{CPU: cpu, Value: energy.Value})
		}
	}
	for _, battery := range p.Batteries {
		addPrivate(private, battery.Name)
		out.Batteries = append(out.Batteries, power.Battery{Percent: battery.Percent,
			Status: allowed(strings.ToLower(battery.Status), "charging", "discharging", "full", "not charging", "unknown")})
	}
	for _, profile := range profiles {
		if values := choices(q.Drivers[profile], driverNames...); len(values) != 0 {
			if out.Profiles.Drivers == nil {
				out.Profiles.Drivers = make(map[string][]string)
			}
			out.Profiles.Drivers[profile] = values
		}
	}
	return out
}

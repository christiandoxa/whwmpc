//go:build linux

package power

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const (
	cpuRoot   = "sys/devices/system/cpu"
	freqRoot  = cpuRoot + "/cpufreq"
	powerRoot = "sys/class/power_supply"
	acpiRoot  = "sys/firmware/acpi"
)

func Scan(ctx context.Context, fsys fs.FS, runner sysrun.Runner) Snapshot {
	s := Snapshot{At: time.Now().UTC()}
	s.Policies, s.CPUFreqStatus = cpuPolicies(ctx, fsys)
	s.Boost, s.BoostSource = globalBoost(fsys)
	s.EnergyBias = energyBias(ctx, fsys)
	s.PlatformProfile, _ = readValue(fsys, acpiRoot+"/platform_profile")
	choices, _ := readValue(fsys, acpiRoot+"/platform_profile_choices")
	s.PlatformChoices = strings.Fields(choices)
	s.PowerSource, s.Batteries, s.PowerStatus = supplies(ctx, fsys)
	s.Profiles = InspectProfiles(ctx, fsys, runner)
	return s
}

func cpuPolicies(ctx context.Context, fsys fs.FS) ([]Policy, domain.Availability) {
	entries, err := fs.ReadDir(fsys, freqRoot)
	if err != nil {
		return nil, readStatus(err)
	}
	var policies []Policy
	status := domain.Available
	for _, entry := range entries {
		if ctx.Err() != nil {
			return policies, domain.Partial
		}
		if !indexed(entry.Name(), "policy") {
			continue
		}
		policy, readErr := readPolicy(fsys, entry.Name())
		// Missing optional hardware readouts remain absent in the result.
		if readErr != nil || policy.Governor == "" || policy.ConfigMinKHz == nil ||
			policy.ConfigMaxKHz == nil || (policy.ObservedKHz == nil && policy.DriverKHz == nil && policy.AverageKHz == nil) {
			status = domain.Partial
		}
		policies = append(policies, policy)
	}
	if len(policies) == 0 {
		return nil, domain.Unsupported
	}
	sort.Slice(policies, func(i, j int) bool { return indexedLess(policies[i].Name, policies[j].Name) })
	return policies, status
}

func readPolicy(fsys fs.FS, name string) (Policy, error) {
	root := path.Join(freqRoot, name)
	get := func(file string) string {
		value, _ := readValue(fsys, path.Join(root, file))
		return value
	}
	freq := func(file string) *uint64 {
		return unsigned(get(file), 1, ^uint64(0))
	}
	driver, err := readValue(fsys, path.Join(root, "scaling_driver"))
	if driver == "" && err == nil {
		err = errors.New("empty CPUFreq driver")
	}
	return Policy{
		Name:           name,
		CPUs:           strings.Fields(get("affected_cpus")),
		Driver:         driver,
		Governor:       get("scaling_governor"),
		Governors:      strings.Fields(get("scaling_available_governors")),
		ObservedKHz:    freq("cpuinfo_cur_freq"),
		AverageKHz:     freq("cpuinfo_avg_freq"),
		DriverKHz:      freq("scaling_cur_freq"),
		ConfigMinKHz:   freq("scaling_min_freq"),
		ConfigMaxKHz:   freq("scaling_max_freq"),
		HardwareMinKHz: freq("cpuinfo_min_freq"),
		HardwareMaxKHz: freq("cpuinfo_max_freq"),
		FirmwareCapKHz: freq("bios_limit"),
		Boost:          binary(get("boost")),
		EPP:            get("energy_performance_preference"),
		EPPChoices:     strings.Fields(get("energy_performance_available_preferences")),
	}, err
}

func globalBoost(fsys fs.FS) (*bool, string) {
	// Intel's active P-state driver exposes the inverse of the generic boost flag.
	if raw, _ := readValue(fsys, cpuRoot+"/intel_pstate/no_turbo"); binary(raw) != nil {
		return new(raw == "0"), cpuRoot + "/intel_pstate/no_turbo"
	}
	if raw, _ := readValue(fsys, freqRoot+"/boost"); binary(raw) != nil {
		return binary(raw), freqRoot + "/boost"
	}
	return nil, ""
}

func energyBias(ctx context.Context, fsys fs.FS) []EnergyBias {
	entries, _ := fs.ReadDir(fsys, cpuRoot)
	var bias []EnergyBias
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		if !indexed(entry.Name(), "cpu") {
			continue
		}
		raw, err := readValue(fsys, path.Join(cpuRoot, entry.Name(), "power/energy_perf_bias"))
		if value := unsigned(raw, 0, 15); err == nil && value != nil {
			bias = append(bias, EnergyBias{CPU: entry.Name(), Value: *value})
		}
	}
	sort.Slice(bias, func(i, j int) bool { return indexedLess(bias[i].CPU, bias[j].CPU) })
	return bias
}

func supplies(ctx context.Context, fsys fs.FS) (string, []Battery, domain.Availability) {
	entries, err := fs.ReadDir(fsys, powerRoot)
	if err != nil {
		return "unknown", nil, readStatus(err)
	}
	var batteries []Battery
	externalOnline, charging, discharging, unreadable := false, false, false, false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return "unknown", batteries, domain.Partial
		}
		root := path.Join(powerRoot, entry.Name())
		get := func(field string) string {
			value, _ := readValue(fsys, path.Join(root, field))
			return value
		}
		if get("scope") == "Device" {
			continue
		}
		kind := get("type")
		switch {
		case kind == "Battery":
			if get("present") == "0" {
				continue
			}
			state := get("status")
			capacity := unsigned(get("capacity"), 0, 100)
			charging = charging || state == "Charging"
			discharging = discharging || state == "Discharging"
			batteries = append(batteries, Battery{Name: safeText(entry.Name()), Status: state, Percent: capacity})
			unreadable = unreadable || state == "" || capacity == nil
		case kind == "Mains" || kind == "Wireless" || strings.HasPrefix(kind, "USB"):
			online := get("online")
			externalOnline = externalOnline || online == "1"
			unreadable = unreadable || binary(online) == nil
		case kind == "":
			unreadable = true
		}
	}
	status := domain.Available
	if unreadable {
		status = domain.Partial
	}
	if externalOnline || charging {
		return "ac", batteries, status
	}
	if discharging {
		return "battery", batteries, status
	}
	if len(batteries) > 0 || unreadable {
		return "unknown", batteries, domain.Partial
	}
	return "unknown", nil, domain.Unknown
}

func indexed(s, prefix string) bool {
	value, ok := strings.CutPrefix(s, prefix)
	return ok && value != "" && !strings.ContainsFunc(value, func(r rune) bool { return r < '0' || r > '9' })
}

func indexedLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func unsigned(s string, min, max uint64) *uint64 {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n < min || n > max {
		return nil
	}
	return &n
}

func binary(s string) *bool {
	switch s {
	case "1":
		return new(true)
	case "0":
		return new(false)
	default:
		return nil
	}
}

func readStatus(err error) domain.Availability {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return domain.Unsupported
	case errors.Is(err, fs.ErrPermission):
		return domain.PermissionRequired
	default:
		return domain.Error
	}
}

func readValue(fsys fs.FS, name string) (string, error) {
	raw, err := fs.ReadFile(fsys, name)
	return safeText(string(raw)), err
}

func safeText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\t' || r == '\r' || r == '\n' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s))
}

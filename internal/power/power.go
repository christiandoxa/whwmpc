package power

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Snapshot struct {
	At              time.Time           `json:"at"`
	CPUFreqStatus   domain.Availability `json:"cpufreq_status"`
	Policies        []Policy            `json:"policies,omitempty"`
	Boost           *bool               `json:"boost,omitempty"`
	BoostSource     string              `json:"boost_source,omitempty"`
	EnergyBias      []EnergyBias        `json:"energy_bias,omitempty"`
	PlatformProfile string              `json:"platform_profile,omitempty"`
	PlatformChoices []string            `json:"platform_choices,omitempty"`
	PowerStatus     domain.Availability `json:"power_status"`
	PowerSource     string              `json:"power_source"`
	Batteries       []Battery           `json:"batteries,omitempty"`
	Profiles        Profiles            `json:"profiles"`
}

type Policy struct {
	Name           string   `json:"name"`
	CPUs           []string `json:"cpus,omitempty"`
	Driver         string   `json:"driver,omitempty"`
	Governor       string   `json:"governor,omitempty"`
	Governors      []string `json:"governors,omitempty"`
	ObservedKHz    *uint64  `json:"cpuinfo_cur_freq_khz,omitempty"`
	AverageKHz     *uint64  `json:"cpuinfo_avg_freq_khz,omitempty"`
	DriverKHz      *uint64  `json:"scaling_cur_freq_khz,omitempty"`
	ConfigMinKHz   *uint64  `json:"scaling_min_freq_khz,omitempty"`
	ConfigMaxKHz   *uint64  `json:"scaling_max_freq_khz,omitempty"`
	HardwareMinKHz *uint64  `json:"cpuinfo_min_freq_khz,omitempty"`
	HardwareMaxKHz *uint64  `json:"cpuinfo_max_freq_khz,omitempty"`
	FirmwareCapKHz *uint64  `json:"bios_limit_khz,omitempty"`
	Boost          *bool    `json:"boost,omitempty"`
	EPP            string   `json:"epp,omitempty"`
	EPPChoices     []string `json:"epp_choices,omitempty"`
}

type EnergyBias struct {
	CPU   string `json:"cpu"`
	Value uint64 `json:"value"`
}

type Battery struct {
	Name    string  `json:"name"`
	Percent *uint64 `json:"percent,omitempty"`
	Status  string  `json:"status,omitempty"`
}

type Profiles struct {
	Status        domain.Availability `json:"status"`
	ManagerStatus domain.Availability `json:"manager_status"`
	Backend       string              `json:"backend,omitempty"`
	Target        string              `json:"-"`
	API           string              `json:"-"`
	Active        string              `json:"active,omitempty"`
	Available     []string            `json:"available,omitempty"`
	Drivers       map[string][]string `json:"drivers,omitempty"`
	Degraded      string              `json:"degraded,omitempty"`
	Inhibited     string              `json:"inhibited,omitempty"`
	Holds         *int                `json:"active_holds,omitempty"`
	Managers      []string            `json:"managers,omitempty"`
	Conflicts     []string            `json:"conflicts,omitempty"`
	Detail        string              `json:"detail,omitempty"`
}

const (
	profileDaemon = "power-profiles-daemon"
	currentAPI    = "org.freedesktop.UPower.PowerProfiles"
	legacyAPI     = "net.hadess.PowerProfiles"
	busAddress    = "--address=unix:path=/run/dbus/system_bus_socket"
	propertiesAPI = "org.freedesktop.DBus.Properties"
)

func ValidProfile(name string) bool {
	switch name {
	case "balanced", "power-saver", "performance":
		return true
	default:
		return false
	}
}

func (p Profiles) CanChange() bool {
	return p.Status == domain.Available && p.ManagerStatus == domain.Available &&
		p.Backend == profileDaemon && p.Holds != nil && *p.Holds == 0 && len(p.Conflicts) == 0 &&
		profileObject(p.API) != "" && profileOwner(p.Target) != "" && ValidProfile(p.Active) &&
		slices.Contains(p.Available, p.Active)
}

// ProfileCommand builds arguments for busctl from a freshly observed daemon identity.
func ProfileCommand(p Profiles, desired string) ([]string, error) {
	if !p.CanChange() || !ValidProfile(desired) || !slices.Contains(p.Available, desired) {
		return nil, errors.New("power profile change unavailable for this daemon")
	}
	if desired == "performance" && p.Inhibited != "" {
		return nil, errors.New("the daemon inhibits the performance profile")
	}
	return []string{busAddress, "--auto-start=no", "--allow-interactive-authorization=no",
		"call", profileOwner(p.Target), profileObject(p.API), propertiesAPI,
		"Set", "ssv", p.API, "ActiveProfile", "s", desired}, nil
}

func profileOwner(target string) string {
	busID, owner, ok := strings.Cut(target, "/")
	if !ok || len(busID) != 32 || !strings.HasPrefix(owner, ":") || len(owner) > 64 {
		return ""
	}
	if strings.ContainsFunc(busID, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef", r) }) {
		return ""
	}
	fields := strings.Split(owner[1:], ".")
	if len(fields) != 2 {
		return ""
	}
	for _, field := range fields {
		if field == "" || strings.ContainsFunc(field, func(r rune) bool { return r < '0' || r > '9' }) {
			return ""
		}
	}
	return owner
}

func profileObject(api string) string {
	switch api {
	case currentAPI:
		return "/org/freedesktop/UPower/PowerProfiles"
	case legacyAPI:
		return "/net/hadess/PowerProfiles"
	default:
		return ""
	}
}

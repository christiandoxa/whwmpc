package power

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Snapshot struct {
	OS              string              `json:"-"`
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

func (p Snapshot) LinuxInterfaces() bool {
	return p.OS == "linux" || p.OS == ""
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
	busPath       = "/run/dbus/system_bus_socket"
	busAddress    = "--address=unix:path=" + busPath
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
	guid, _ := profileEndpoint(p.Target)
	return p.Status == domain.Available && p.ManagerStatus == domain.Available &&
		p.Backend == profileDaemon && p.Holds != nil && *p.Holds == 0 && len(p.Conflicts) == 0 &&
		profileObject(p.API) != "" && guid != "" && ValidProfile(p.Active) &&
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
	guid, owner := profileEndpoint(p.Target)
	return []string{busAddress + ",guid=" + guid, "--auto-start=no", "--allow-interactive-authorization=no",
		"call", owner, profileObject(p.API), propertiesAPI,
		"Set", "ssv", p.API, "ActiveProfile", "s", desired}, nil
}

func profileEndpoint(target string) (guid, owner string) {
	busID, endpoint, _ := strings.Cut(target, "/")
	guid, owner, _ = strings.Cut(endpoint, "/")
	if !validBusID(busID) || !validBusID(guid) || !validOwner(owner) {
		return "", ""
	}
	return guid, owner
}

func validBusID(id string) bool {
	return len(id) == 32 && id != strings.Repeat("0", 32) && strings.Trim(id, "0123456789abcdef") == ""
}

func validOwner(owner string) bool {
	if !strings.HasPrefix(owner, ":") || len(owner) > 64 {
		return false
	}
	fields := strings.Split(owner[1:], ".")
	if len(fields) != 2 {
		return false
	}
	for _, field := range fields {
		if field == "" || strings.ContainsFunc(field, func(r rune) bool { return r < '0' || r > '9' }) {
			return false
		}
	}
	return true
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

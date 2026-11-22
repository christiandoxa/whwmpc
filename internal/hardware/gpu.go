// Package hardware discovers graphics devices and documented tuning controls.
package hardware

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

const (
	PowerLimit           = "gpu-power-limit"
	CoreMax              = "gpu-core-max"
	VoltageOffset        = "gpu-voltage-offset"
	RestorePowerLimit    = "restore-gpu-power-limit"
	RestoreCoreMax       = "restore-gpu-core-max"
	RestoreVoltageOffset = "restore-gpu-voltage-offset"
)

type Snapshot struct {
	Availability domain.Availability `json:"availability"`
	At           time.Time           `json:"at"`
	Devices      []Device            `json:"devices,omitempty"`
	Detail       string              `json:"detail,omitempty"`
}

type Device struct {
	ID                 string              `json:"-"`
	PCI                string              `json:"pci"`
	Vendor             string              `json:"vendor"`
	Driver             string              `json:"driver,omitempty"`
	Version            string              `json:"driver_version,omitempty"`
	Model              string              `json:"model"`
	Backend            string              `json:"backend"`
	Availability       domain.Availability `json:"availability"`
	TuningAvailability domain.Availability `json:"tuning_availability"`
	TuningDetail       string              `json:"tuning_detail,omitempty"`
	Metrics            Metrics             `json:"metrics"`
	Controls           []Control           `json:"controls,omitempty"`
}

type Metrics struct {
	UtilizationPct *int64 `json:"utilization_percent,omitempty"`
	TemperatureC   *int64 `json:"temperature_c,omitempty"`
	VRAMUsedBytes  *int64 `json:"vram_used_bytes,omitempty"`
	VRAMTotalBytes *int64 `json:"vram_total_bytes,omitempty"`
	PowerMilliW    *int64 `json:"power_milliwatts,omitempty"`
	PowerScope     string `json:"power_scope,omitempty"`
	CoreMHz        *int64 `json:"core_mhz,omitempty"`
	MemoryMHz      *int64 `json:"memory_mhz,omitempty"`
	EnforcedMilliW *int64 `json:"enforced_power_limit_milliwatts,omitempty"`
}

type Control struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Unit    string `json:"unit"`
	Current int64  `json:"current"`
	Min     int64  `json:"min"`
	Max     int64  `json:"max"`
	Default *int64 `json:"default,omitempty"`
}

var pciPattern = regexp.MustCompile(`^(?:0000)?([0-9a-f]{4}:[0-9a-f]{2}:[0-1][0-9a-f]\.[0-7])$`)
var gpuUUIDPattern = regexp.MustCompile(`^GPU-[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func canonicalPCI(value string) string {
	match := pciPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(value)))
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func ParseTarget(kind, target string) (id string, value int64, restoreMode string, ok bool) {
	id, text, found := strings.Cut(target, "=")
	if !found || len(target) > 100 {
		return "", 0, "", false
	}
	switch kind {
	case PowerLimit, RestorePowerLimit:
		if !gpuUUIDPattern.MatchString(id) {
			return "", 0, "", false
		}
	case CoreMax, VoltageOffset, RestoreCoreMax, RestoreVoltageOffset:
		if id == "" || canonicalPCI(id) != id {
			return "", 0, "", false
		}
		if kind == RestoreCoreMax || kind == RestoreVoltageOffset {
			text, restoreMode, _ = strings.Cut(text, ",")
			if restoreMode != "auto" && restoreMode != "manual" {
				return "", 0, "", false
			}
		}
	default:
		return "", 0, "", false
	}
	value, err := strconv.ParseInt(text, 10, 32)
	if err != nil || strconv.FormatInt(value, 10) != text {
		return "", 0, "", false
	}
	return id, value, restoreMode, true
}

func boundedInt(text string, min, max int64) *int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil || value < min || value > max {
		return nil
	}
	return &value
}

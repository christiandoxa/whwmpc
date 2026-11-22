package domain

import (
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

type PercentScope string

const PrimaryProcessorGroup PercentScope = "primary_processor_group"

type Percent struct {
	Availability Availability `json:"availability"`
	Scope        PercentScope `json:"scope,omitempty"`
	Samples      []float64    `json:"samples,omitempty"`
	StartedAt    time.Time    `json:"started_at,omitzero"`
	CapturedAt   time.Time    `json:"captured_at,omitzero"`
}

func (p Percent) Latest() (float64, bool) {
	if len(p.Samples) == 0 || (p.Availability != Available && p.Availability != Partial) {
		return 0, false
	}
	return p.Samples[len(p.Samples)-1], true
}

type Memory struct {
	Availability Availability `json:"availability"`
	TotalBytes   uint64       `json:"total_bytes,omitempty"`
	FreeBytes    uint64       `json:"available_bytes,omitempty"`
	SwapStatus   Availability `json:"swap_availability"`
	SwapBytes    uint64       `json:"swap_total_bytes,omitempty"`
	SwapUsed     uint64       `json:"swap_used_bytes,omitempty"`
	Use          Percent      `json:"use"`
}

type Disk struct {
	Mount        string       `json:"-"`
	FSType       string       `json:"filesystem"`
	Root         bool         `json:"root_filesystem"`
	Availability Availability `json:"availability"`
	TotalBytes   uint64       `json:"total_bytes,omitempty"`
	FreeBytes    uint64       `json:"available_bytes,omitempty"`
	UsedPercent  float64      `json:"used_percent,omitempty"`
}

type Temperature struct {
	Availability Availability `json:"availability"`
	Celsius      *float64     `json:"celsius,omitempty"`
	Source       string       `json:"source,omitempty"`
	CapturedAt   time.Time    `json:"captured_at,omitzero"`
}

type Process struct {
	Name string  `json:"-"`
	CPU  float64 `json:"cpu_percent_of_host"`
	RSS  uint64  `json:"rss_bytes"`
}

type Overview struct {
	Availability  Availability `json:"availability"`
	CapturedAt    time.Time    `json:"captured_at"`
	OS            string       `json:"os"`
	Distro        string       `json:"distribution,omitempty"`
	DistroID      string       `json:"distribution_id,omitempty"`
	Kernel        string       `json:"kernel,omitempty"`
	Arch          string       `json:"architecture"`
	Environment   string       `json:"environment,omitempty"`
	Systemd       bool         `json:"systemd_detected"`
	UptimeSeconds *float64     `json:"uptime_seconds,omitempty"`
	CPUModel      string       `json:"cpu_model,omitempty"`
	LogicalCPUs   int          `json:"logical_cpus"`
	CPU           Percent      `json:"cpu"`
	Memory        Memory       `json:"memory"`
	DiskStatus    Availability `json:"disk_availability"`
	Disks         []Disk       `json:"filesystems,omitempty"`
	Temperature   Temperature  `json:"temperature"`
	PowerStatus   Availability `json:"power_availability"`
	OnAC          *bool        `json:"on_ac,omitempty"`
	Battery       *float64     `json:"battery_percent,omitempty"`
	ProcessStatus Availability `json:"process_availability"`
	Processes     []Process    `json:"processes,omitempty"`
}

// SafeText returns a single line safe to interpolate into terminal content.
func SafeText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case r == '\n', r == '\r', r == '\t':
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == '\u2028', r == '\u2029':
			return -1
		default:
			return r
		}
	}, ansi.Strip(s)))
}

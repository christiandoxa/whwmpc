// Package report projects local diagnostics into a redacted, versioned export.
package report

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

const SchemaVersion = 1

type Input struct {
	At       time.Time
	Demo     bool
	Overview *domain.Overview
	SMART    *storage.SMARTAudit
	Trim     *storage.TrimAudit
	Power    *power.Snapshot
	Findings []diagnostics.Finding
	Actions  []actions.Result
}

type Report struct {
	SchemaVersion int                   `json:"schema_version"`
	GeneratedAt   time.Time             `json:"generated_at"`
	Source        string                `json:"source"`
	Overview      *domain.Overview      `json:"overview,omitempty"`
	SMART         *storage.SMARTAudit   `json:"smart,omitempty"`
	Trim          *storage.TrimAudit    `json:"trim,omitempty"`
	Power         *power.Snapshot       `json:"power,omitempty"`
	Findings      []diagnostics.Finding `json:"findings"`
	Actions       []Action              `json:"actions,omitempty"`
}

type Action struct {
	Kind                  string         `json:"kind"`
	Status                actions.Status `json:"status"`
	FinishedAt            time.Time      `json:"finished_at"`
	Started               bool           `json:"started"`
	CancellationRequested bool           `json:"cancellation_requested"`
	Changed               *bool          `json:"changed,omitempty"`
	PotentialDiscardBytes *uint64        `json:"potential_discard_bytes,omitempty"`
	RemovedBytes          *uint64        `json:"removed_bytes,omitempty"`
	FreeSpaceDelta        *int64         `json:"free_space_delta,omitempty"`
}

func Build(input Input) Report {
	report := Report{SchemaVersion: SchemaVersion, GeneratedAt: input.At.UTC(), Source: "live"}
	if input.Demo {
		report.Source = "demo"
	}
	private := make(map[string]bool)
	if input.Overview != nil {
		report.Overview = projectOverview(*input.Overview, private)
	}
	if input.SMART != nil {
		report.SMART = projectSMART(*input.SMART, private)
	}
	if input.Trim != nil {
		report.Trim = projectTrim(*input.Trim, private)
	}
	if input.Power != nil {
		report.Power = projectPower(*input.Power, private)
	}
	for _, result := range input.Actions {
		addPrivate(private, result.Plan.ID, result.Plan.Identity, result.AfterIdentity, result.Receipt)
		if !power.ValidProfile(result.Plan.Target) && allowed(result.Plan.Target, "fstrim.timer", "apt-cache") == "" {
			addPrivate(private, result.Plan.Target)
		}
	}
	redact := proseRedactor(private)
	report.Findings = make([]diagnostics.Finding, 0, len(input.Findings))
	for _, value := range input.Findings {
		report.Findings = append(report.Findings, projectFinding(value, redact))
	}
	if !input.Demo {
		for _, result := range input.Actions {
			report.Actions = append(report.Actions, Action{
				Kind: publicAction(result.Plan.Kind), Status: publicActionStatus(result.Status),
				FinishedAt: result.FinishedAt.UTC(), Started: result.Started, CancellationRequested: result.CancellationRequested,
				Changed: result.Changed, PotentialDiscardBytes: result.PotentialDiscardBytes,
				RemovedBytes: result.RemovedBytes, FreeSpaceDelta: result.FreeSpaceDelta,
			})
		}
	}
	return report
}

func Encode(report Report, format string) ([]byte, error) {
	switch format {
	case "json":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	case "markdown":
		return []byte(markdown(report)), nil
	default:
		return nil, errors.New("format report harus json atau markdown")
	}
}

var absolutePath = regexp.MustCompile(`(?i)(?:^|[\s=[:punct:]])(?:[a-z]:[\\/]|/)[^\s<>"',;]+\b`)
var identified = regexp.MustCompile(`(?i)\b(?:hostname|username|serial(?:_number)?)\s*[:=]\s*[^\s,;]+`)
var findingID = regexp.MustCompile(`^(?:overview|power|trim|smart|recommend|cleanup|benchmark|gpu|firmware|filesystem)\.[a-z-]{2,30}(?:\.[0-9]{1,5})?$`)
var ordinal = regexp.MustCompile(`^[0-9]{1,20}$`)

func proseRedactor(private map[string]bool) func(string) string {
	terms := make([]string, 0, len(private))
	for term := range private {
		terms = append(terms, term)
	}
	slices.SortFunc(terms, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	pairs := make([]string, 0, 2*len(terms))
	for _, term := range terms {
		pairs = append(pairs, term, "[redacted]")
	}
	replacer := strings.NewReplacer(pairs...)
	return func(value string) string {
		value = domain.SafeText(value)
		value = replacer.Replace(value)
		value = absolutePath.ReplaceAllString(value, " [redacted]")
		return strings.TrimSpace(identified.ReplaceAllString(value, "[redacted]"))
	}
}

func addPrivate(private map[string]bool, values ...string) {
	for _, value := range values {
		value = domain.SafeText(value)
		if len(value) >= 3 && len(value) <= 4096 {
			private[value] = true
		}
	}
}

func projectFinding(f diagnostics.Finding, redact func(string) string) diagnostics.Finding {
	out := diagnostics.Finding{
		ID: f.ID, Category: allowed(f.Category, "cpu", "memory", "disk", "temperature", "process", "power", "ssd", "trim", "recommendation", "cleanup", "benchmark", "gpu", "firmware", "filesystem"),
		Availability: availability(f.Availability), Severity: f.Severity, At: f.At.UTC(), Source: publicSource(f.Source),
		Observation: redact(f.Observation), Meaning: redact(f.Meaning), Limits: redact(f.Limits), Recommendation: redact(f.Recommendation),
	}
	if trimmed := redact(out.ID); trimmed != out.ID || !findingID.MatchString(out.ID) {
		out.ID = "diagnostic"
	}
	if out.Severity != diagnostics.Critical && out.Severity != diagnostics.Warning {
		out.Severity = diagnostics.Info
	}
	for _, action := range f.Actions {
		kind := publicAction(action.Kind)
		if kind == "unknown" {
			continue
		}
		target := ""
		switch kind {
		case actions.TrimMount:
			if ordinal.MatchString(action.Target) {
				target = action.Target
			}
		case actions.PowerProfile:
			if power.ValidProfile(action.Target) {
				target = action.Target
			}
		case actions.EnableTrimTimer:
			target = allowed(action.Target, "fstrim.timer")
		case "navigate":
			if slices.Contains(assistant.Menu, assistant.Intent(action.Target)) {
				target = action.Target
			}
		case "workload":
			if slices.Contains(assistant.Workloads, assistant.Workload(action.Target)) {
				target = action.Target
			}
		}
		out.Actions = append(out.Actions, diagnostics.Action{Kind: kind, Target: target, Label: redact(action.Label)})
	}
	return out
}

func publicSource(value string) string {
	switch value {
	case "Filesystem checker", "Filesystem inventory", "fwupd inventory", "fwupd verification", "whwmpc benchmark", "GPU vendor telemetry", "/proc/stat", "/proc/meminfo (MemAvailable, SwapTotal, SwapFree)", "statfs (Bavail) + /proc/self/mountinfo", "Linux hwmon / thermal zones",
		"/proc/[pid]/stat delta + /proc/stat", "Linux power_supply class", "Linux CPUFreq policy sysfs",
		"Linux power_supply + platform_profile + energy_perf_bias", "D-Bus PowerProfiles API + systemd and /proc manager checks",
		"Workload preference + overview temperature + power_supply + PowerProfiles API",
		"/proc/self/mountinfo + Linux block queue discard_max_bytes + fstrim.service journal",
		"systemctl show fstrim.timer/fstrim.service + system journal", "cron capability check", "systemd/cron capability checks",
		"smartctl JSON (SMART status, exit bitmask, NVMe Health log)",
		"/sys/class/block + smartctl JSON (SMART status, exit bitmask, NVMe Health log)",
		"NVMe SMART/Health Log 0x02 (Percentage Used)",
		storage.DiskUtilitySource, storage.WindowsStorageSource, storage.DarwinTRIMSource, storage.WindowsTRIMSource,
		"macOS top interval", "macOS hw.memsize and vm_stat", "macOS statfs (startup volume)", "macOS ps (%CPU and RSS)", "macOS pmset -g batt",
		"Win32 GetSystemTimes", "Win32 GlobalMemoryStatusEx", "Win32 GetDiskFreeSpaceEx", "Win32 process timing and working set", "Win32 GetSystemPowerStatus":
		return value
	default:
		return "whwmpc"
	}
}

func publicAction(kind string) string {
	switch kind {
	case actions.PowerProfile, actions.TrimMount, actions.EnableTrimTimer, actions.CleanAPT, actions.CleanPacman, actions.CleanDNF, actions.RestorePower, actions.RestoreTrimTimer,
		actions.FirmwareUpdate, actions.FirmwareRefresh, actions.FilesystemRepair,
		hardware.PowerLimit, hardware.CoreMax, hardware.VoltageOffset, hardware.RestorePowerLimit, hardware.RestoreCoreMax, hardware.RestoreVoltageOffset,
		"rescan", "navigate", "workload":
		return kind
	default:
		return "unknown"
	}
}

func publicActionStatus(status actions.Status) actions.Status {
	switch status {
	case actions.NotStarted, actions.Verified, actions.Failed, actions.Partial, actions.Unknown:
		return status
	default:
		return actions.Unknown
	}
}

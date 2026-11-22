// Package storage describes the evidence available for SMART and discard on a host.
package storage

import (
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type Collector struct {
	Runner sysrun.Runner
	Now    func() time.Time
	Root   string // Alternate filesystem root for fixtures.
}

type SMARTAudit struct {
	Availability domain.Availability `json:"availability"`
	CapturedAt   time.Time           `json:"captured_at"`
	Drives       []Drive             `json:"drives,omitempty"`
	Detail       string              `json:"detail,omitempty"`
}

type Drive struct {
	Availability domain.Availability `json:"availability"`
	CapturedAt   time.Time           `json:"captured_at"`
	Name         string              `json:"name"`
	DeviceID     string              `json:"device_id"`
	Model        string              `json:"model,omitempty"`
	Transport    string              `json:"transport,omitempty"`
	Protocol     string              `json:"protocol,omitempty"`
	Rotational   *bool               `json:"rotational,omitempty"`
	Health       *SMART              `json:"smart,omitempty"`
	MountIDs     []uint64            `json:"mount_ids,omitempty"`
	Detail       string              `json:"detail,omitempty"`
}

type SMART struct {
	Passed           *bool          `json:"passed,omitempty"`
	CriticalWarning  *uint64        `json:"critical_warning,omitempty"`
	TemperatureC     *int64         `json:"temperature_c,omitempty"`
	Spare            *uint64        `json:"available_spare,omitempty"`
	SpareThreshold   *uint64        `json:"spare_threshold,omitempty"`
	PercentageUsed   *uint64        `json:"percentage_used,omitempty"`
	MediaErrors      string         `json:"media_errors,omitempty"`
	UnsafeShutdowns  string         `json:"unsafe_shutdowns,omitempty"`
	PowerOnHours     string         `json:"power_on_hours,omitempty"`
	DataUnitsRead    string         `json:"data_units_read,omitempty"`
	DataUnitsWritten string         `json:"data_units_written,omitempty"`
	ATAAttributes    []ATAAttribute `json:"ata_attributes,omitempty"`
	ExitCode         int            `json:"exit_code"`
	ExitFlags        []string       `json:"exit_flags,omitempty"`
}

type ATAAttribute struct {
	ID         uint8  `json:"id"`
	Name       string `json:"name"`
	Normalized *uint8 `json:"normalized,omitempty"`
	Threshold  *uint8 `json:"threshold,omitempty"`
	Raw        string `json:"raw,omitempty"`
	WhenFailed string `json:"when_failed,omitempty"`
}

const (
	SMARTInvalidArguments = 1 << iota
	SMARTDeviceOpenFailed
	SMARTCommandFailed
	SMARTDiskFailing
	SMARTCurrentThreshold
	SMARTHistoricalThreshold
	SMARTErrorLog
	SMARTSelfTestLog
)

type TrimAudit struct {
	Availability domain.Availability `json:"availability"`
	CapturedAt   time.Time           `json:"captured_at"`
	Mounts       []Mount             `json:"mounts,omitempty"`
	Timer        Timer               `json:"timer"`
	Manual       domain.Availability `json:"fstrim"`
	Detail       string              `json:"detail,omitempty"`
}

type Mount struct {
	ID           uint64              `json:"id"`
	DeviceID     string              `json:"device_id"`
	Root         string              `json:"-"`
	Point        string              `json:"-"`
	Source       string              `json:"-"`
	FSType       string              `json:"filesystem"`
	ReadOnly     bool                `json:"read_only"`
	Discard      domain.Availability `json:"discard"`
	MountOptions []string            `json:"-"`
	DiscardMode  string              `json:"discard_mount_option,omitempty"`
	Layers       []BlockLayer        `json:"layers,omitempty"`
	Evidence     string              `json:"evidence"`
	LastTrim     *TrimRecord         `json:"last_trim,omitempty"`

	parentID uint64
}

type BlockLayer struct {
	Name            string   `json:"name"`
	DeviceID        string   `json:"device_id"`
	DiskSequence    string   `json:"-"`
	MaxDiscardBytes *uint64  `json:"max_discard_bytes,omitempty"`
	BackingDevices  []string `json:"backing_devices,omitempty"`
}

type Timer struct {
	Availability         domain.Availability `json:"availability"`
	Journal              domain.Availability `json:"journal_evidence"`
	Scheduler            string              `json:"scheduler,omitempty"`
	Detail               string              `json:"detail,omitempty"`
	LoadState            string              `json:"load_state,omitempty"`
	UnitFileState        string              `json:"unit_file_state,omitempty"`
	ActiveState          string              `json:"active_state,omitempty"`
	ActiveEnterMonotonic string              `json:"-"`
	SubState             string              `json:"sub_state,omitempty"`
	Unit                 string              `json:"-"`
	NextElapse           string              `json:"next_elapse,omitempty"`
	NextMonotonic        string              `json:"next_monotonic,omitempty"`
	LastTrigger          string              `json:"last_trigger,omitempty"`
	FragmentPath         string              `json:"-"`
	DropInPaths          string              `json:"-"`
	ServiceLoadState     string              `json:"service_load_state,omitempty"`
	ServiceActiveState   string              `json:"service_active_state,omitempty"`
	ServiceResult        string              `json:"service_result,omitempty"`
	ServiceExitCode      *int                `json:"service_exit_code,omitempty"`
	LastServiceStart     string              `json:"last_service_start,omitempty"`
	LastServiceExit      string              `json:"last_service_exit,omitempty"`
	ServiceFragmentPath  string              `json:"-"`
	ServiceDropInPaths   string              `json:"-"`
	ServiceExecStart     string              `json:"-"`
	NeedDaemonReload     bool                `json:"need_daemon_reload,omitempty"`
}

type TrimRecord struct {
	RecordedAt            time.Time `json:"recorded_at"`
	PotentialDiscardBytes uint64    `json:"potential_discard_bytes"`
}

func (c Collector) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

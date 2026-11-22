//go:build linux

package hardware

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type Settings struct {
	ID, PCI, Hardware, Firmware string
	Control                     Control
	Mode, Table, SysfsDir       string
}

type Change struct {
	Before  Settings
	Desired int64
	Mode    string
}

func Prepare(ctx context.Context, runner sysrun.Runner, kind, target string) (Change, error) {
	id, value, mode, ok := ParseTarget(kind, target)
	if !ok {
		return Change{}, errors.New("target GPU harus menggunakan identitas dan angka yang valid")
	}
	before, err := ReadSettings(ctx, runner, kind, id)
	if err != nil {
		return Change{}, err
	}
	control := before.Control
	if value < control.Min || value > control.Max {
		return Change{}, fmt.Errorf("nilai %d di luar rentang %d–%d %s", value, control.Min, control.Max, control.Unit)
	}
	if control.readbackStep > 0 && value%control.readbackStep != 0 {
		return Change{}, fmt.Errorf("nilai %d %s memerlukan ketelitian melebihi pembacaan ulang nvidia-smi (%d %s)",
			value, control.Unit, control.readbackStep, control.Unit)
	}
	if mode == "" && before.Mode != "" {
		mode = "manual"
	}
	if value == control.Current && mode == before.Mode {
		return Change{}, errors.New("nilai dan mode yang diminta sudah aktif")
	}
	return Change{Before: before, Desired: value, Mode: mode}, nil
}

func ReadSettings(ctx context.Context, runner sysrun.Runner, kind, id string) (Settings, error) {
	if err := ctx.Err(); err != nil {
		return Settings{}, err
	}
	if runner == nil {
		runner = sysrun.Native{}
	}
	switch kind {
	case PowerLimit, RestorePowerLimit:
		gpus, _, err := readNVIDIA(ctx, runner, id)
		if err != nil {
			return Settings{}, err
		}
		gpu := gpus[0]
		if len(gpu.Controls) != 1 || gpu.Version == "" {
			return Settings{}, errors.New("batas daya dan versi driver NVIDIA belum terverifikasi")
		}
		return Settings{ID: gpu.ID, PCI: gpu.PCI, Hardware: gpu.Model, Firmware: gpu.Version, Control: gpu.Controls[0]}, nil
	case CoreMax, RestoreCoreMax, VoltageOffset, RestoreVoltageOffset:
		return amdSettings(systemPCI, kind, id)
	default:
		return Settings{}, errors.New("kontrol GPU tidak dikenal")
	}
}

func amdSettings(pciRoot, kind, id string) (Settings, error) {
	amd, err := readAMD(pciRoot, id)
	if err != nil {
		return Settings{}, err
	}
	controlKind := strings.TrimPrefix(kind, "restore-")
	controls := amd.controls()
	i := slices.IndexFunc(controls, func(c Control) bool { return c.Kind == controlKind })
	if i < 0 {
		return Settings{}, fmt.Errorf("nilai/rentang OverDrive %s tidak tersedia: %w", controlKind, os.ErrNotExist)
	}
	if kind == CoreMax && !amd.coreMaxRangeVerified() {
		return Settings{}, errors.New(coreRangeUnavailable)
	}
	return Settings{ID: id, PCI: id, Hardware: amd.Device + "/" + amd.Subsystem, Control: controls[i], Mode: amd.Mode, Table: amd.Table, SysfsDir: amd.Dir}, nil
}

func (s Settings) InitialTarget() string {
	target := fmt.Sprintf("%s=%d", s.ID, s.Control.Current)
	if s.Mode != "" {
		target += "," + s.Mode
	}
	return target
}

// SameDevice excludes changing values but keeps driver bounds and backend identity.
func (s Settings) SameDevice(other Settings) bool {
	limits := s.Control
	otherLimits := other.Control
	limits.Current, otherLimits.Current = 0, 0
	return s.ID == other.ID && s.PCI == other.PCI && s.Hardware == other.Hardware && s.Firmware == other.Firmware &&
		s.SysfsDir == other.SysfsDir && limits.Kind == otherLimits.Kind && limits.Unit == otherLimits.Unit &&
		limits.Min == otherLimits.Min && limits.Max == otherLimits.Max &&
		((limits.Default == nil && otherLimits.Default == nil) || (limits.Default != nil && otherLimits.Default != nil && *limits.Default == *otherLimits.Default))
}

func (c Change) NVIDIAArgs() []string {
	return []string{"--id=" + c.Before.ID, fmt.Sprintf("--power-limit=%d.%03d", c.Desired/1000, c.Desired%1000)}
}

func (c Change) OverdriveValue() string {
	if c.Before.Control.Kind == CoreMax {
		return fmt.Sprintf("s 1 %d", c.Desired)
	}
	return fmt.Sprintf("vo %d", c.Desired)
}

// OtherTable leaves the driver text intact except for the controlled value.
func (s Settings) OtherTable() string {
	var kept []string
	section := ""
	for line := range strings.SplitSeq(s.Table, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "OD_") {
			section = strings.TrimSuffix(trimmed, ":")
		} else if (s.Control.Kind == CoreMax && section == "OD_SCLK" && strings.HasPrefix(trimmed, "1:")) ||
			(s.Control.Kind == VoltageOffset && section == "OD_VDDGFX_OFFSET" && len(strings.Fields(trimmed)) == 1) {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "\n")
}

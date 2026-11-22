//go:build linux

package hardware

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	OverdriveFile   = "pp_od_clk_voltage"
	PerformanceFile = "power_dpm_force_performance_level"
)

type amdState struct {
	PCI, Device, Subsystem, Mode, Table, Dir string
}

func readAMD(pciRoot, pci string) (amdState, error) {
	if pci == "" || canonicalPCI(pci) != pci {
		return amdState{}, errors.New("ID PCI GPU tidak valid")
	}
	dir := filepath.Join(pciRoot, pci)
	vendor, err := readSys(filepath.Join(dir, "vendor"))
	if err != nil {
		return amdState{}, err
	}
	driver, err := os.Readlink(filepath.Join(dir, "driver"))
	if err != nil {
		return amdState{}, err
	}
	if vendor != "0x1002" || filepath.Base(driver) != "amdgpu" {
		return amdState{}, errors.New("perangkat bukan GPU dengan driver amdgpu")
	}
	device, err := readSys(filepath.Join(dir, "device"))
	if err != nil {
		return amdState{}, err
	}
	if _, err := os.Lstat(filepath.Join(dir, "physfn")); !errors.Is(err, os.ErrNotExist) {
		return amdState{}, errors.New("OverDrive memerlukan perangkat fisik PCI")
	}
	subVendor, err := readSys(filepath.Join(dir, "subsystem_vendor"))
	if err != nil {
		return amdState{}, err
	}
	subDevice, err := readSys(filepath.Join(dir, "subsystem_device"))
	if err != nil {
		return amdState{}, err
	}
	mode, err := readSys(filepath.Join(dir, PerformanceFile))
	if err != nil {
		return amdState{}, err
	}
	if mode != "auto" && mode != "manual" {
		return amdState{}, fmt.Errorf("mode daya GPU %q di luar mode auto dan manual", mode)
	}
	table, err := readSys(filepath.Join(dir, OverdriveFile))
	if err != nil {
		return amdState{}, err
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return amdState{}, err
	}
	return amdState{PCI: pci, Device: device, Subsystem: subVendor + "/" + subDevice, Mode: mode, Table: table, Dir: canonical}, nil
}

func (a amdState) controls() []Control {
	var out []Control
	var gfxMin, gfxMax, gfxRangeMin, gfxRangeMax, voltage, voltageMin, voltageMax *int64
	section := ""
	repeat := make(map[string]bool)
	for line := range strings.SplitSeq(a.Table, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 1 && strings.HasPrefix(fields[0], "OD_") {
			section = strings.TrimSuffix(fields[0], ":")
			if repeat[section] {
				return nil
			}
			repeat[section] = true
			continue
		}
		switch section {
		case "OD_SCLK":
			if len(fields) != 2 || repeat[section+fields[0]] {
				return nil
			}
			repeat[section+fields[0]] = true
			number := numberWithUnit(fields[1], "mhz", 1, 100000)
			switch fields[0] {
			case "0:":
				gfxMin = number
			case "1:":
				gfxMax = number
			default:
				return nil
			}
		case "OD_RANGE":
			if len(fields) != 3 {
				continue
			}
			switch fields[0] {
			case "SCLK:":
				if repeat["SCLK"] {
					return nil
				}
				repeat["SCLK"] = true
				gfxRangeMin = numberWithUnit(fields[1], "mhz", 1, 100000)
				gfxRangeMax = numberWithUnit(fields[2], "mhz", 1, 100000)
			case "VDDGFX_OFFSET:":
				if repeat["VDDGFX_OFFSET"] {
					return nil
				}
				repeat["VDDGFX_OFFSET"] = true
				voltageMin = numberWithUnit(fields[1], "mv", -1000, 1000)
				voltageMax = numberWithUnit(fields[2], "mv", -1000, 1000)
			}
		case "OD_VDDGFX_OFFSET":
			if len(fields) != 1 || repeat["offset"] {
				return nil
			}
			repeat["offset"] = true
			voltage = numberWithUnit(fields[0], "mv", -1000, 1000)
		}
	}
	if gfxMin != nil && gfxMax != nil && gfxRangeMin != nil && gfxRangeMax != nil {
		floor := max(*gfxMin, *gfxRangeMin)
		if floor < *gfxRangeMax && *gfxMax >= floor && *gfxMax <= *gfxRangeMax {
			out = append(out, Control{Kind: CoreMax, Label: "Batas clock inti", Unit: "MHz", Current: *gfxMax, Min: floor, Max: *gfxRangeMax})
		}
	}
	if voltage != nil && voltageMin != nil && voltageMax != nil {
		ceiling := min(*voltageMax, 0)
		if *voltageMin < ceiling && *voltage >= *voltageMin && *voltage <= ceiling {
			out = append(out, Control{Kind: VoltageOffset, Label: "Offset voltase GPU", Unit: "mV", Current: *voltage, Min: *voltageMin, Max: ceiling})
		}
	}
	return out
}

func numberWithUnit(s, unit string, min, max int64) *int64 {
	trimmed, ok := strings.CutSuffix(strings.ToLower(s), unit)
	if !ok {
		return nil
	}
	return boundedInt(trimmed, min, max)
}

func amdMetrics(dir string) Metrics {
	sensor := func(path string, min, max int64) *int64 {
		data, err := readSys(path)
		if err != nil {
			return nil
		}
		return boundedInt(data, min, max)
	}
	m := Metrics{
		UtilizationPct: sensor(filepath.Join(dir, "gpu_busy_percent"), 0, 100),
		VRAMUsedBytes:  sensor(filepath.Join(dir, "mem_info_vram_used"), 0, 1<<50),
		VRAMTotalBytes: sensor(filepath.Join(dir, "mem_info_vram_total"), 0, 1<<50),
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "hwmon", "hwmon*"))
	for _, p := range paths {
		name, _ := readSys(filepath.Join(p, "name"))
		if name != "amdgpu" {
			continue
		}
		scale := func(name string, min, max, denominator int64) *int64 {
			value := sensor(filepath.Join(p, name), min, max)
			if value == nil {
				return nil
			}
			return new(*value / denominator)
		}
		m.TemperatureC = scale("temp1_input", -100000, 200000, 1000)
		m.PowerMilliW = scale("power1_average", 0, 100000000000, 1000)
		if m.PowerMilliW == nil {
			m.PowerMilliW = scale("power1_input", 0, 100000000000, 1000)
		}
		if m.PowerMilliW != nil {
			m.PowerScope = "soc"
		}
		m.CoreMHz = scale("freq1_input", 0, 100000000000, 1000000)
		m.MemoryMHz = scale("freq2_input", 0, 100000000000, 1000000)
		break
	}
	return m
}

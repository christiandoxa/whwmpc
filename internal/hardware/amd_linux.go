//go:build linux

package hardware

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	OverdriveFile        = "pp_od_clk_voltage"
	PerformanceFile      = "power_dpm_force_performance_level"
	coreRangeUnavailable = "Rentang untuk nilai maksimum clock OverDrive belum terverifikasi pada backend AMDGPU ini."
)

var errCoreRangeUnavailable = errors.New(coreRangeUnavailable)

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
	mode, err := readAMDControl(filepath.Join(dir, PerformanceFile))
	if err != nil {
		return amdState{}, err
	}
	if mode != "auto" && mode != "manual" {
		return amdState{}, fmt.Errorf("mode daya GPU %q di luar mode auto dan manual", mode)
	}
	table, err := readAMDControl(filepath.Join(dir, OverdriveFile))
	if err != nil {
		return amdState{}, err
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return amdState{}, err
	}
	return amdState{PCI: pci, Device: device, Subsystem: subVendor + "/" + subDevice, Mode: mode, Table: table, Dir: canonical}, nil
}

func readAMDControl(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0200 == 0 {
		return "", fmt.Errorf("driver tidak menyediakan kontrol %s yang dapat ditulis administrator", filepath.Base(path))
	}
	return readSys(path)
}

func (a amdState) coreMaxMinimum(control Control) (int64, error) {
	// Some PMFW backends print the Fmin floor in OD_RANGE. Their setter checks a separate Fmax floor.
	ip := filepath.Join(a.Dir, "ip_discovery", "die", "0", "MP1", "0")
	if _, err := os.Stat(ip); errors.Is(err, os.ErrNotExist) {
		// Older Renoir kernels may lack IP discovery. These IDs all select CHIP_RENOIR.
		switch a.Device {
		case "0x15e7", "0x1636", "0x1638", "0x164c":
			return control.Min, nil
		}
		return 0, errCoreRangeUnavailable
	}
	var version [3]string
	for i, name := range [...]string{"major", "minor", "revision"} {
		value, err := readSys(filepath.Join(ip, name))
		if err != nil {
			return 0, errCoreRangeUnavailable
		}
		version[i] = value
	}
	switch strings.Join(version[:], ".") {
	case "12.0.0", "12.0.1", "13.0.1", "13.0.3", "13.0.8", "14.0.0", "14.0.1", "14.0.4", "14.0.5":
		return control.Min, nil
	case "13.0.0", "13.0.10":
		return a.smu13CoreMaxMinimum(control, 20)
	case "13.0.7":
		return a.smu13CoreMaxMinimum(control, 22)
	default:
		return 0, errCoreRangeUnavailable
	}
}

func (a amdState) smu13CoreMaxMinimum(control Control, format byte) (int64, error) {
	const pageSize = 4096
	// An AMDGPU pp_table upload cannot fit this 5424-byte table in one 4 KiB sysfs write.
	if os.Getpagesize() != pageSize {
		return 0, errCoreRangeUnavailable
	}
	file, err := os.Open(filepath.Join(a.Dir, "pp_table"))
	if errors.Is(err, os.ErrPermission) {
		return 0, fmt.Errorf("Pembacaan rentang Fmax pp_table memerlukan izin: %w", err)
	}
	if err != nil {
		return 0, errCoreRangeUnavailable
	}
	defer file.Close()
	table, err := io.ReadAll(io.LimitReader(file, pageSize))
	if err != nil || len(table) != pageSize-1 {
		return 0, errCoreRangeUnavailable
	}
	// The observed combo tables use an 832-byte driver portion before PPTable_t.
	if binary.LittleEndian.Uint16(table[:2]) != 5424 || table[2] != format || table[3] != 0 || table[4] != 3 ||
		binary.LittleEndian.Uint16(table[6:8]) != 832 {
		return 0, errCoreRangeUnavailable
	}
	// The kernel reads these fields in SkuTable.OverDriveLimitsMin and OverDriveLimitsBasicMax.
	fminMin := int64(int16(binary.LittleEndian.Uint16(table[3770:3772])))
	fmaxMin := int64(int16(binary.LittleEndian.Uint16(table[3772:3774])))
	fmaxMax := int64(int16(binary.LittleEndian.Uint16(table[3860:3862])))
	if fminMin != control.reportedClockMin || fmaxMax != control.Max || fmaxMin <= 0 || fmaxMin > fmaxMax {
		return 0, errCoreRangeUnavailable
	}
	minimum := max(control.Min, fmaxMin)
	if control.Current < minimum || minimum >= control.Max {
		return minimum, errCoreRangeUnavailable
	}
	return minimum, nil
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
			out = append(out, Control{Kind: CoreMax, Label: "Batas clock inti", Unit: "MHz", Current: *gfxMax,
				Min: floor, Max: *gfxRangeMax, reportedClockMin: *gfxRangeMin})
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

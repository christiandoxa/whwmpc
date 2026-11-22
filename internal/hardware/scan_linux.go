//go:build linux

package hardware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const systemPCI = "/sys/bus/pci/devices"

func Scan(ctx context.Context, runner sysrun.Runner) Snapshot {
	return scanAt(ctx, runner, systemPCI)
}

func scanAt(ctx context.Context, runner sysrun.Runner, pciRoot string) Snapshot {
	if runner == nil {
		runner = sysrun.Native{}
	}
	out := Snapshot{At: time.Now().UTC(), Availability: domain.Unknown}
	entries, discErr := os.ReadDir(pciRoot)
	nvidiaSeen := false
	for _, entry := range entries {
		if ctx.Err() != nil {
			out.Detail = "Pembacaan GPU dihentikan."
			return out
		}
		pci := entry.Name()
		if pci == "" || canonicalPCI(pci) != pci {
			continue
		}
		dir := filepath.Join(pciRoot, pci)
		class, _ := readSys(filepath.Join(dir, "class"))
		if !strings.HasPrefix(class, "0x03") {
			continue
		}
		vendor, _ := readSys(filepath.Join(dir, "vendor"))
		device, _ := readSys(filepath.Join(dir, "device"))
		driver, _ := os.Readlink(filepath.Join(dir, "driver"))
		gpu := Device{ID: pci, PCI: pci, Vendor: vendor, Model: device, Backend: "pci",
			Availability: domain.Unsupported, TuningAvailability: domain.Unsupported, Driver: filepath.Base(driver)}
		if driver == "" {
			gpu.Driver = ""
		}
		switch vendor {
		case "0x1002":
			gpu.Vendor = "AMD"
			if gpu.Driver == "amdgpu" {
				gpu.Backend, gpu.Metrics = "amdgpu-sysfs", amdMetrics(dir)
				gpu.Availability = domain.Partial
				if hasMetrics(gpu.Metrics) {
					gpu.Availability = domain.Available
				}
				state, err := readAMD(pciRoot, pci)
				switch {
				case err == nil:
					gpu.Controls = state.controls()
					i := slices.IndexFunc(gpu.Controls, func(c Control) bool { return c.Kind == CoreMax })
					if i >= 0 {
						minimum, err := state.coreMaxMinimum(gpu.Controls[i])
						if err == nil {
							gpu.Controls[i].Min = minimum
						} else {
							gpu.Controls = slices.Delete(gpu.Controls, i, i+1)
							gpu.TuningDetail = domain.SafeText(err.Error())
							if errors.Is(err, os.ErrPermission) {
								gpu.TuningAvailability = domain.PermissionRequired
							}
						}
					}
					if len(gpu.Controls) > 0 && gpu.TuningDetail != "" {
						gpu.TuningAvailability = domain.Partial
					} else if len(gpu.Controls) > 0 {
						gpu.TuningAvailability = domain.Available
					} else if gpu.TuningDetail == "" {
						gpu.TuningDetail = "OverDrive tidak melaporkan nilai/rentang untuk kontrol yang didukung."
					}
				case errors.Is(err, os.ErrPermission):
					gpu.TuningAvailability = domain.PermissionRequired
					gpu.TuningDetail = "Pembacaan antarmuka OverDrive memerlukan izin."
				default:
					gpu.TuningDetail = domain.SafeText(err.Error())
				}
			}
		case "0x10de":
			gpu.Vendor = "NVIDIA"
			nvidiaSeen = true
		case "0x8086":
			gpu.Vendor = "Intel"
		}
		out.Devices = append(out.Devices, gpu)
	}
	if nvidiaSeen || discErr != nil {
		gpus, status, err := readNVIDIA(ctx, runner, "")
		if err != nil {
			out.Detail = err.Error()
			for i := range out.Devices {
				if out.Devices[i].Vendor == "NVIDIA" && out.Devices[i].Driver != "nouveau" {
					out.Devices[i].Availability, out.Devices[i].TuningAvailability = status, status
					out.Devices[i].TuningDetail = out.Detail
				}
			}
		} else {
			for _, gpu := range gpus {
				i := slices.IndexFunc(out.Devices, func(local Device) bool { return local.PCI == gpu.PCI })
				if i < 0 {
					out.Devices = append(out.Devices, gpu)
				} else {
					out.Devices[i] = gpu
				}
			}
		}
	}
	if len(out.Devices) == 0 {
		if discErr != nil {
			out.Detail = "Inventaris perangkat PCI tidak dapat dibaca."
		} else {
			out.Detail = "Tidak ada perangkat grafis PCI yang terdeteksi."
		}
		return out
	}
	out.Availability = out.Devices[0].Availability
	for _, gpu := range out.Devices[1:] {
		if gpu.Availability != out.Availability {
			out.Availability = domain.Partial
			break
		}
	}
	return out
}

func hasMetrics(m Metrics) bool {
	return m.UtilizationPct != nil || m.TemperatureC != nil || m.VRAMUsedBytes != nil || m.VRAMTotalBytes != nil ||
		m.PowerMilliW != nil || m.CoreMHz != nil || m.MemoryMHz != nil
}

func readSys(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	const limit = 32 << 10
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("nilai sysfs melebihi batas")
	}
	return strings.TrimSpace(string(data)), nil
}

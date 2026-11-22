//go:build linux

package hardware

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

// The requested and enforced limits are different measurements.
const nvidiaQuery = "uuid,pci.bus_id,name,driver_version,utilization.gpu,temperature.gpu,memory.used,memory.total,power.draw,clocks.current.graphics,clocks.current.memory,power.limit,power.min_limit,power.max_limit,power.default_limit,enforced.power.limit"

func readNVIDIA(ctx context.Context, runner sysrun.Runner, target string) ([]Device, domain.Availability, error) {
	executable, err := runner.LookPath("nvidia-smi")
	if err != nil {
		return nil, domain.DependencyMissing, fmt.Errorf("nvidia-smi: %w", err)
	}
	args := []string{"--query-gpu=" + nvidiaQuery, "--format=csv,noheader,nounits"}
	if target != "" {
		if !gpuUUIDPattern.MatchString(target) {
			return nil, domain.Error, errors.New("ID NVIDIA tidak valid")
		}
		args = append(args, "--id="+target)
	}
	result := runner.Run(ctx, executable, args...)
	if result.Err != nil || result.ExitCode != 0 {
		status := domain.Error
		switch {
		case result.ExitCode == 4 || errors.Is(result.Err, os.ErrPermission):
			status = domain.PermissionRequired
		case result.ExitCode == 6 || result.ExitCode == 9:
			status = domain.Unknown
		case result.ExitCode == 12:
			status = domain.DependencyMissing
		case result.ExitCode == 2 || result.ExitCode == 3:
			status = domain.Unsupported
		}
		return nil, status, fmt.Errorf("nvidia-smi tidak dapat membaca GPU (kode %d)", result.ExitCode)
	}
	gpus, err := parseNVIDIA(result.Output)
	if err != nil {
		return nil, domain.Error, err
	}
	if target != "" && (len(gpus) != 1 || gpus[0].ID != target) {
		return nil, domain.Stale, errors.New("GPU NVIDIA yang terpilih sudah berubah atau hilang")
	}
	return gpus, domain.Available, nil
}

func parseNVIDIA(data []byte) ([]Device, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = 16
	r.TrimLeadingSpace = true
	var out []Device
	seen := make(map[string]bool)
	for {
		fields, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("output CSV nvidia-smi tidak dikenali")
		}
		id := strings.TrimSpace(fields[0])
		pci := canonicalPCI(fields[1])
		if !gpuUUIDPattern.MatchString(id) || pci == "" || seen[id] || seen[pci] {
			return nil, errors.New("identitas perangkat nvidia-smi ambigu atau tidak dikenali")
		}
		seen[id], seen[pci] = true, true
		gpu := Device{ID: id, PCI: pci, Vendor: "NVIDIA", Driver: "nvidia", Model: domain.SafeText(fields[2]),
			Version: domain.SafeText(fields[3]), Backend: "nvidia-smi",
			Availability: domain.Available, TuningAvailability: domain.Unsupported}
		gpu.Metrics = Metrics{
			UtilizationPct: boundedInt(fields[4], 0, 100), TemperatureC: boundedInt(fields[5], -100, 200),
			VRAMUsedBytes: mibToBytes(fields[6]), VRAMTotalBytes: mibToBytes(fields[7]),
			PowerMilliW: wattsToMilli(fields[8]), PowerScope: "board",
			CoreMHz: boundedInt(fields[9], 0, 100000), MemoryMHz: boundedInt(fields[10], 0, 100000),
			EnforcedMilliW: wattsToMilli(fields[15]),
		}
		if !hasMetrics(gpu.Metrics) {
			gpu.Availability = domain.Partial
		}
		current, min, max, reset := wattsToMilli(fields[11]), wattsToMilli(fields[12]), wattsToMilli(fields[13]), wattsToMilli(fields[14])
		if current != nil && min != nil && max != nil && reset != nil &&
			*min > 0 && *min < *max && *current >= *min && *current <= *max && *reset >= *min && *reset <= *max {
			// wattsToMilli has checked that at most three decimal places are reported.
			_, fraction, _ := strings.Cut(strings.TrimSpace(fields[11]), ".")
			step := [...]int64{1000, 100, 10, 1}[len(fraction)]
			gpu.Controls = []Control{{Kind: PowerLimit, Label: "Batas daya GPU", Unit: "mW",
				Current: *current, Min: *min, Max: *max, Default: reset, readbackStep: step}}
			gpu.TuningAvailability = domain.Available
		} else {
			gpu.TuningDetail = "Backend tidak melaporkan batas, nilai awal, dan nilai default daya yang dapat divalidasi."
		}
		out = append(out, gpu)
	}
	if len(out) == 0 {
		return nil, errors.New("nvidia-smi tidak melaporkan GPU")
	}
	return out, nil
}

func mibToBytes(text string) *int64 {
	value := boundedInt(text, 0, 1<<30)
	if value == nil {
		return nil
	}
	return new(*value * (1 << 20))
}

func wattsToMilli(text string) *int64 {
	whole, fraction, dot := strings.Cut(strings.TrimSpace(text), ".")
	if len(whole) == 0 || strings.Trim(whole, "0123456789") != "" {
		return nil
	}
	watts := boundedInt(whole, 0, 100000)
	if watts == nil {
		return nil
	}
	if !dot {
		return new(*watts * 1000)
	}
	if len(fraction) == 0 || len(fraction) > 3 || strings.Trim(fraction, "0123456789") != "" {
		return nil
	}
	milli := boundedInt(fraction+strings.Repeat("0", 3-len(fraction)), 0, 999)
	return new(*watts*1000 + *milli)
}

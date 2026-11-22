//go:build linux

package hardware

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const sampleUUID = "GPU-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
const sampleNVIDIA = sampleUUID + ", 00000000:01:00.0, GPU fixture, 615.71.09, 0, 35, 123, 12288, 12.15, 210, [N/A], 160.00, 100.00, 170.00, 170.00, 150.00\n"

type gpuRunner struct {
	lookErr error
	result  sysrun.Result
	args    []string
}

func (r *gpuRunner) LookPath(string) (string, error) { return "/fixture/nvidia-smi", r.lookErr }
func (r *gpuRunner) Run(_ context.Context, _ string, args ...string) sysrun.Result {
	r.args = slices.Clone(args)
	return r.result
}

func TestNVIDIACSVKeepsUnknownAndRequestedLimit(t *testing.T) {
	devices, err := parseNVIDIA([]byte(sampleNVIDIA))
	if err != nil {
		t.Fatal(err)
	}
	d := devices[0]
	control := d.Controls[0]
	if d.PCI != "0000:01:00.0" || d.Metrics.MemoryMHz != nil || *d.Metrics.UtilizationPct != 0 ||
		control.Kind != PowerLimit || control.Current != 160000 || *d.Metrics.EnforcedMilliW != 150000 {
		t.Fatalf("fields misread: %+v %+v", d, control)
	}
	if _, err := parseNVIDIA([]byte(sampleNVIDIA + sampleNVIDIA)); err == nil {
		t.Fatal("duplicate GPU was accepted")
	}
	if _, err := parseNVIDIA([]byte(sampleUUID + ",bad pci")); err == nil {
		t.Fatal("malformed output was accepted")
	}
}

func TestNVIDIAChangeUsesExactBoundsAndTarget(t *testing.T) {
	runner := &gpuRunner{result: sysrun.Result{Output: []byte(sampleNVIDIA)}}
	ctx := context.Background()
	change, err := Prepare(ctx, runner, PowerLimit, sampleUUID+"=155500")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(change.NVIDIAArgs(), []string{"--id=" + sampleUUID, "--power-limit=155.500"}) ||
		!slices.Contains(runner.args, "--id="+sampleUUID) || change.Before.InitialTarget() != sampleUUID+"=160000" {
		t.Fatalf("unexpected change %+v, args %v", change, runner.args)
	}
	for _, bad := range []string{sampleUUID + "=99999", sampleUUID + "=170001", sampleUUID + "=+155500", sampleUUID + "=155000,auto", sampleUUID + "=-2", "0=155500", "--id=111"} {
		if _, err := Prepare(ctx, runner, PowerLimit, bad); err == nil {
			t.Fatalf("accepted target %q", bad)
		}
	}
	other := strings.ReplaceAll(sampleNVIDIA, sampleUUID, "GPU-aaaaaaaa-bbbb-cccc-dddd-ffffffffffff")
	runner.result.Output = []byte(other)
	if _, status, err := readNVIDIA(ctx, runner, sampleUUID); err == nil || status != domain.Stale {
		t.Fatal("a replacement GPU returned the original target as current")
	}
	runner.result = sysrun.Result{ExitCode: 4}
	if _, status, err := readNVIDIA(ctx, runner, sampleUUID); err == nil || status != domain.PermissionRequired {
		t.Fatal("permission required was lost")
	}
	runner.lookErr = os.ErrNotExist
	if _, status, err := readNVIDIA(ctx, runner, sampleUUID); !errors.Is(err, os.ErrNotExist) || status != domain.DependencyMissing {
		t.Fatal("missing executable was not distinguished")
	}
}

func TestNVIDIAChangeFitsReportedReadbackPrecision(t *testing.T) {
	var originalJSON string
	for _, tc := range []struct {
		reported string
		reject   string
		accept   string
	}{
		{"160", "169999", "169000"},
		{"160.0", "169990", "169900"},
		{"160.00", "169999", "169990"},
		{"160.000", "", "169999"},
	} {
		t.Run(tc.reported, func(t *testing.T) {
			row := strings.Replace(sampleNVIDIA, ", 160.00,", ", "+tc.reported+",", 1)
			runner := &gpuRunner{result: sysrun.Result{Output: []byte(row)}}
			if tc.reject != "" {
				if _, err := Prepare(t.Context(), runner, PowerLimit, sampleUUID+"="+tc.reject); err == nil ||
					!strings.Contains(err.Error(), "pembacaan ulang nvidia-smi") {
					t.Fatalf("unverifiable request accepted: %v", err)
				}
			}
			change, err := Prepare(t.Context(), runner, PowerLimit, sampleUUID+"="+tc.accept)
			if err != nil || change.Before.InitialTarget() != sampleUUID+"=160000" {
				t.Fatalf("representable request rejected or original lost: %+v, %v", change, err)
			}
			encoded, err := json.Marshal(change.Before)
			if err != nil {
				t.Fatal(err)
			}
			if originalJSON == "" {
				originalJSON = string(encoded)
			}
			if string(encoded) != originalJSON {
				t.Fatal("readback formatting changed the serialized restore identity")
			}
		})
	}
}

const amdTable = `OD_SCLK:
0: 500Mhz
1: 2150MHz
OD_MCLK:
0: 100Mhz
1: 1000MHz
OD_RANGE:
SCLK: 400Mhz 2600Mhz
VDDGFX_OFFSET: -150mv 25mv
OD_VDDGFX_OFFSET:
-10mV
`

func TestAMDControlsRequireDriverReportedBounds(t *testing.T) {
	state := amdState{Mode: "auto", Table: amdTable}
	controls := state.controls()
	if len(controls) != 2 || controls[0].Current != 2150 || controls[0].Min != 500 || controls[0].Max != 2600 ||
		controls[1].Current != -10 || controls[1].Min != -150 || controls[1].Max != 0 {
		t.Fatalf("unexpected controls: %+v", controls)
	}
	state.Table = strings.ReplaceAll(amdTable, "VDDGFX_OFFSET: -150mv 25mv\n", "")
	if got := state.controls(); len(got) != 1 || got[0].Kind != CoreMax {
		t.Fatalf("missing voltage bounds should disable only the voltage control: %+v", got)
	}
	state.Table = strings.ReplaceAll(amdTable, "0: 500Mhz", "0: 500Mhz 1200mV")
	if got := state.controls(); len(got) != 0 {
		t.Fatalf("unsafe legacy tuple was accepted: %+v", got)
	}
	for _, target := range []string{"../../device=5", "0000:04:00.0=+100", "0000:04:00.0=700,auto"} {
		if _, _, _, ok := ParseTarget(CoreMax, target); ok {
			t.Fatalf("accepted user-supplied invalid target %q", target)
		}
	}
	if _, value, mode, ok := ParseTarget(RestoreVoltageOffset, "0000:04:00.0=-5,manual"); !ok || value != -5 || mode != "manual" {
		t.Fatal("signed restore did not retain the prior mode")
	}
}

func TestLinuxScanPreservesBackendCoverage(t *testing.T) {
	root := t.TempDir()
	pci := "0000:08:00.0"
	card := filepath.Join(root, pci)
	hwmon := filepath.Join(card, "hwmon", "hwmon1")
	if err := os.MkdirAll(hwmon, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"class": "0x030000", "vendor": "0x1002", "device": "0x1636", "subsystem_vendor": "0x1002", "subsystem_device": "0x1234",
		"gpu_busy_percent": "0", "mem_info_vram_total": "536870912",
		PerformanceFile: "auto", OverdriveFile: amdTable,
		"hwmon/hwmon1/name": "amdgpu", "hwmon/hwmon1/temp1_input": "52091", "hwmon/hwmon1/power1_average": "4000000",
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(card, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../../drivers/amdgpu", filepath.Join(card, "driver")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "0000:01:00.0")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]string{"class": "0x030200", "vendor": "0x10de", "device": "0x1234"} {
		if err := os.WriteFile(filepath.Join(other, field), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := scanAt(context.Background(), &gpuRunner{lookErr: os.ErrNotExist}, root)
	if len(snapshot.Devices) != 2 || snapshot.Availability != domain.Partial {
		t.Fatalf("GPU coverage incorrect: %+v", snapshot)
	}
	d := snapshot.Devices[1]
	if d.Backend != "amdgpu-sysfs" || *d.Metrics.TemperatureC != 52 || d.Metrics.MemoryMHz != nil ||
		d.Metrics.PowerScope != "soc" || *d.Metrics.PowerMilliW != 4000 || len(d.Controls) != 2 {
		t.Fatalf("AMD data incorrect: %+v", d)
	}
	if snapshot.Devices[0].Availability != domain.DependencyMissing {
		t.Fatalf("missing NVIDIA tool yielded %+v", snapshot.Devices[0])
	}
	for _, name := range []string{OverdriveFile, PerformanceFile} {
		t.Run(name+" read-only", func(t *testing.T) {
			path := filepath.Join(card, name)
			if err := os.Chmod(path, 0400); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(path, 0600); err != nil {
					t.Error(err)
				}
			})
			device := scanAt(t.Context(), &gpuRunner{lookErr: os.ErrNotExist}, root).Devices[1]
			if device.Availability != domain.Available || device.Metrics.TemperatureC == nil ||
				device.TuningAvailability != domain.Unsupported || len(device.Controls) != 0 || !strings.Contains(device.TuningDetail, name) {
				t.Fatalf("read-only attribute affected availability incorrectly: %+v", device)
			}
			if _, err := amdSettings(root, CoreMax, pci); err == nil {
				t.Fatal("read-only attribute accepted by the planner")
			}
		})
	}
}

//go:build linux

package actions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/hardware"
)

func gpuFixture(value int64, mode string) hardware.Settings {
	return hardware.Settings{ID: "0000:08:00.0", PCI: "0000:08:00.0", SysfsDir: "/sys/devices/pci0000:00/mock", Mode: mode,
		Table:   fmt.Sprintf("OD_SCLK:\n0: 400Mhz\n1: %dMhz\nOD_RANGE:\nSCLK: 400Mhz 2600Mhz", value),
		Control: hardware.Control{Kind: hardware.CoreMax, Unit: "MHz", Current: value, Min: 400, Max: 2600}}
}

func TestExecuteOverdriveCommitsAndRestoresMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		before  hardware.Settings
		desired int64
		mode    string
		want    []string
	}{
		{"apply", gpuFixture(2150, "auto"), 2450, "manual", []string{"manual", "s 1 2450", "c"}},
		{"restore", gpuFixture(2450, "manual"), 2150, "auto", []string{"s 1 2150", "c", "auto"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, pending := tc.before, tc.before
			var seen []string
			change := hardware.Change{Before: tc.before, Desired: tc.desired, Mode: tc.mode}
			step := executeOverdrive(context.Background(), change, func(context.Context) (hardware.Settings, error) { return current, nil },
				func(_ context.Context, path, value string) attempt {
					seen = append(seen, value)
					switch {
					case path == hardware.PerformanceFile:
						current.Mode = value
					case value == "c":
						current.Table, current.Control = pending.Table, pending.Control
					default:
						pending = gpuFixture(tc.desired, current.Mode)
					}
					return attempt{started: true}
				})
			if step.err != nil || !slices.Equal(seen, tc.want) {
				t.Fatalf("steps %v, result %+v", seen, step)
			}
			result := verifyGPU(change, current, "read-back", step)
			if result.Status != Verified || result.Changed == nil || !*result.Changed || result.AfterIdentity != "read-back" {
				t.Fatalf("unexpected result %+v", result)
			}
		})
	}
}

func TestGPUChangeStopsOnConflictAndClassifiesCancellation(t *testing.T) {
	before := gpuFixture(2150, "auto")
	change := hardware.Change{Before: before, Desired: 2450, Mode: "manual"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	current := before
	calls := 0
	step := executeOverdrive(ctx, change, func(context.Context) (hardware.Settings, error) { return current, nil }, func(_ context.Context, path, value string) attempt {
		calls++
		current.Mode = "manual"
		cancel()
		return attempt{started: true}
	})
	got := verifyGPU(change, current, "id", step)
	if calls != 1 || !step.stopped || got.Status != Partial || got.AfterIdentity != "" {
		t.Fatalf("cancellation hid the partial mode change: %+v %+v", step, got)
	}
	current = before
	current.Table += "\nexternal-value"
	calls = 0
	step = executeOverdrive(context.Background(), change, func(context.Context) (hardware.Settings, error) { return current, nil }, func(context.Context, string, string) attempt {
		calls++
		return attempt{started: true}
	})
	if calls != 0 || step.err == nil || step.started {
		t.Fatalf("wrote stale target: %+v", step)
	}
	current = gpuFixture(2450, "manual")
	got = verifyGPU(change, current, "id", attempt{started: true, err: errors.New("commit interrupted")})
	if got.Status != Partial || got.AfterIdentity != "" {
		t.Fatalf("assumed commit succeeded: %+v", got)
	}
}

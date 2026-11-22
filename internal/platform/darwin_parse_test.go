package platform

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func TestDarwinCPUPreservesIntervalsBeforeDeadline(t *testing.T) {
	const first = "CPU usage: 100% user, 0% sys, 0% idle\n"
	const intervals = "CPU usage: 10% user, 0% sys, 90% idle\nCPU usage: 20% user, 0% sys, 80% idle\n"
	const last = "CPU usage: 30% user, 0% sys, 70% idle\n"
	for _, tc := range []struct {
		name, output string
		err          error
		status       domain.Availability
		samples      []float64
	}{
		{"complete log", first + intervals + last, nil, domain.Available, []float64{10, 20, 30}},
		{"deadline with samples", first + intervals + "CPU usage: truncated", context.DeadlineExceeded, domain.Partial, []float64{10, 20}},
		{"deadline before intervals", first, context.DeadlineExceeded, domain.Error, nil},
		{"cancellation", first + intervals, context.Canceled, domain.Error, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := darwinCPUResult(sysrun.Result{Output: []byte(tc.output), Err: tc.err})
			if got.Availability != tc.status || !slices.Equal(got.Samples, tc.samples) {
				t.Fatalf("CPU status = %s, samples = %v", got.Availability, got.Samples)
			}
		})
	}
}

func TestDarwinSensorParsers(t *testing.T) {
	raw := "CPU usage: 100% user, 0% sys, 0% idle\nCPU usage: 2.4% user, 7.6% sys, 90.00% idle\nCPU usage: invalid, 120% idle\n"
	if p := darwinCPUSamples(raw); len(p) != 1 || math.Abs(p[0]-10) > 0.01 {
		t.Fatalf("CPU samples %v", p)
	}
	raw = "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:  56.\nPages inactive: 9999.\n"
	if n, valid := darwinFreeMemory(raw); !valid || n != 917504 {
		t.Fatalf("free pages %d, %v", n, valid)
	}
	if _, ok := darwinFreeMemory("page size of 1 bytes\nPages free:  50."); ok {
		t.Fatal("accepted malformed page size")
	}
	if p := darwinPS(" 120.0 2000 /System/Test Helper\n999.0 2 /bad\n0.0 500 kernel_task", 4); len(p) != 2 || p[0].CPU != 30 || p[0].Name != "Test Helper" {
		t.Fatalf("processes %+v", p)
	}
	battery := darwinPower("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=314)\t66%; discharging;")
	if battery.OnAC == nil || *battery.OnAC || battery.Percent == nil || *battery.Percent != 66 ||
		battery.Availability != domain.Available || battery.BatteryStatus != "Discharging" {
		t.Fatalf("power %+v", battery)
	}
	if unknown := darwinPower("garbage 99%;"); unknown.Percent != nil || unknown.Availability != domain.Unknown {
		t.Fatalf("unidentified power source was treated as a battery")
	}
	if partial := darwinPower("-InternalBattery-0 (id=314)\t66%; not charging;"); partial.OnAC != nil ||
		partial.Availability != domain.Partial || partial.Percent == nil || partial.BatteryStatus != "Not charging" {
		t.Fatalf("battery reading without a power source was lost or marked complete: %+v", partial)
	}
	for _, input := range []struct{ raw, want string }{
		{"charging", "Charging"}, {"not charging", "Not charging"}, {"charged", "Full"}, {"unknown", ""},
	} {
		battery := darwinPower("Now drawing from 'AC Power'\n -InternalBattery-0 (id=314)\t66%; " + input.raw + ";")
		if battery.OnAC == nil || !*battery.OnAC || battery.BatteryStatus != input.want {
			t.Fatalf("%s: %+v", input.raw, battery)
		}
	}
}

func TestDarwinPMSetBatteryPhases(t *testing.T) {
	// pmset prints these phases before the optional time, health, and presence fields.
	for _, fixture := range []struct {
		name, source, body, status string
		percent                    float64
	}{
		{"paused on AC", "AC Power", "80%; AC attached; not charging present: true", "Not charging", 80},
		{"finishing charge", "AC Power", "100%; finishing charge; 0:05 remaining present: true", "Charging", 100},
		{"charged with health details", "AC Power", "98%; charged (Fair/Good) present: true", "Full", 98},
		{"discharging with raw connection flag", "Battery Power", "80%; discharging rawExternalConnected: 0 present: true", "Discharging", 80},
		{"unrecognized phase", "AC Power", "80%; charging paused present: true", "", 80},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got := darwinPower("Now drawing from '" + fixture.source + "'\n -InternalBattery-0 (id=314)\t" + fixture.body)
			if got.OnAC == nil || *got.OnAC != (fixture.source == "AC Power") || got.Availability != domain.Available ||
				got.Percent == nil || *got.Percent != fixture.percent || got.BatteryStatus != fixture.status {
				t.Fatalf("unexpected power reading %+v", got)
			}
		})
	}
	got := darwinPower(" -InternalBattery-0 (id=314)\tcharging; (no estimate) present: true")
	if got.Percent != nil || got.OnAC != nil || got.Availability != domain.Partial || got.BatteryStatus != "Charging" {
		t.Fatalf("expected independently reported charging status, got %+v", got)
	}
}

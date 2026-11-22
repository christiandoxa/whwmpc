package platform

import (
	"math"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

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
	ac, charge, status := darwinPower("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=314)\t66%; discharging;")
	if ac == nil || *ac || charge == nil || *charge != 66 || status != domain.Available {
		t.Fatalf("power %v %v %s", ac, charge, status)
	}
	_, charge, status = darwinPower("garbage 99%;")
	if charge != nil || status != domain.Unknown {
		t.Fatalf("unidentified power source was treated as a battery")
	}
}

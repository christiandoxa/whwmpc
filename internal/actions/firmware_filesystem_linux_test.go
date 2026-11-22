package actions

import (
	"context"
	"fmt"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type firmwarePowerRunner map[string]string

func (r firmwarePowerRunner) LookPath(string) (string, error) { return "/usr/bin/busctl", nil }

func (r firmwarePowerRunner) Run(_ context.Context, _ string, args ...string) sysrun.Result {
	if args[len(args)-1] == "IsPresent" && args[len(args)-2] != "org.freedesktop.UPower.Device" {
		return sysrun.Result{Err: fmt.Errorf("wrong DisplayDevice interface")}
	}
	value, ok := r[args[len(args)-1]]
	if !ok {
		return sysrun.Result{ExitCode: 1}
	}
	return sysrun.Result{Output: []byte(value)}
}

func TestFirmwarePowerRequiresACAndAdmissibleCharge(t *testing.T) {
	for _, tt := range []struct {
		charge, threshold, onBattery, hasBattery string
		eligible                                 bool
	}{
		{"u 95", "u 40", "b false", "b true", true},
		{"u 101", "u 40", "b false", "b false", true},
		{"u 101", "u 101", "b false", "b false", true},
		{"u 39", "u 40", "b false", "b true", false},
		{"u 101", "u 40", "b false", "b true", false},
		{"u 101", "u 40", "b false", "", false},
		{"u 101", "u 101", "b false", "b true", false},
		{"u 101", "u 101", "b false", "", false},
		{"u 95", "u 40", "b true", "b true", false},
	} {
		host := linuxPlatform{runner: firmwarePowerRunner{
			"BatteryLevel": tt.charge, "BatteryThreshold": tt.threshold, "OnBattery": tt.onBattery, "IsPresent": tt.hasBattery,
		}}
		if err := checkFirmwarePower(context.Background(), host); (err == nil) != tt.eligible {
			t.Fatalf("power gate %+v: %v", tt, err)
		}
	}
}

func TestFilesystemRepairRequiresReadOnlyConfirmation(t *testing.T) {
	for _, tt := range []struct {
		exit int
		read string
		want Status
	}{
		{1, "clean", Verified},
		{2, "clean", Partial},
		{4, "errors_detected", Partial},
		{8, "clean", Unknown},
		{1, "unknown", Unknown},
	} {
		if got := filesystemResult(tt.exit, filesystem.CheckResult{State: tt.read}, attempt{}); got.Status != tt.want {
			t.Fatalf("repair verification %+v produced %+v", tt, got)
		}
	}
}

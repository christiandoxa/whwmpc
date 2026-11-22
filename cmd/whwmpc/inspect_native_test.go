//go:build darwin || windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

func TestNativeCLI(t *testing.T) {
	binary := os.Getenv("WHWMPC_NATIVE_BINARY")
	if binary == "" {
		t.Skip("set WHWMPC_NATIVE_BINARY to a build on a native test host")
	}
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		data, err := exec.CommandContext(ctx, binary, args...).Output()
		if err != nil {
			t.Fatalf("%v failed: %v", args, err)
		}
		return data
	}
	power := string(run("power"))
	if !strings.Contains(power, "Status daya available") || strings.Contains(power, "CPUFreq") {
		t.Fatalf("wrong native power summary: %s", power)
	}
	smartSource, trimSource := storage.DiskUtilitySource, storage.DarwinTRIMSource
	powerSource := "macOS pmset -g batt"
	if runtime.GOOS == "windows" {
		smartSource, trimSource = storage.WindowsStorageSource, storage.WindowsTRIMSource
		powerSource = "Win32 GetSystemPowerStatus"
	}
	trim := string(run("trim"))
	if !strings.Contains(trim, trimSource) || strings.Contains(trim, "fstrim") {
		t.Fatalf("wrong native TRIM summary: %s", trim)
	}
	if runtime.GOOS == "windows" && !strings.Contains(trim, "ntfs") {
		t.Fatal("Windows TRIM summary omitted the NTFS notification setting")
	}
	var doc report.Report
	if err := json.Unmarshal(run("check", "--json"), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Source != "live" || doc.Overview == nil || doc.Overview.OS != runtime.GOOS || doc.Overview.Arch != runtime.GOARCH ||
		doc.Power == nil || doc.Power.PowerStatus != domain.Available || doc.SMART == nil || doc.SMART.Source != smartSource ||
		len(doc.SMART.Drives) == 0 || doc.Trim == nil || doc.Trim.Source != trimSource || doc.Trim.Manual != domain.Unsupported {
		t.Fatal("native report omitted expected readings or source labels")
	}
	var powerReported bool
	for _, f := range doc.Findings {
		if strings.HasPrefix(f.ID, "power.cpufreq") || f.ID == "power.profiles" {
			t.Fatalf("Linux-only capability counted as a native finding: %s", f.ID)
		}
		powerReported = powerReported || f.ID == "power.supply" && f.Source == powerSource
	}
	if !powerReported {
		t.Fatal("report omitted the native power finding")
	}
	var healthReported bool
	for _, d := range doc.SMART.Drives {
		healthReported = healthReported || d.HealthStatus != ""
	}
	if runtime.GOOS == "windows" && (!healthReported || len(doc.Trim.Policy) == 0) {
		t.Fatal("Windows report lost the Storage provider result or notification policy")
	}
	t.Logf("%s/%s CLI power=%s battery=%t drive health=%s disks=%d TRIM=%s policies=%d", runtime.GOOS, runtime.GOARCH,
		doc.Power.PowerStatus, len(doc.Power.Batteries) > 0, doc.SMART.Availability, len(doc.SMART.Drives), doc.Trim.Availability, len(doc.Trim.Policy))
}

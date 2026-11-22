//go:build darwin || windows

package storage

import (
	"os"
	"runtime"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func TestNativeStorageReadOnly(t *testing.T) {
	if os.Getenv("WHWMPC_TEST_NATIVE") != "1" {
		t.Skip("native CI explicitly requests live read-only telemetry")
	}
	c := Collector{}
	smartSource, trimSource := DiskUtilitySource, DarwinTRIMSource
	if runtime.GOOS == "windows" {
		smartSource, trimSource = WindowsStorageSource, WindowsTRIMSource
	}
	smart, trim := c.InspectSMART(t.Context()), c.InspectTrim(t.Context())
	if smart.Source != smartSource || smart.CapturedAt.IsZero() || smart.Availability == "" ||
		smart.Availability == domain.Error || smart.Availability == domain.DependencyMissing {
		t.Errorf("native disk inventory failed: %s from %s", smart.Availability, smart.Source)
	}
	if trim.Source != trimSource || trim.CapturedAt.IsZero() || trim.Availability == "" || trim.Manual != domain.Unsupported {
		t.Errorf("native TRIM capability is invalid: %s from %s", trim.Availability, trim.Source)
	}
	t.Logf("%s/%s health=%s disk count=%d trim evidence=%s items=%d",
		runtime.GOOS, runtime.GOARCH, smart.Availability, len(smart.Drives), trim.Availability, len(trim.Policy))
}

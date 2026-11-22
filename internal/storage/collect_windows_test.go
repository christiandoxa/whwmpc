package storage

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type windowsStorageFixture struct {
	sysrun.Native
	output []byte
}

func (f windowsStorageFixture) Run(context.Context, string, ...string) sysrun.Result {
	return sysrun.Result{Output: f.output}
}

func TestWindowsSMARTCoverageAtInventoryLimit(t *testing.T) {
	limit := strconv.Itoa(windowsSMARTDriveLimit)
	if !strings.Contains(windowsStorageRead, "Select-Object -First "+limit+")") {
		t.Fatal("PowerShell selection and decoder limits disagree")
	}
	for _, fixture := range []struct {
		count  int
		status domain.Availability
	}{
		{windowsSMARTDriveLimit - 1, domain.Available},
		{windowsSMARTDriveLimit, domain.Partial},
	} {
		c := Collector{Runner: windowsStorageFixture{output: windowsSMARTFixture(fixture.count)}}
		audit := c.InspectSMART(t.Context())
		if audit.Source != WindowsStorageSource || audit.Availability != fixture.status || len(audit.Drives) != fixture.count ||
			nativeDriveStatus(audit.Drives) != domain.Available || audit.Drives[fixture.count-1].HealthStatus != "unhealthy" {
			t.Fatalf("%d records: %s, %d retained drives, detail %q", fixture.count, audit.Availability, len(audit.Drives), audit.Detail)
		}
		if atLimit := fixture.count == windowsSMARTDriveLimit; atLimit != strings.Contains(audit.Detail, limit) {
			t.Fatalf("%d records: unexpected inventory limit detail %q", fixture.count, audit.Detail)
		}
	}
}

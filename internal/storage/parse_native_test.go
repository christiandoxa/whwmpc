package storage

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func TestNativeStorageParsers(t *testing.T) {
	disks, err := parseDarwinDisks([]byte(`<?xml version="1.0"?><plist><dict><key>WholeDisks</key><array><string>disk0</string></array><key>Sizes</key><array><integer>22</integer></array></dict></plist>`))
	if err != nil || len(disks) != 1 || disks[0] != "disk0" {
		t.Fatalf("diskutil list %+v %v", disks, err)
	}
	const plist = `<plist><dict><key>DeviceIdentifier</key><string>disk0</string><key>WholeDisk</key><true/><key>SMARTStatus</key><string>Failing</string><key>SolidState</key><true/></dict></plist>`
	d := parseDarwinSMART("disk0", []byte(plist), time.Now())
	if d.Health == nil || d.Health.Passed == nil || *d.Health.Passed || d.Availability != domain.Available {
		t.Fatalf("missing failed SMART %+v", d)
	}
	if d := parseDarwinSMART("disk1", []byte(plist), time.Now()); d.Health != nil || d.Availability != domain.Error {
		t.Fatalf("accepted changed ID %+v", d)
	}
	if d := parseDarwinSMART("disk0", []byte(strings.Replace(plist, "WholeDisk</key><true/>", "WholeDisk</key><false/>", 1)), time.Now()); d.Health != nil || d.Availability != domain.Error {
		t.Fatalf("accepted a partition as a whole disk %+v", d)
	}
	if d := parseDarwinSMART("disk0", []byte(strings.Replace(plist, "Failing", "Not Supported", 1)), time.Now()); d.Availability != domain.Unsupported {
		t.Fatalf("unsupported SMART %+v", d)
	}

	counters := `[{"health":0,"media":4,"bus":17,"counters":{"temperature":28,"wear":4,"hours":42,"read":18446744073709551615}},{"health":5,"counterStatus":"permission"},{"health":2},{"health":0,"counterStatus":"permission"}]`
	got, err := parseWindowsSMART([]byte(counters), time.Now())
	if err != nil || got[0].Health.ReadErrorsUncorrected != "18446744073709551615" || got[0].Transport != "NVMe" ||
		got[1].Health != nil || got[1].Availability != domain.PermissionRequired || got[2].HealthStatus != "unhealthy" ||
		got[3].Availability != domain.Partial {
		t.Fatalf("storage counters %+v %v", got, err)
	}
	if _, err := parseWindowsSMART([]byte(`[{"health":"healthy"}]`), time.Now()); err == nil {
		t.Fatal("accepted malformed Windows health record")
	}
}

func windowsSMARTFixture(count int) []byte {
	const healthy = `{"health":0,"counterStatus":"available","counters":{"read":0}}`
	const unhealthy = `{"health":2,"counterStatus":"available","counters":{"read":7}}`
	return []byte("[" + strings.Repeat(healthy+",", count-1) + unhealthy + "]")
}

func TestWindowsSMARTInventoryLimit(t *testing.T) {
	for _, count := range []int{windowsSMARTDriveLimit - 1, windowsSMARTDriveLimit} {
		drives, err := parseWindowsSMART(windowsSMARTFixture(count), time.Now())
		if err != nil || len(drives) != count {
			t.Fatalf("%d valid records returned %d drives, error %v", count, len(drives), err)
		}
		last := drives[len(drives)-1]
		if last.HealthStatus != "unhealthy" || last.Health == nil || last.Health.ReadErrorsUncorrected != "7" {
			t.Fatalf("last drive measurement lost: %+v", last)
		}
	}
	if drives, err := parseWindowsSMART(windowsSMARTFixture(windowsSMARTDriveLimit+1), time.Now()); err == nil || len(drives) != 0 {
		t.Fatalf("accepted %d records beyond the collector limit", len(drives))
	}
}

func TestNativeTrimIsOnlyPolicy(t *testing.T) {
	got := parseWindowsTrim("NTFS DisableDeleteNotify = 0  (Disabled)\r\nReFS DisableDeleteNotify = 1  (Enabled)\r\n")
	if len(got) != 2 || !got[0].Enabled || got[1].Enabled {
		t.Fatalf("inverted notification policy %+v", got)
	}
	got, err := parseDarwinTrim([]byte(`{"SPNVMeDataType":[{"_items":[{"SSD":{"spsata_trim_support":"Yes"}}]}],"SPSerialATADataType":[{"spsata_trim_support":"No"}]}`))
	if err != nil || len(got) != 2 || !got[0].Enabled || got[1].Enabled {
		t.Fatalf("invalid Darwin policy %+v %v", got, err)
	}
	if nativeCommandStatus(sysrun.Result{Err: fs.ErrPermission}) != domain.PermissionRequired ||
		nativeCommandStatus(sysrun.Result{ExitCode: 127}) != domain.DependencyMissing ||
		nativeCommandStatus(sysrun.Result{Err: errors.New("malformed")}) != domain.Error {
		t.Fatal("read failures lost capability distinction")
	}
}

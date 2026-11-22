package filesystem

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const otherVolumeUUID = "22222222-2222-2222-2222-222222222222"

const apfsFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Containers</key><array>
  <dict>
    <key>APFSContainerUUID</key><string>AAAABBBB-CCCC-DDDD-EEEE-123456789ABC</string>
    <key>Volumes</key><array>
      <dict>
        <key>DeviceIdentifier</key><string>disk3s1</string>
        <key>APFSVolumeUUID</key><string>` + testVolumeUUID + `</string>
        <key>Locked</key><false/>
      </dict>
      <dict>
        <key>DeviceIdentifier</key><string>disk3s2</string>
        <key>APFSVolumeUUID</key><string>98765678-2222-3333-4444-555555555555</string>
        <key>Locked</key><true/>
      </dict>
    </array>
  </dict>
  <dict>
    <key>APFSContainerUUID</key><string>FFFFDDDD-CCCC-DDDD-EEEE-123456700000</string>
    <key>Volumes</key><array>
      <dict>
        <key>DeviceIdentifier</key><string>disk8s1</string>
        <key>APFSVolumeUUID</key><string>32126567-1111-2222-3333-444444444444</string>
        <key>Locked</key><false/>
      </dict>
    </array>
  </dict>
</array></dict></plist>`

type nativeFixtureRunner struct {
	run     func(string, ...string) sysrun.Result
	missing string
}

func (r nativeFixtureRunner) LookPath(path string) (string, error) {
	if path == r.missing {
		return "", os.ErrNotExist
	}
	return path, nil
}

func (r nativeFixtureRunner) Run(_ context.Context, path string, args ...string) sysrun.Result {
	return r.run(path, args...)
}

func TestParseAPFSIdentitiesAndActiveContainer(t *testing.T) {
	volumes, err := parseAPFSVolumes([]byte(apfsFixture), []apfsMount{{"com.apple.os.update@/dev/disk3s1s1", "/", true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 3 || !volumes[0].ReadOnly || volumes[0].Mounts[0] != "/" || volumes[0].UUID != testVolumeUUID ||
		!volumes[1].Locked || volumes[1].Mounted() || !volumes[0].live || !volumes[1].live || volumes[2].live {
		t.Fatalf("unexpected outlines: %+v", volumes)
	}
	for _, data := range []string{
		`<plist><dict></dict></plist>`,
		strings.Replace(apfsFixture, "disk3s2", "disk3s1", 1),
		strings.Replace(apfsFixture, "disk3s2", "disk3s2s1", 1),
		strings.Replace(apfsFixture, "<key>Locked</key><false/>", "", 1),
	} {
		if _, err := parseAPFSVolumes([]byte(data), nil); err == nil {
			t.Fatal("accepted incomplete or ambiguous APFS identity")
		}
	}
	for _, mount := range []apfsMount{{"/dev/disk99s99", "/Volumes/unknown", false}, {"unknown", "/", true}} {
		if _, err := parseAPFSVolumes([]byte(apfsFixture), []apfsMount{mount}); err == nil {
			t.Fatalf("accepted unknown APFS mount: %+v", mount)
		}
	}
}

func TestNativeCheckResultEvidence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		code         int
		stdout       string
		stderr       string
		err          error
		live         bool
		wantState    string
		availability domain.Availability
	}{
		{"APFS offline", 0, "** The volume fixture with UUID 11111111-1111-1111-1111-111111111111 appears to be OK.", "", nil, false, "clean", domain.Available},
		{"APFS live", 0, "The volume appears to be OK.", "", nil, true, "no_errors_observed", domain.Partial},
		{"APFS warning", 0, "The volume appears to be OK.", "warning: doc-id tree record", nil, false, "review_required", domain.Partial},
		{"APFS skipped", 0, "Skipping encrypted metadata", "", nil, false, "review_required", domain.Partial},
		{"APFS check error", 8, "warning: partial data", "error: invalid inode", nil, true, "errors_reported", domain.Partial},
		{"APFS system permission", 8, "", "error: failed to open device: Operation not permitted", nil, false, "unknown", domain.PermissionRequired},
		{"APFS unknown", 8, "", "", nil, false, "inconclusive", domain.Error},
		{"APFS missing volume completion", 0, "The container appears to be OK.", "", nil, false, "inconclusive", domain.Unknown},
		{"APFS misleading volume name", 0, "** The volume fixture appears to be OK was not checked.", "", nil, false, "inconclusive", domain.Unknown},
		{"APFS cancellation", -1, "", "", context.Canceled, false, "cancelled", domain.Unknown},
		{"APFS timeout", -1, "", "", context.DeadlineExceeded, true, "inconclusive", domain.Unknown},
		{"NTFS observation", 0, "", "", nil, true, "no_errors_observed", domain.Partial},
		{"NTFS unexpected repair return", 1, "", "", nil, true, "inconclusive", domain.Partial},
		{"NTFS ambiguous cleanup return", 2, "", "", nil, true, "review_required", domain.Partial},
		{"NTFS failed or incomplete", 3, "", "", nil, true, "inconclusive", domain.Partial},
		{"NTFS unknown exit", 8, "", "", nil, true, "unknown", domain.Error},
		{"NTFS permission", -1, "", "", os.ErrPermission, true, "unknown", domain.PermissionRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := sysrun.Result{ExitCode: tc.code, Output: []byte(tc.stdout), Stderr: []byte(tc.stderr), Err: tc.err}
			var got CheckResult
			if strings.HasPrefix(tc.name, "APFS") {
				got = interpretAPFSCheck(raw, "/dev/disk3s1", tc.live)
			} else {
				got = interpretNTFSCheck(raw, "C:")
			}
			if got.Availability != tc.availability || got.State != tc.wantState || got.ExitCode != tc.code || got.Detail == "" {
				t.Fatalf("unexpected result: %+v", got)
			}
		})
	}
}

func TestCheckAPFSChoosesReadOnlyModeAndRevalidates(t *testing.T) {
	for _, tc := range []struct {
		name        string
		live        bool
		changed     bool
		wantState   string
		wantsSwitch []string
	}{
		{"offline", false, false, "clean", []string{"-n", "/dev/rdisk3s1"}},
		{"live", true, false, "no_errors_observed", []string{"-n", "-l", "/dev/rdisk3s1"}},
		{"mount changed", false, true, "target_changed", []string{"-n", "/dev/rdisk3s1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lists, checked := 0, 0
			runner := nativeFixtureRunner{run: func(path string, args ...string) sysrun.Result {
				switch path {
				case diskutilPath:
					if !slices.Equal(args, []string{"apfs", "list", "-plist"}) {
						t.Fatalf("unexpected diskutil arguments: %v", args)
					}
					lists++
					return sysrun.Result{Output: []byte(apfsFixture)}
				case fsckAPFSPath:
					checked++
					if !slices.Equal(args, tc.wantsSwitch) {
						t.Fatalf("unexpected fsck_apfs arguments: %v", args)
					}
					return sysrun.Result{Output: []byte("The volume appears to be OK.")}
				default:
					t.Fatalf("unexpected executable: %s", path)
					return sysrun.Result{}
				}
			}}
			mounts := func() ([]apfsMount, error) {
				if tc.live || tc.changed && lists == 2 {
					return []apfsMount{{"/dev/disk3s1s1", "/", true}}, nil
				}
				return nil, nil
			}
			got := checkAPFS(t.Context(), runner, "/dev/disk3s1", true, mounts)
			if got.State != tc.wantState || lists != 2 || checked != 1 {
				t.Fatalf("unexpected result %+v (inventories=%d checks=%d)", got, lists, checked)
			}
		})
	}
}

func TestCheckAPFSRejectsUnavailableTargets(t *testing.T) {
	for _, tc := range []struct {
		target, missing string
		elevated        bool
		want            domain.Availability
	}{
		{"/dev/disk3", "", true, domain.Unsupported},
		{"/dev/rdisk3s1", "", true, domain.Unsupported},
		{"/dev/disk3s1s1", "", true, domain.Unsupported},
		{"/dev/disk3s1", "", false, domain.PermissionRequired},
		{"/dev/disk3s2", "", true, domain.PermissionRequired},
		{"/dev/disk3s1", fsckAPFSPath, true, domain.DependencyMissing},
		{"/dev/disk3s1", diskutilPath, true, domain.DependencyMissing},
	} {
		t.Run(tc.target+"/"+tc.missing+"/"+string(tc.want), func(t *testing.T) {
			runner := nativeFixtureRunner{missing: tc.missing, run: func(path string, _ ...string) sysrun.Result {
				if path != diskutilPath {
					t.Fatalf("unexpected check: %s", path)
				}
				return sysrun.Result{Output: []byte(apfsFixture)}
			}}
			got := checkAPFS(t.Context(), runner, tc.target, tc.elevated, func() ([]apfsMount, error) { return nil, nil })
			if got.State != "not_started" || got.Availability != tc.want {
				t.Fatalf("unexpected result: %+v", got)
			}
		})
	}
}

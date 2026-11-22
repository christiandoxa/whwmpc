package filesystem

import (
	"os"
	"slices"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func TestCheckNTFSUsesStableVolumeWithoutSwitches(t *testing.T) {
	for _, changed := range []bool{false, true} {
		const path = `C:\Windows\System32\chkdsk.exe`
		probes, checks := 0, 0
		runner := nativeFixtureRunner{run: func(got string, args ...string) sysrun.Result {
			checks++
			if got != path || !slices.Equal(args, []string{`\\?\Volume{` + testVolumeUUID + `}`}) {
				t.Fatalf("invalid CHKDSK invocation %s %v", got, args)
			}
			return sysrun.Result{}
		}}
		probe := func(target string) (Volume, error) {
			probes++
			if target != "C:" {
				t.Fatalf("target was not normalized: %s", target)
			}
			id := testVolumeUUID
			if probes == 2 && changed {
				id = otherVolumeUUID
			}
			return Volume{Device: target, Kind: "volume", Filesystem: "ntfs", UUID: id, Mounts: []string{`C:\`}}, nil
		}
		got := checkNTFS(t.Context(), runner, "c:", path, true, probe)
		if (!changed && (got.Availability != domain.Partial || got.State != "no_errors_observed")) ||
			(changed && (got.Availability != domain.Unknown || got.State != "target_changed")) || probes != 2 || checks != 1 {
			t.Fatalf("unexpected result %+v (probes=%d checks=%d)", got, probes, checks)
		}
	}
}

func TestCheckNTFSRejectsUnsafeTargetsBeforeStart(t *testing.T) {
	const path = `C:\Windows\System32\chkdsk.exe`
	for _, tc := range []struct {
		target, id      string
		probeErr        error
		elevated, found bool
		want            domain.Availability
	}{
		{`C: /f`, "", nil, true, true, domain.Unsupported},
		{`\\?\Volume{` + testVolumeUUID + `}`, "", nil, true, true, domain.Unsupported},
		{`C:\`, "", nil, true, true, domain.Unsupported},
		{"C:", "", nil, false, true, domain.PermissionRequired},
		{"C:", "", errNotLocalNTFS, true, true, domain.Unsupported},
		{"C:", "", os.ErrPermission, true, true, domain.PermissionRequired},
		{"C:", "invalidGUID", nil, true, true, domain.Unknown},
		{"C:", testVolumeUUID, nil, true, false, domain.DependencyMissing},
	} {
		t.Run(tc.target+"/"+string(tc.want), func(t *testing.T) {
			runner := nativeFixtureRunner{run: func(path string, args ...string) sysrun.Result {
				t.Fatalf("unexpected subprocess: %s %v", path, args)
				return sysrun.Result{}
			}}
			if !tc.found {
				runner.missing = path
			}
			got := checkNTFS(t.Context(), runner, tc.target, path, tc.elevated, func(target string) (Volume, error) {
				return Volume{Device: target, Kind: "volume", Filesystem: "ntfs", UUID: tc.id}, tc.probeErr
			})
			if got.State != "not_started" || got.Availability != tc.want {
				t.Fatalf("unexpected result: %+v", got)
			}
		})
	}
}

func TestNativeFilesystemDiscovery(t *testing.T) {
	if os.Getenv("WHWMPC_TEST_NATIVE") != "1" {
		t.Skip("run on the native Windows CI host")
	}
	got := Discover(t.Context(), nil)
	if got.Backend != "chkdsk" || (got.Availability != domain.Available && got.Availability != domain.Partial) || len(got.Volumes) == 0 {
		t.Fatalf("NTFS inventory unavailable: %+v", got)
	}
	for _, v := range got.Volumes {
		if !v.CheckCandidate() || !v.Mounted() {
			t.Fatalf("invalid listed NTFS volume: %+v", v)
		}
	}
}

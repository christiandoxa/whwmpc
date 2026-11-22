package filesystem

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const testVolumeUUID = "11111111-1111-1111-1111-111111111111"

type fixtureRunner struct {
	result  sysrun.Result
	missing bool
}

func (r fixtureRunner) LookPath(name string) (string, error) {
	if r.missing {
		return "", os.ErrNotExist
	}
	return "/usr/bin/" + name, nil
}

func (r fixtureRunner) Run(_ context.Context, _ string, _ ...string) sysrun.Result { return r.result }

func TestDiscoverOfflineCandidates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux lsblk backend")
	}
	out := fmt.Sprintf(`{"blockdevices":[
{"name":"/dev/vdb1","maj:min":"8:17","type":"part","fstype":"ext4","ro":false,"uuid":%q,"mountpoints":[null],"size":4294967296},
{"name":"/dev/vda1","maj:min":"8:1","type":"part","fstype":"ext4","ro":false,"uuid":%q,"mountpoints":["/"],"size":4294967296},
{"name":"/dev/vdb2","maj:min":"8:18","type":"part","fstype":"xfs","ro":false,"mountpoints":[],"size":4096}
]}`, testVolumeUUID, testVolumeUUID)
	s := Discover(context.Background(), fixtureRunner{result: sysrun.Result{Output: []byte(out)}})
	if s.Availability != domain.Available || len(s.Volumes) != 3 {
		t.Fatalf("inventory: %+v", s)
	}
	if _, err := s.Select("/dev/vdb1"); err != nil {
		t.Fatalf("offline volume rejected: %v", err)
	}
	for _, target := range []string{"/dev/vda1", "/dev/vdb2", "/dev/disk/by-uuid/" + testVolumeUUID, "--bad"} {
		if _, err := s.Select(target); err == nil {
			t.Fatalf("ineligible target %s accepted", target)
		}
	}
	if bad := Discover(context.Background(), fixtureRunner{result: sysrun.Result{Output: []byte(`{}`)}}); bad.Availability != domain.Error {
		t.Fatal("a missing list appeared healthy")
	}
	if missing := Discover(context.Background(), fixtureRunner{missing: true}); missing.Availability != domain.DependencyMissing {
		t.Fatalf("missing lsblk: %v", missing.Availability)
	}
}

func TestReadOnlyResultAvailability(t *testing.T) {
	for _, tt := range []struct {
		code       int
		msg, state string
		status     domain.Availability
	}{
		{0, "", "clean", domain.Available},
		{0, "Pass 5: Free count wrong. Fix? no", "errors_detected", domain.Available},
		{4, "", "errors_detected", domain.Available},
		{8, "Permission denied", "unknown", domain.PermissionRequired},
		{0, "Warning: skipping journal recovery", "journal_recovery_needed", domain.Partial},
		{32, "", "unknown", domain.Error},
		{0, "Warning: drive is in use.", "mount_changed", domain.Unknown},
	} {
		got := Interpret(sysrun.Result{ExitCode: tt.code, Output: []byte(tt.msg)}, "/dev/vdb1")
		if got.State != tt.state || got.Availability != tt.status {
			t.Fatalf("incorrect state %+v expected %s %s", got, tt.state, tt.status)
		}
	}
	for _, mode := range []bool{false, true} {
		args := strings.Join(E2fsckArgs("/usr/sbin/e2fsck", "/proc/42/fd/8", mode), " ")
		if strings.Contains(args, " -y ") || strings.Contains(args, " -c ") || !strings.HasSuffix(args, " -- /proc/42/fd/8") ||
			(strings.Contains(args, " -f -n ") == mode) {
			t.Fatalf("unsafe argv %s", args)
		}
	}
}

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type fixtureRunner struct {
	admit []string
	run   func(string, ...string) sysrun.Result
}

func (r fixtureRunner) LookPath(name string) (string, error) {
	if !slices.Contains(r.admit, name) {
		return "", os.ErrNotExist
	}
	return "/usr/bin/" + name, nil
}

func (r fixtureRunner) Run(_ context.Context, file string, args ...string) sysrun.Result {
	if r.run == nil {
		return sysrun.Result{Err: errors.New("unexpected subprocess")}
	}
	return r.run(filepath.Base(file), args...)
}

func put(t *testing.T, root, path, value string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, root, path, target string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Dir(path), filepath.Join(root, target))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, path); err != nil {
		t.Fatal(err)
	}
}

func kernelDevice(t *testing.T, root, dir, majorMinor, maxDiscard string, whole, physical bool, backing ...string) {
	t.Helper()
	name := filepath.Base(dir)
	put(t, root, filepath.Join(dir, "dev"), majorMinor+"\n")
	put(t, root, filepath.Join(dir, "ro"), "0\n")
	if whole {
		put(t, root, filepath.Join(dir, "diskseq"), "734\n")
		put(t, root, filepath.Join(dir, "queue/discard_max_bytes"), maxDiscard+"\n")
		put(t, root, filepath.Join(dir, "queue/rotational"), "0\n")
		if err := os.MkdirAll(filepath.Join(root, dir, "slaves"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		put(t, root, filepath.Join(dir, "partition"), "1\n")
	}
	if physical {
		if err := os.MkdirAll(filepath.Join(root, dir, "device"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, child := range backing {
		link(t, root, filepath.Join(dir, "slaves", child), filepath.Join("/sys/class/block", child))
	}
	link(t, root, filepath.Join("/sys/class/block", name), dir)
}

func TestDiscardLVMRAIDAndMountScope(t *testing.T) {
	root := t.TempDir()
	kernelDevice(t, root, "/sys/devices/usb2/disk/sda", "8:0", "4096", true, true)
	kernelDevice(t, root, "/sys/devices/usb2/disk/sda/sda1", "8:1", "", false, false)
	kernelDevice(t, root, "/sys/devices/disk/sdb", "8:16", "0", true, true)
	kernelDevice(t, root, "/sys/devices/nvme/nvme9/nvme9n1", "259:0", "8192", true, true)
	kernelDevice(t, root, "/sys/devices/virtual/block/dm-0", "253:0", "4096", true, false, "sda1")
	kernelDevice(t, root, "/sys/devices/virtual/block/md0", "9:0", "4096", true, false, "sda1", "sdb")
	kernelDevice(t, root, "/sys/devices/virtual/block/dm-1", "253:1", "", true, false, "sda1")
	put(t, root, "/proc/self/mountinfo", `10 2 253:0 / / rw,relatime shared:1 - ext4 /dev/mapper/system rw,discard
11 10 9:0 / /mnt/raid rw - xfs /dev/md0 rw,nodiscard
12 10 253:1 / /mnt/private\040target rw - ext4 /dev/dm-1 rw
13 10 253:0 /branch /mnt/subdir rw - ext4 /dev/mapper/system rw
14 10 259:0 / /test-overlay rw - ext4 /dev/nvme9n1 rw
15 14 259:0 / /test-overlay/hides-me rw - ext4 /dev/nvme9n1 rw
16 14 0:9 / /test-overlay rw - tmpfs tmpfs rw
17 10 259:0 / /mnt/frozen rw - ext4 /dev/nvme9n1 ro
`)
	audit := (Collector{Root: root, Runner: fixtureRunner{admit: []string{"fstrim", "cron"}}}).InspectTrim(t.Context())
	if audit.Availability != domain.Partial || audit.Manual != domain.Available ||
		audit.Timer.Scheduler != "cron" || audit.Timer.Availability != domain.Unknown {
		t.Fatalf("inconsistent partial or non-systemd capability: %+v", audit)
	}
	want := map[uint64]domain.Availability{
		10: domain.Available, 11: domain.Unsupported, 12: domain.Unknown, 13: domain.Partial,
		14: domain.Unknown, 15: domain.Unknown, 17: domain.Unsupported,
	}
	if len(audit.Mounts) != len(want) {
		t.Fatalf("mounts: %+v", audit.Mounts)
	}
	for _, m := range audit.Mounts {
		if m.Discard != want[m.ID] {
			t.Fatalf("unexpected mount scope: %+v", m)
		}
	}
	dm := audit.Mounts[0]
	if dm.DiscardMode != "discard" || len(dm.Layers) != 3 ||
		dm.Layers[0].Name != "dm-0" || dm.Layers[1].Name != "sda1" || dm.Layers[2].Name != "sda" ||
		dm.Layers[1].DiskSequence != "734" || audit.Mounts[2].Point != "/mnt/private target" {
		t.Fatalf("lost topology or mount details: %+v", audit.Mounts)
	}
	data, err := json.Marshal(audit)
	if err != nil || strings.Contains(string(data), "private") || strings.Contains(string(data), "/dev/") {
		t.Fatalf("unredacted paths: %s (%v)", data, err)
	}
}

func TestBtrfsIncludesAllBackingDevices(t *testing.T) {
	root := t.TempDir()
	kernelDevice(t, root, "/sys/devices/block/sda", "8:0", "4096", true, true)
	kernelDevice(t, root, "/sys/devices/block/sdb", "8:16", "4096", true, true)
	group := "/sys/fs/btrfs/5a17e094-a1ee-4c47-af11-7ebcf5304bd7"
	link(t, root, group+"/devices/sda", "/sys/class/block/sda")
	link(t, root, group+"/devices/sdb", "/sys/class/block/sdb")
	put(t, root, group+"/devinfo/1/missing", "0\n")
	put(t, root, group+"/devinfo/2/missing", "0\n")
	put(t, root, "/proc/self/mountinfo", "7 2 0:20 / / rw - btrfs /dev/sda rw,discard=async\n")
	c := Collector{Root: root, Runner: fixtureRunner{}}
	audit := c.InspectTrim(t.Context())
	if len(audit.Mounts) != 1 || audit.Mounts[0].Discard != domain.Available ||
		audit.Mounts[0].DiscardMode != "discard=async" || len(audit.Mounts[0].Layers) != 2 {
		t.Fatalf("Btrfs multi-device topology: %+v", audit)
	}
	put(t, root, group+"/devinfo/2/missing", "1\n")
	degraded := c.InspectTrim(t.Context())
	if degraded.Mounts[0].Discard != domain.Unknown {
		t.Fatalf("missing Btrfs device considered covered: %+v", degraded.Mounts[0])
	}
}

func TestMountInfoRejectsIncompleteIdentity(t *testing.T) {
	root := t.TempDir()
	kernelDevice(t, root, "/sys/devices/block/sda", "8:0", "4096", true, true)
	put(t, root, "/proc/self/mountinfo", "1 2 8:0 / /mnt/A\u00a0B\\134040 rw - ext4 /dev/sda rw\nwrong record\n")
	audit := (Collector{Root: root, Runner: fixtureRunner{}}).InspectTrim(t.Context())
	if audit.Availability != domain.Partial || len(audit.Mounts) != 1 ||
		audit.Mounts[0].Discard != domain.Unknown || audit.Mounts[0].Point != "/mnt/A\u00a0B\\040" {
		t.Fatalf("incomplete mountinfo treated as safe: %+v", audit)
	}
}

func TestSMARTDeviceMappingAndHotplug(t *testing.T) {
	root := t.TempDir()
	kernelDevice(t, root, "/sys/devices/nvme/nvme42/nvme42n1", "259:0", "4096", true, true)
	kernelDevice(t, root, "/sys/devices/nvme/nvme42/nvme42n1/nvme42n1p1", "259:1", "", false, false)
	kernelDevice(t, root, "/sys/devices/controller/usb2/sdX", "8:0", "0", true, true)
	put(t, root, "/proc/self/mountinfo", "12 2 259:1 / / rw - ext4 /dev/nvme42n1p1 rw\n")
	fixture, err := os.ReadFile("testdata/nvme.json")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1770000000, 0).UTC()
	check := Collector{Root: root, Now: func() time.Time { return at }}
	check.Runner = fixtureRunner{admit: []string{"smartctl"}, run: func(file string, args ...string) sysrun.Result {
		if file != "smartctl" || !slices.Contains(args, "--quietmode=noserial") {
			t.Fatalf("unexpected subprocess: %s %q", file, args)
		}
		switch args[len(args)-1] {
		case "/dev/nvme42n1":
			if !slices.Contains(args, "--device=nvme") {
				t.Fatalf("NVMe device type: %q", args)
			}
			return sysrun.Result{Output: fixture, ExitCode: SMARTDiskFailing}
		case "/dev/sdX":
			return sysrun.Result{Output: []byte(`{"smartctl":{"exit_status":2,"messages":[{"string":"Permission denied"}]}}`), ExitCode: 2}
		default:
			t.Fatalf("unmapped device probed: %q", args)
			return sysrun.Result{}
		}
	}}
	audit := check.InspectSMART(t.Context())
	if audit.Availability != domain.Partial || len(audit.Drives) != 2 || audit.CapturedAt != at ||
		!slices.Equal(audit.Drives[0].MountIDs, []uint64{12}) || audit.Drives[0].Availability != domain.Available ||
		audit.Drives[1].Availability != domain.PermissionRequired || audit.Drives[1].Transport != "USB" {
		t.Fatalf("unexpected audit: %+v", audit)
	}
	check.Runner = fixtureRunner{admit: []string{"smartctl"}, run: func(file string, args ...string) sysrun.Result {
		put(t, root, "/sys/class/block/nvme42n1/diskseq", "735\n")
		return sysrun.Result{Output: fixture, ExitCode: SMARTDiskFailing}
	}}
	audit = check.InspectSMART(t.Context())
	if audit.Drives[0].Availability != domain.Stale || audit.Drives[0].Health != nil {
		t.Fatalf("health accepted after disk changed: %+v", audit.Drives[0])
	}
}

func TestSystemdStatesAndPerMountJournalEvidence(t *testing.T) {
	root := t.TempDir()
	kernelDevice(t, root, "/sys/devices/block/sda", "8:0", "4096", true, true)
	kernelDevice(t, root, "/sys/devices/block/sdb", "8:16", "4096", true, true)
	put(t, root, "/run/systemd/system/.fixture", "")
	put(t, root, "/proc/self/mountinfo", "10 2 8:0 / / rw - ext4 /dev/sda rw\n11 10 8:16 / /mnt/local rw - xfs /dev/sdb rw\n")
	c := Collector{Root: root, Runner: fixtureRunner{admit: []string{"systemctl", "journalctl", "fstrim"}, run: func(file string, args ...string) sysrun.Result {
		switch {
		case file == "systemctl" && args[len(args)-1] == "fstrim.timer":
			return sysrun.Result{Output: []byte("LoadState=loaded\nUnitFileState=disabled\nActiveState=active\nSubState=waiting\nUnit=fstrim.service\nNextElapseUSecRealtime=Mon 2026-05-04 00:00:00 UTC\nLastTriggerUSec=Sun 2026-05-03 00:00:00 UTC\n")}
		case file == "systemctl" && args[len(args)-1] == "fstrim.service":
			return sysrun.Result{Output: []byte("LoadState=loaded\nActiveState=inactive\nResult=exit-code\nExecMainStatus=64\nExecMainStartTimestamp=Sun 2026-05-03 00:00:00 UTC\nExecMainExitTimestamp=Sun 2026-05-03 00:00:10 UTC\nInvocationID=00000000000000000000000000000042\n")}
		case file == "journalctl":
			if !slices.Contains(args, "_SYSTEMD_INVOCATION_ID=00000000000000000000000000000042") ||
				!slices.Contains(args, "_UID=0") {
				t.Fatalf("unscoped journal query: %q", args)
			}
			return sysrun.Result{Output: []byte(`{"MESSAGE":"/: 4 KiB (4096 bytes) trimmed on /dev/sda","__REALTIME_TIMESTAMP":"1777777777000000"}
{"MESSAGE":"/mnt/local: 8 KiB (8192 bytes) trimmed on /dev/sda","__REALTIME_TIMESTAMP":"1777777777000000"}
{"MESSAGE":"/mnt/local: 0 B (dry run) trimmed on /dev/sdb","__REALTIME_TIMESTAMP":"1777777777000001"}
`)}
		default:
			t.Fatalf("unexpected subprocess: %s %q", file, args)
			return sysrun.Result{}
		}
	}}}
	audit := c.InspectTrim(t.Context())
	if audit.Availability != domain.Available || audit.Timer.Journal != domain.Available ||
		audit.Timer.UnitFileState != "disabled" || audit.Timer.ActiveState != "active" ||
		audit.Timer.NextElapse == "" || audit.Timer.LastTrigger == "" || audit.Timer.LastServiceExit == "" ||
		audit.Timer.ServiceResult != "exit-code" || audit.Timer.ServiceExitCode == nil || *audit.Timer.ServiceExitCode != 64 {
		t.Fatalf("timer and service proof: %+v", audit.Timer)
	}
	if audit.Mounts[0].LastTrim == nil || audit.Mounts[0].LastTrim.PotentialDiscardBytes != 4096 ||
		audit.Mounts[1].LastTrim != nil {
		t.Fatalf("journal evidence matched the wrong filesystem: %+v", audit.Mounts)
	}
}

//go:build linux || darwin

package privread

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/firmware"
)

var (
	diskName  = regexp.MustCompile(`^disk[0-9]{1,4}$`)
	blockName = regexp.MustCompile(`^/dev/[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
	trimLogID = regexp.MustCompile(`^_SYSTEMD_INVOCATION_ID=[0-9a-f]{32}$`)
	busAddr   = regexp.MustCompile(`^--address=unix:path=/run/dbus/system_bus_socket(,guid=[0-9a-f]{32})?$`)
)

func readArgs(op Operation, file string, a []string) bool {
	switch filepath.Base(file) {
	case "smartctl":
		options := []string{"--json=v", "--info", "--health", "--attributes", "--quietmode=noserial"}
		if op != SMART || len(a) < 6 || len(a) > 7 || !slices.Equal(a[:len(options)], options) {
			return false
		}
		if len(a) == 7 && a[5] != "--device=nvme" && a[5] != "--nocheck=standby" {
			return false
		}
		path := a[len(a)-1]
		if !blockName.MatchString(path) {
			return false
		}
		block, err := os.Lstat(path)
		_, sysErr := os.Stat(filepath.Join("/sys/class/block", filepath.Base(path), "dev"))
		return err == nil && sysErr == nil && block.Mode()&(os.ModeDevice|os.ModeCharDevice) == os.ModeDevice
	case "journalctl":
		prefix := []string{"--quiet", "--no-pager", "--output=json", "--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "--lines=128", "_SYSTEMD_UNIT=fstrim.service", "_UID=0", "_TRANSPORT=stdout"}
		return op == TRIM && len(a) == len(prefix)+1 && slices.Equal(a[:len(prefix)], prefix) && trimLogID.MatchString(a[len(prefix)])
	case "systemctl":
		if op == TRIM {
			return len(a) == 4 && a[0] == "--no-pager" && a[1] == "show" && strings.HasPrefix(a[2], "--property=") && (a[3] == "fstrim.timer" || a[3] == "fstrim.service")
		}
		return op == Power && slices.Equal(a, []string{"show", "--no-pager", "--property=Id,LoadState,ActiveState,MainPID", "--",
			"power-profiles-daemon.service", "tlp.service", "tlp-pd.service", "tuned.service", "tuned-ppd.service", "auto-cpufreq.service", "snap.auto-cpufreq.service.service"})
	case "busctl":
		if op != Power || len(a) < 9 || !busAddr.MatchString(a[0]) || !slices.Equal(a[1:5], []string{"--auto-start=no", "--allow-interactive-authorization=no", "--json=short", "call"}) {
			return false
		}
		return slices.Contains([]string{"GetId", "GetNameOwner", "GetConnectionUnixProcessID", "GetAll", "Get"}, a[8])
	case "nvidia-smi":
		return op == GPU && len(a) == 2 && strings.HasPrefix(a[0], "--query-gpu=") && a[1] == "--format=csv,noheader,nounits"
	case "fwupdmgr":
		return op == Firmware && (slices.Equal(a, firmware.Args("get-devices")) || slices.Equal(a, firmware.Args("--version")))
	case "lsblk":
		return op == Filesystem && slices.Equal(a, []string{"--json", "--paths", "--list", "--bytes", "--output", "NAME,MAJ:MIN,TYPE,FSTYPE,UUID,RO,MOUNTPOINTS,SIZE"})
	case "diskutil":
		if op == Filesystem {
			return slices.Equal(a, []string{"apfs", "list", "-plist"})
		}
		return op == SMART && (slices.Equal(a, []string{"list", "-plist", "physical"}) || len(a) == 3 && a[0] == "info" && a[1] == "-plist" && diskName.MatchString(a[2]))
	case "system_profiler":
		return op == TRIM && slices.Equal(a, []string{"-json", "-detailLevel", "full", "SPNVMeDataType", "SPSerialATADataType"})
	default:
		return false
	}
}

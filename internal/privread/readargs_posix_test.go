//go:build linux || darwin

package privread

import "testing"

func TestToolArgumentsRemainReadOnly(t *testing.T) {
	for _, tc := range []struct {
		op   Operation
		tool string
		args []string
	}{
		{TRIM, "fstrim", []string{"-v", "/"}},
		{TRIM, "systemctl", []string{"enable", "fstrim.timer"}},
		{Filesystem, "diskutil", []string{"repairVolume", "disk0"}},
		{SMART, "diskutil", []string{"info", "-plist", "/etc/shadow"}},
		{SMART, "smartctl", []string{"--json=v", "--info", "--health", "--attributes", "--quietmode=noserial", "../../etc/shadow"}},
		{Firmware, "fwupdmgr", []string{"update", "x"}},
		{GPU, "nvidia-smi", []string{"--power-limit=300"}},
		{Power, "busctl", []string{"--address=unix:path=/tmp/fake", "--auto-start=no", "--allow-interactive-authorization=no", "--json=short", "call", "a", "b", "c", "Get"}},
	} {
		if readArgs(tc.op, tc.tool, tc.args) {
			t.Fatalf("unsafe command accepted: %s %q", tc.tool, tc.args)
		}
	}
	if !readArgs(TRIM, "systemctl", []string{"--no-pager", "show", "--property=ActiveState", "fstrim.timer"}) {
		t.Fatal("valid timer query was refused")
	}
}

package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func TestMountedOrUnreadableCannotProceed(t *testing.T) {
	const mount = "46 32 8:1 / / rw,relatime - ext4 /dev/vda1 rw\n"
	for _, tt := range []struct {
		text  string
		valid bool
	}{
		{mount, true},
		{mount + "47 32 8:17 / /mnt rw - ext4 /dev/vdb1 rw\n", false},
		{mount + "partial mount descriptor\n", false},
		{"", false},
	} {
		if err := parseMounts(strings.NewReader(tt.text), "8:17"); (err == nil) != tt.valid {
			t.Fatalf("mount gate %q: %v", tt.text, err)
		}
	}
}

func TestDirectProbeRejectsChangedIdentity(t *testing.T) {
	v := Volume{UUID: testVolumeUUID, Filesystem: "ext4"}
	for _, tt := range []struct {
		data string
		ok   bool
	}{
		{"UUID=" + testVolumeUUID + "\nTYPE=ext4\n", true},
		{"UUID=" + testVolumeUUID + "\nTYPE=ext4\nTYPE=ext4\n", false},
		{"UUID=" + testVolumeUUID + "\nTYPE=xfs\n", false},
		{"UUID=01234567-aaaa-aaaa-aaaa-abcdef123456\nTYPE=ext4\n", false},
	} {
		r := fixtureRunner{result: sysrun.Result{Output: []byte(tt.data)}}
		if err := Probe(context.Background(), r, v, "/proc/42/fd/8"); (err == nil) != tt.ok {
			t.Fatalf("probe %q: %v", tt.data, err)
		}
	}
}

// Only the disposable regular file created here is formatted or modified.
func TestOfflineToolsOnTemporaryExt4Image(t *testing.T) {
	native := sysrun.Native{}
	tools := make(map[string]string)
	for _, name := range []string{"mke2fs", "debugfs", "e2fsck", "env"} {
		p, err := native.LookPath(name)
		if err != nil {
			t.Skipf("%s not installed", name)
		}
		tools[name] = p
	}
	img := filepath.Join(t.TempDir(), "volume.img")
	file, err := os.OpenFile(img, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(24 << 20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := func(name string, code int, args ...string) sysrun.Result {
		t.Helper()
		result := native.Run(ctx, tools[name], args...)
		if result.Err != nil || result.ExitCode != code {
			t.Fatalf("%s exit %d error %v\n%s\n%s", name, result.ExitCode, result.Err, result.Output, result.Stderr)
		}
		return result
	}
	run("mke2fs", 0, "-q", "-t", "ext4", "-F", img)
	held, err := os.Open(img)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	operand := Operand(held)
	hash := func() []byte {
		t.Helper()
		f, err := os.Open(img)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, f); err != nil {
			t.Fatal(err)
		}
		return sum.Sum(nil)
	}
	before := hash()
	clean := Interpret(run("env", 0, E2fsckArgs(tools["e2fsck"], operand, false)...), img)
	if clean.State != "clean" || !bytes.Equal(before, hash()) {
		t.Fatal("clean read-only check did not preserve image")
	}
	run("debugfs", 0, "-w", "-R", "set_inode_field <2> links_count 8", img)
	run("debugfs", 0, "-w", "-R", "set_super_value state 2", img)
	before = hash()
	errors := Interpret(run("env", 4, E2fsckArgs(tools["e2fsck"], operand, false)...), img)
	if errors.State != "errors_detected" || !bytes.Equal(before, hash()) {
		t.Fatal("read-only diagnosis did not preserve known inconsistency")
	}
	run("env", 1, E2fsckArgs(tools["e2fsck"], operand, true)...)
	if bytes.Equal(before, hash()) {
		t.Fatal("repair did not change the corrupted image")
	}
	if after := Interpret(run("env", 0, E2fsckArgs(tools["e2fsck"], operand, false)...), img); after.State != "clean" {
		t.Fatalf("verification: %+v", after)
	}
}

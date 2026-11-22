package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type firmwarePowerRunner map[string]string

func (r firmwarePowerRunner) LookPath(string) (string, error) { return "/usr/bin/busctl", nil }

func (r firmwarePowerRunner) Run(_ context.Context, _ string, args ...string) sysrun.Result {
	if args[len(args)-1] == "IsPresent" && args[len(args)-2] != "org.freedesktop.UPower.Device" {
		return sysrun.Result{Err: fmt.Errorf("wrong DisplayDevice interface")}
	}
	value, ok := r[args[len(args)-1]]
	if !ok {
		return sysrun.Result{ExitCode: 1}
	}
	return sysrun.Result{Output: []byte(value)}
}

func TestFirmwarePowerRequiresACAndAdmissibleCharge(t *testing.T) {
	for _, tt := range []struct {
		charge, threshold, onBattery, hasBattery string
		eligible                                 bool
	}{
		{"u 95", "u 40", "b false", "b true", true},
		{"u 101", "u 40", "b false", "b false", true},
		{"u 101", "u 101", "b false", "b false", true},
		{"u 39", "u 40", "b false", "b true", false},
		{"u 101", "u 40", "b false", "b true", false},
		{"u 101", "u 40", "b false", "", false},
		{"u 101", "u 101", "b false", "b true", false},
		{"u 101", "u 101", "b false", "", false},
		{"u 95", "u 40", "b true", "b true", false},
	} {
		host := linuxPlatform{runner: firmwarePowerRunner{
			"BatteryLevel": tt.charge, "BatteryThreshold": tt.threshold, "OnBattery": tt.onBattery, "IsPresent": tt.hasBattery,
		}}
		if err := checkFirmwarePower(context.Background(), host); (err == nil) != tt.eligible {
			t.Fatalf("power gate %+v: %v", tt, err)
		}
	}
}

func TestFirmwareCacheDigestRequiresTheReportedSnapshot(t *testing.T) {
	dir := t.TempDir()
	metadata := filepath.Join(dir, "firmware.xml.zst")
	signature := metadata + ".jcat"
	stamp := time.Unix(1_700_000_000, 0)
	contents := []byte("fixture jcat")
	for _, path := range []string{metadata, signature} {
		if err := os.WriteFile(path, contents, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	metadataInfo, err := os.Lstat(metadata)
	if err != nil {
		t.Fatal(err)
	}
	signatureInfo, err := os.Lstat(signature)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	if got := readFirmwareCacheDigest(metadata, signature, stamp.Unix(), metadataInfo, signatureInfo); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected signature digest %q", got)
	}
	if got := readFirmwareCacheDigest(metadata, signature, stamp.Unix()+1, metadataInfo, signatureInfo); got != "" {
		t.Fatal("accepted a mismatch with the daemon's metadata mtime")
	}
	if err := os.Chtimes(metadata, stamp, stamp.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := readFirmwareCacheDigest(metadata, signature, stamp.Unix(), metadataInfo, signatureInfo); got != "" {
		t.Fatal("accepted metadata changed after the initial check")
	}
	if err := os.Chtimes(metadata, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	metadataInfo, err = os.Lstat(metadata)
	if err != nil {
		t.Fatal(err)
	}
	oldSignature := signature + ".old"
	if err := os.Rename(signature, oldSignature); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldSignature, signature); err != nil {
		t.Fatal(err)
	}
	if got := readFirmwareCacheDigest(metadata, signature, stamp.Unix(), metadataInfo, signatureInfo); got != "" {
		t.Fatal("accepted a replaced signature path")
	}
}

func TestFirmwareCacheDigestRejectsOtherPaths(t *testing.T) {
	metadata := "/var/lib/fwupd/metadata/lvfs/firmware.xml.zst"
	for _, remote := range []firmware.Remote{
		{MetadataURI: "https://cdn.fwupd.org/downloads/firmware.xml.zst", FilenameCache: metadata, FilenameCacheSig: "/tmp/fixture.jcat"},
		{MetadataURI: "https://cdn.fwupd.org/downloads/firmware.xml.gz", FilenameCache: metadata, FilenameCacheSig: metadata + ".jcat"},
	} {
		if firmwareCacheDigest(remote) != "" {
			t.Fatal("accepted a path outside the selected remote's standard cache")
		}
	}
}

func TestFilesystemRepairRequiresReadOnlyConfirmation(t *testing.T) {
	for _, tt := range []struct {
		exit int
		read string
		want Status
	}{
		{1, "clean", Verified},
		{2, "clean", Partial},
		{4, "errors_detected", Partial},
		{8, "clean", Unknown},
		{1, "unknown", Unknown},
	} {
		if got := filesystemResult(tt.exit, filesystem.CheckResult{State: tt.read}, attempt{}); got.Status != tt.want {
			t.Fatalf("repair verification %+v produced %+v", tt, got)
		}
	}
}

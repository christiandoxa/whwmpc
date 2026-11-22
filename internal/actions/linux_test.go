//go:build linux

package actions

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
)

func TestAPTConfigRejectsUnsafeLocations(t *testing.T) {
	const fixture = "CACHE='/var/cache/apt/'\nARCHIVES='/var/cache/apt/machine/archives/'\nSTATE='/var/lib/apt/'\n" +
		"LISTS='/var/lib/apt/lists/'\nPKG='/var/cache/apt/pkgcache.bin'\nSRC=''\nSANDBOX='root'\n"
	layout, err := parseAPTLayout(fixture)
	if err != nil || layout.Archives != "/var/cache/apt/machine/archives" || layout.SrcPkgCache != "" {
		t.Fatalf("layout %+v, err %v", layout, err)
	}
	argv := strings.Join(layout.operation(), " ")
	if !strings.HasPrefix(argv, "-o RootDir= ") ||
		!strings.Contains(argv, "Debug::NoLocking=false") || !strings.Contains(argv, "Debug::SetupAPTPartialDirectory::AssumeGood=true") ||
		!strings.Contains(argv, "Dir::Cache::archives="+layout.Archives) ||
		!strings.Contains(argv, "Dir::State::lists="+layout.Lists) || !strings.HasSuffix(argv, " clean") {
		t.Fatalf("unexpected APT invocation %q", argv)
	}
	for _, path := range []string{"/home/user", "/var/cache/apt/../other", "/var/cache/apt", "/var/cache/apt/x\x1b"} {
		input := strings.Replace(fixture, "/var/cache/apt/machine/archives", path, 1)
		if _, err := parseAPTLayout(input); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	for _, extra := range []string{"ROOT='/alt/'\n", "BINROOT='/alt/'\n", "BINARCHIVES='/var/cache/apt/other/'\n", "BINPKG=''\n", "PKG='/var/cache/apt/fake.bin'\n"} {
		if _, err := parseAPTLayout(fixture + extra); err == nil {
			t.Fatalf("accepted extra setting %q", extra)
		}
	}
}

func aptFixture(t *testing.T) aptLayout {
	t.Helper()
	root := t.TempDir()
	layout := aptLayout{Cache: filepath.Join(root, "cache"), State: filepath.Join(root, "state"), SandboxUID: uint32(os.Getuid())}
	layout.Archives = filepath.Join(layout.Cache, "archives")
	layout.Lists = filepath.Join(layout.State, "lists")
	layout.PkgCache = filepath.Join(layout.Cache, "pkgcache.bin")
	for _, path := range []string{filepath.Join(layout.Archives, "partial"), filepath.Join(layout.Lists, "partial"), filepath.Join(layout.Lists, "auxfiles")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return layout
}

func fixtureGuard(path string, uid uint32, optional bool) (os.FileInfo, error) {
	if !safePath(path) {
		return nil, errors.New("invalid path")
	}
	info, err := os.Lstat(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err == nil && (identity(info).UID != uid || info.Mode()&(os.ModeSymlink|0022) != 0) {
		err = errors.New("untrusted path")
	}
	return info, err
}

func putFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAPTInventoryScopeAndMeasuredRemoval(t *testing.T) {
	layout := aptFixture(t)
	paths := []string{
		filepath.Join(layout.Archives, "cache.deb"),
		filepath.Join(layout.Archives, "partial", "pending.deb"),
		filepath.Join(layout.Lists, "partial", "pending.index"),
		layout.PkgCache, layout.PkgCache + ".tmp",
	}
	keep := []string{
		filepath.Join(layout.Archives, "lock"),
		filepath.Join(layout.Lists, "lock"),
		filepath.Join(layout.Lists, "live-index"),
		filepath.Join(layout.Lists, "auxfiles", "keep"),
	}
	for _, path := range append(paths, keep...) {
		putFile(t, path, "12345")
	}
	before, err := layout.readInventory(t.Context(), fixtureGuard, layout.SandboxUID)
	if err != nil || before.KnownFiles != len(paths) || before.KnownBytes != uint64(5*len(paths)) || len(before.Scope) != 5 {
		t.Fatalf("inventory %+v, err %v", before, err)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	after, err := layout.readInventory(t.Context(), fixtureGuard, layout.SandboxUID)
	result := verifyAPT(before, after, attempt{}, err)
	if result.Status != Verified || result.RemovedBytes == nil || *result.RemovedBytes != before.KnownBytes {
		t.Fatalf("result %+v, err %v", result, err)
	}
	for _, path := range keep {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "12345" {
			t.Fatalf("unexpected scope for %s: %v", filepath.Base(path), err)
		}
	}
	after.Unknown = []string{filepath.Join(layout.Lists, "partial")}
	result = verifyAPT(before, after, attempt{}, nil)
	if result.Status != Partial || result.RemovedBytes == nil || *result.RemovedBytes != before.KnownBytes {
		t.Fatalf("unobservable private scope was treated as verified: %+v", result)
	}
}

func TestAPTSymlinkAndDirectoryReplacement(t *testing.T) {
	t.Run("entry", func(t *testing.T) {
		layout := aptFixture(t)
		external := filepath.Join(t.TempDir(), "external")
		putFile(t, external, "preserve me")
		if err := os.Symlink(external, filepath.Join(layout.Archives, "evil.deb")); err != nil {
			t.Fatal(err)
		}
		if _, err := layout.readInventory(t.Context(), fixtureGuard, layout.SandboxUID); err == nil {
			t.Fatal("accepted symlink in native deletion scope")
		}
	})
	t.Run("directory", func(t *testing.T) {
		layout := aptFixture(t)
		external := t.TempDir()
		matches := 0
		guard := func(path string, owner uint32, optional bool) (os.FileInfo, error) {
			info, err := fixtureGuard(path, owner, optional)
			if path == layout.Archives {
				matches++
				if matches == 2 {
					if err := os.Rename(path, path+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(external, path); err != nil {
						t.Fatal(err)
					}
				}
			}
			return info, err
		}
		if _, err := layout.readInventory(t.Context(), guard, layout.SandboxUID); err == nil {
			t.Fatal("accepted replacement directory")
		}
	})
}

func TestTrimOutputAndVerification(t *testing.T) {
	path := "/proc/118/fd/34"
	value, found := trimCount(path, path+": 18446744073709551615 bytes trimmed\n")
	if !found || value != math.MaxUint64 {
		t.Fatalf("value %d, found %v", value, found)
	}
	for _, output := range []string{
		"/proc/118/fd/35: 1 bytes trimmed",
		path + ": 0 B (dry run) trimmed",
		path + ": 18446744073709551616 bytes trimmed",
		path + ": 1 bytes trimmed\n/other: 0 bytes trimmed",
		path + ": 1 GiB (1 bytes) trimmed",
	} {
		if _, found := trimCount(path, output); found {
			t.Fatalf("accepted invalid fstrim result %q", output)
		}
	}
	timer := "[Timer]\nOnCalendar=weekly\nPersistent=true\n[Install]\nWantedBy=timers.target\n"
	if !standardTimer(timer) || standardTimer(timer+"Also=other.timer\n") {
		t.Fatal("incorrectly assessed timer scope")
	}
	service := "[Service]\nType=oneshot\nExecStart=/sbin/fstrim --fstab --verbose --quiet-unsupported\n"
	command, supported := standardService(service)
	if !supported || !loadedServiceMatches("{ path=/sbin/fstrim ; argv[]="+command+" ; ignore_errors=no ; start_time=[n/a] }", command) {
		t.Fatal("rejected vendor fstrim service")
	}
	if _, supported := standardService(service + "ExecStartPost=/sbin/fstrim --all\n"); supported {
		t.Fatal("accepted extra executable scope")
	}
	before := timerState{Enabled: "disabled", Active: "inactive", Definition: "same"}
	want := timerState{Enabled: "enabled", Active: "active", Definition: "same"}
	after := timerState{Enabled: "enabled", Active: "inactive", Definition: "same"}
	result := verifyTimer(before, want, after, nil, "", attempt{err: errors.New("start failed")})
	if result.Status != Partial || result.Changed == nil || !*result.Changed {
		t.Fatalf("missed partial enablement: %+v", result)
	}
}

func TestManualTrimDescriptorBindsOnlyRuntimePIDAndFD(t *testing.T) {
	const tool, sudo = "/usr/bin/busybox", "/usr/bin/sudo"
	const target = "/media/selected"
	approved := []string{sudo, "--non-interactive", "--", tool, "fstrim", "-v", "--", descriptorOperand}
	inv := invocation{spec: approved, exe: executable{path: tool}, sudo: executable{path: sudo}, privilege: Sudo}
	executable, reference, err := descriptorInvocation(inv, 118, 34)
	if err != nil || reference != "/proc/118/fd/34" ||
		!slices.Equal(executable.spec, append(slices.Clone(approved[:len(approved)-1]), reference)) ||
		approved[len(approved)-1] != descriptorOperand {
		t.Fatalf("invocation %v, ref %q, err %v", executable.spec, reference, err)
	}
	for _, invalid := range []struct {
		pid int
		fd  uintptr
	}{{118, 0}, {118, ^uintptr(0)}, {-1, 34}} {
		if _, _, err := descriptorInvocation(inv, invalid.pid, invalid.fd); err == nil {
			t.Fatal("accepted invalid descriptor coordinates")
		}
	}
	s, now := fixtureService(&fakePlatform{})
	plan := Plan{Version: planVersion, Kind: TrimMount, Target: target, Operation: approved,
		CreatedAt: *now, ExpiresAt: now.Add(planTTL), Nonce: strings.Repeat("0", 24)}
	plan.ID = s.sign("plan", plan)
	if err := s.Validate(plan, plan.ID); err != nil {
		t.Fatal(err)
	}
	plan.Operation = executable.spec
	if err := s.Validate(plan, plan.ID); err == nil {
		t.Fatal("actual descriptor replaced the stable signed template in saved plan")
	}
	inv.spec = append(slices.Clone(approved[:len(approved)-1]), target)
	if _, _, err := descriptorInvocation(inv, 118, 34); err == nil {
		t.Fatal("accepted a pathname fallback")
	}
}

func TestDescriptorReferenceRetainsOpenedDirectoryAfterReplacement(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "storage")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	pinned, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	operand, err := descriptorPath(os.Getpid(), pinned.Fd())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkDescriptorReference(pinned, operand); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(operand)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Stat(path)
	if err != nil || os.SameFile(original, replacement) {
		t.Fatalf("descriptor was redirected to the replacement directory, err %v", err)
	}
}

func TestBusyBoxTrimHelpRequiresAuditedApplet(t *testing.T) {
	const body = " (distribution package) multi-call binary.\n\nUsage: fstrim [OPTIONS] MOUNTPOINT\n"
	for _, version := range []string{"v1.35.0", "v1.36.1", "v1.37.0"} {
		if !busyboxTrimHelp("BusyBox " + version + body) {
			t.Fatalf("did not detect supported %s applet", version)
		}
	}
	for _, output := range []string{"fstrim: applet not found", "BusyBox v1.38.0" + body, "BusyBox v1.36.1\nUsage: dd [OPERANDS]"} {
		if busyboxTrimHelp(output) {
			t.Fatalf("accepted unsupported applet output %q", output)
		}
	}
}

func TestPowerVerificationDistinguishesDeniedAndChanged(t *testing.T) {
	before := power.Profiles{Status: domain.Available, Target: "one", Active: "balanced"}
	step := attempt{err: errors.New("polkit denied")}
	if result := verifyProfile(before, before, "power-saver", "", step); result.Status != Failed || result.Changed == nil || *result.Changed {
		t.Fatalf("could not distinguish denied: %+v", result)
	}
	after := before
	after.Active = "power-saver"
	step.stopped = true
	if result := verifyProfile(before, after, "power-saver", "", step); result.Status != Partial || result.Changed == nil || !*result.Changed {
		t.Fatalf("did not report observed change after cancel: %+v", result)
	}
	after.Target = "two"
	if result := verifyProfile(before, after, "power-saver", "", step); result.Status != Unknown || result.Changed != nil {
		t.Fatalf("trusted replacement D-Bus target: %+v", result)
	}
}

func TestOutputCaptureIsBounded(t *testing.T) {
	var output limitedOutput
	data := bytes.NewReader(make([]byte, outputLimit+1))
	if n, err := io.Copy(&output, data); err != nil || n != outputLimit+1 || output.buffer.Len() != outputLimit || !output.truncated {
		t.Fatalf("bounded capture retained %d bytes, read %d bytes, err %v", output.buffer.Len(), n, err)
	}
}

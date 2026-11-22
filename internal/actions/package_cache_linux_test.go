package actions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"golang.org/x/sys/unix"
)

func TestPacmanCacheConfig(t *testing.T) {
	if !supportsPacman("arch", "amd64") || !supportsPacman("archarm", "arm64") ||
		supportsPacman("archarm", "arm") || supportsPacman("archarm", "amd64") || supportsPacman("manjaro", "arm64") {
		t.Fatal("unexpected platform support for Pacman")
	}
	const config = "RootDir = /\nDBPath = /var/lib/pacman/\nCacheDir = /var/cache/pacman/pkg/\nCacheDir = /var/cache/pacman/secondary/\n"
	layout, err := parsePacmanLayout(config)
	if err != nil || len(layout.Caches) != 2 || layout.Caches[1] != "/var/cache/pacman/secondary" {
		t.Fatalf("layout %+v err %v", layout, err)
	}
	args, answers := layout.operation()
	if strings.Contains(strings.Join(args, " "), "--noconfirm") || answers != "Y\nY\nN\n" {
		t.Fatalf("unexpected native confirmations %v %q", args, answers)
	}
	for _, value := range []string{"/home/person", "/var/cache/pacman/pkg/../other", "/var/cache/pacman/pkg/subdir", "/var/cache/pacman"} {
		if _, err := parsePacmanLayout(strings.Replace(config, "/var/cache/pacman/secondary/", value, 1)); err == nil {
			t.Errorf("accepted unsafe or overlapping cache %q", value)
		}
	}
	if _, err := parsePacmanLayout(strings.Replace(config, "DBPath = /var/lib/pacman/", "DBPath = /tmp", 1)); err == nil {
		t.Error("accepted alternate system database")
	}
}

func TestDNFCacheConfig(t *testing.T) {
	const config = "installroot = /\npersistdir = /var/lib/dnf\nsystem_cachedir = /var/cache/libdnf5/active\ncachedir = /cache/libdnf5\nskip_system_repo_lock = 0\n"
	if _, err := parseDNFLayout(config, true); err == nil {
		t.Fatal("root config resolved a different cache")
	}
	layout, err := parseDNFLayout(config, false)
	want := dnfLayout{Cache: "/var/cache/libdnf5/active", Lock: "/var/lib/dnf/system-repo.lock"}
	if err != nil || layout != want {
		t.Fatalf("layout %+v err %v", layout, err)
	}
	for _, value := range []string{"/home/person", "/var/cache/libdnf5/a/../../x", "/var/cache/libdnf5/$releasever", "/var/cache/libdnf5-alt"} {
		if _, err := parseDNFLayout(strings.Replace(config, want.Cache, value, 1), false); err == nil {
			t.Errorf("accepted unsafe or unresolved cache %q", value)
		}
	}
	if _, err := parseDNFLayout(strings.Replace(config, "skip_system_repo_lock = 0", "skip_system_repo_lock = 1", 1), false); err == nil {
		t.Error("accepted config bypassing native locking")
	}
	if dnfSupportedVersion("dnf5 version 5.4.6.0\nlibdnf5 version 5.4.7.0") {
		t.Error("accepted different libdnf5 version")
	}
}

func TestNativePackageInventory(t *testing.T) {
	for _, kind := range []string{CleanPacman, CleanDNF} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			owner := uint32(os.Geteuid())
			path := root
			name := "fixture-1-1-any.pkg.tar.zst"
			scan := func() (cacheInventory, error) {
				return pacmanLayout{Caches: []string{root}}.inventory(t.Context(), fixtureGuard, owner)
			}
			if kind == CleanDNF {
				path, name = filepath.Join(root, "repo-123", "packages"), "fixture-1-1.noarch.rpm"
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				putFile(t, filepath.Join(filepath.Dir(path), "metadata"), "preserve")
				scan = func() (cacheInventory, error) {
					inventory, err := (dnfLayout{Cache: root}).inventory(t.Context(), fixtureGuard, owner)
					return inventory.cacheInventory, err
				}
			}
			file := filepath.Join(path, name)
			putFile(t, file, "1234")
			before, err := scan()
			if err != nil || before.KnownFiles != 1 || before.KnownBytes != 4 {
				t.Fatalf("inventory %+v, error %v", before, err)
			}
			putFile(t, file, "x")
			after, err := scan()
			if err != nil || before.fingerprint() == after.fingerprint() {
				t.Fatalf("changed file did not invalidate the preview, error %v", err)
			}
			stopped := verifyPackageCache(before, after, true, attempt{stopped: true})
			if stopped.Status != Unknown || stopped.Changed != nil {
				t.Fatalf("replacement should leave unknown effect: %+v", stopped)
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			after, err = scan()
			result := verifyPackageCache(before, after, err == nil, attempt{})
			if result.Status != Verified || result.RemovedBytes == nil || *result.RemovedBytes != 4 {
				t.Fatalf("result %+v, error %v", result, err)
			}
			result = verifyPackageCache(before, after, true, attempt{stopped: true, err: errors.New("cancelled")})
			if result.Status != Partial || result.Changed == nil || !*result.Changed {
				t.Fatalf("cancellation with removal should show partial effect: %+v", result)
			}
			outside := filepath.Join(t.TempDir(), "sentinel")
			putFile(t, outside, "preserve")
			if err := os.Symlink(outside, file); err != nil {
				t.Fatal(err)
			}
			if _, err := scan(); err == nil {
				t.Fatal("accepted symlink in deletion scope")
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(outside, file); err != nil {
				t.Fatal(err)
			}
			if _, err := scan(); err == nil {
				t.Fatal("accepted hard link in deletion scope")
			}
		})
	}
}

func TestDNFNativeRecordLock(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "system-repo.lock")
	putFile(t, lock, "")
	info, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(lock, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	request := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(file.Fd(), unix.F_SETLK, &request); err != nil {
		t.Fatal(err)
	}
	if err := checkDNFLock(lock, info); !errors.Is(err, errDNFBusy) {
		t.Fatalf("record lock conflict was not detected: %v", err)
	}
}

// This integration test intentionally operates only on the cache of a disposable Docker image.
// The workflow mounts just the compiled test binary and disables networking.
func TestNativePackageManagerContainer(t *testing.T) {
	platform := os.Getenv("WHWMPC_TEST_CACHE_CONTAINER")
	if platform == "" {
		t.Skip("requires the disposable package-manager container workflow")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil || os.Geteuid() != 0 {
		t.Fatal("run with the container workflow, which provides isolated package paths")
	}
	release, err := os.ReadFile("/etc/os-release")
	if err != nil || distroID(release) != platform {
		t.Fatalf("unexpected test platform %q, error %v", distroID(release), err)
	}
	var kind, path, keep string
	pacman := platform == "arch" || platform == "archarm"
	switch platform {
	case "arch", "archarm":
		kind, path, keep = CleanPacman, "/var/cache/pacman/pkg/whwmpc-fixture-1-1-any.pkg.tar.zst", "/var/lib/pacman/sync/whwmpc-fixture.db"
	case "fedora":
		kind, path, keep = CleanDNF, "/var/cache/libdnf5/whwmpc-fixture-123/packages/whwmpc-fixture-1-1.noarch.rpm", "/var/cache/libdnf5/whwmpc-fixture-123/repodata/whwmpc-fixture.xml"
	default:
		t.Fatalf("unknown test platform %q", platform)
	}
	for _, file := range []string{path, keep} {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		putFile(t, file, "preserve")
	}
	s := New()
	preview := func() Plan {
		t.Helper()
		inventory, err := s.Cleanup(t.Context())
		if err != nil || inventory.Availability != domain.Available || inventory.Kind != kind || inventory.KnownFiles != 1 {
			t.Fatalf("inventory %+v, error %v", inventory, err)
		}
		plan, err := s.Preview(t.Context(), inventory.Kind, "")
		if err != nil || plan.EstimatedBytes == nil || *plan.EstimatedBytes != inventory.KnownBytes {
			t.Fatalf("plan %+v, error %v", plan, err)
		}
		return plan
	}
	plan := preview()
	putFile(t, path, "new-content")
	if result, err := s.Execute(t.Context(), plan, plan.ID); err == nil || result.Status != NotStarted {
		t.Fatalf("stale confirmation started: %+v, error %v", result, err)
	}
	plan = preview()
	var releaseLock func()
	t.Cleanup(func() {
		if releaseLock != nil {
			releaseLock()
		}
	})
	locked, err := s.ExecuteWithProgress(t.Context(), plan, plan.ID, func(stage Stage) {
		if stage != Executing {
			return
		}
		if pacman {
			lock := filepath.Join(pacmanDBPath, "db.lck")
			file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			file.Close()
			releaseLock = func() {
				if err := os.Remove(lock); err != nil {
					t.Fatalf("fixture lock disappeared: %v", err)
				}
			}
		} else {
			file, err := os.OpenFile(filepath.Join(dnfPersist, dnfLockName), os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			releaseLock = func() { file.Close() }
			request := unix.Flock_t{Type: unix.F_WRLCK}
			if err := unix.FcntlFlock(file.Fd(), unix.F_SETLK, &request); err != nil {
				t.Fatal(err)
			}
		}
	})
	if releaseLock == nil {
		t.Fatalf("could not advance to the lock contention test: %+v, error %v", locked, err)
	}
	releaseLock()
	releaseLock = nil
	if pacman {
		if err != nil || locked.Status != Failed || locked.Changed == nil || *locked.Changed {
			t.Fatalf("Pacman did not respect its lock: %+v, error %v", locked, err)
		}
	} else if !errors.Is(err, errDNFBusy) || locked.Status != NotStarted {
		t.Fatalf("DNF5 crossed a contested native repository lock: %+v, error %v", locked, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new-content" {
		t.Fatalf("candidate changed during lock contention: %v", err)
	}
	plan = preview()
	result, err := s.Execute(t.Context(), plan, plan.ID)
	if err != nil || result.Status != Verified || result.Changed == nil || !*result.Changed ||
		result.RemovedBytes == nil || *result.RemovedBytes != uint64(len("new-content")) || result.FreeSpaceDelta == nil {
		t.Fatalf("cleanup did not verify: %+v, error %v", result, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate still present: %v", err)
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "preserve" {
		t.Fatalf("outside-scope sentinel changed: %v", err)
	}
	if _, err := s.Execute(t.Context(), plan, plan.ID); err == nil {
		t.Fatal("reused confirmation")
	}
	t.Logf("%s: rejected changed plan and native lock conflict, %d bytes removed, off-scope sentinel preserved", platform, *result.RemovedBytes)
}

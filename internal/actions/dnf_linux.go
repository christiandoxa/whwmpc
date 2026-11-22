package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	dnfCategory  = "Cache paket DNF5"
	dnfTarget    = "dnf5-cache"
	dnfCacheRoot = "/var/cache/libdnf5"
	dnfPersist   = "/var/lib/dnf"
	dnfLockName  = "system-repo.lock"
	dnfBusyExit  = 113
	dnfImpact    = "DNF5 menghapus unduhan RPM teramati di subdirektori packages. Paket terpasang, metadata repositori, dan cache solver dipertahankan."
)

var (
	dnfCachePath = regexp.MustCompile(`^/var/cache/libdnf5(?:/[a-zA-Z0-9_.+-]+)*$`)
	dnfArchive   = regexp.MustCompile(`^[a-zA-Z0-9@_+.%~-]+\.rpm(?:\.part)?$`)
	dnfSucceeded = regexp.MustCompile(`(?m)^Removed [0-9]+ files, [0-9]+ directories \(total of [^)]+\)\. 0 errors occurred\.$`)
	errDNFBusy   = errors.New("repositori sistem DNF sedang digunakan. Coba kembali setelah transaksi berakhir")
)

type dnfLayout struct {
	Cache string
	Lock  string
}

type dnfInventory struct {
	cacheInventory
	packages []directoryID
}

func dnfOptions(logdir string) []string {
	return []string{"--no-plugins", "--cacheonly", "--setopt=logdir=" + logdir, "--setopt=log_rotate=0", "--setopt=color=never"}
}

func (p linuxPlatform) dnfLayout(ctx context.Context) (dnfLayout, string, error) {
	release, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return dnfLayout{}, "", err
	}
	if distroID(release) != "fedora" {
		return dnfLayout{}, "", fmt.Errorf("backend DNF5 memerlukan Fedora: %w", errCacheUnsupported)
	}
	for _, path := range []string{"/etc/dnf", "/etc/dnf/dnf.conf"} {
		if _, err := protectedPath(path, 0, false); err != nil {
			return dnfLayout{}, "", fmt.Errorf("konfigurasi DNF5 sistem tidak dapat diperiksa: %w", err)
		}
	}
	// Even configuration and version queries open a DNF5 log. Keep it in a private scratch directory.
	scratch, err := os.MkdirTemp("/tmp", "whwmpc-dnf5-")
	if err != nil {
		return dnfLayout{}, "", err
	}
	defer os.RemoveAll(scratch)
	version := p.runner.Run(ctx, "dnf5", append(dnfOptions(scratch), "--version")...)
	if version.Err != nil {
		return dnfLayout{}, "", version.Err
	}
	if version.ExitCode != 0 || !dnfSupportedVersion(string(version.Output)) {
		return dnfLayout{}, "", fmt.Errorf("backend DNF5 memerlukan dnf5 dan libdnf5 versi 5.4.6.0: %w", errCacheUnsupported)
	}
	cfg := p.runner.Run(ctx, "dnf5", append(dnfOptions(scratch), "--dump-main-config")...)
	if cfg.Err != nil {
		return dnfLayout{}, "", cfg.Err
	}
	if cfg.ExitCode != 0 {
		return dnfLayout{}, "", errors.New("DNF5 gagal membaca konfigurasi cache sistem")
	}
	layout, err := parseDNFLayout(string(cfg.Output), os.Geteuid() == 0)
	if err != nil {
		return dnfLayout{}, "", err
	}
	flock := p.runner.Run(ctx, "flock", "--help")
	if flock.Err != nil {
		return dnfLayout{}, "", flock.Err
	}
	if flock.ExitCode != 0 || !strings.Contains(string(flock.Output), "--fcntl") || !strings.Contains(string(flock.Output), "--no-fork") {
		return dnfLayout{}, "", fmt.Errorf("flock dengan dukungan --fcntl dan --no-fork diperlukan: %w", errCacheUnsupported)
	}
	lock, err := protectedPath(layout.Lock, 0, false)
	if err != nil {
		return dnfLayout{}, "", fmt.Errorf("lock repositori sistem DNF5: %w", err)
	}
	if !lock.Mode().IsRegular() || lock.Sys().(*syscall.Stat_t).Nlink != 1 {
		return dnfLayout{}, "", errors.New("lock repositori sistem DNF5 harus berupa satu file biasa")
	}
	if err := checkDNFLock(layout.Lock, lock); err != nil {
		return dnfLayout{}, "", err
	}
	return layout, digest(string(version.Output), layout, identity(lock)), nil
}

func dnfSupportedVersion(output string) bool {
	var cli, library bool
	for line := range strings.SplitSeq(output, "\n") {
		cli = cli || strings.TrimSpace(line) == "dnf5 version 5.4.6.0"
		library = library || strings.TrimSpace(line) == "libdnf5 version 5.4.6.0"
	}
	return cli && library
}

func parseDNFLayout(output string, root bool) (dnfLayout, error) {
	values := make(map[string]string)
	for line := range strings.SplitSeq(output, "\n") {
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		switch key {
		case "installroot", "persistdir", "system_cachedir", "cachedir", "skip_system_repo_lock":
			if _, exists := values[key]; exists {
				return dnfLayout{}, errors.New("keluaran konfigurasi DNF5 memuat nilai duplikat")
			}
			values[key] = trimSlash(value)
		}
	}
	cache := values["system_cachedir"]
	if values["installroot"] != "/" || values["persistdir"] != dnfPersist || values["skip_system_repo_lock"] != "0" {
		return dnfLayout{}, fmt.Errorf("installroot, persistdir, atau pengaturan lock DNF5 memerlukan peninjauan manual: %w", errCacheUnsupported)
	}
	if !within(dnfCacheRoot, cache) || !dnfCachePath.MatchString(cache) || (root && values["cachedir"] != cache) {
		return dnfLayout{}, fmt.Errorf("cache sistem DNF5 harus berada di bawah /var/cache/libdnf5 dengan konfigurasi root yang konsisten: %w", errCacheUnsupported)
	}
	return dnfLayout{Cache: cache, Lock: filepath.Join(dnfPersist, dnfLockName)}, nil
}

func checkDNFLock(path string, expected os.FileInfo) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(expected, current) {
		return errors.New("lock repositori sistem DNF5 berubah saat diperiksa")
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0}
	if err := unix.FcntlFlock(file.Fd(), unix.F_OFD_GETLK, &lock); err != nil {
		return fmt.Errorf("filesystem lock DNF5 tidak mendukung F_OFD_GETLK: %w", err)
	}
	if lock.Type != unix.F_UNLCK {
		return errDNFBusy
	}
	return nil
}

func (layout dnfLayout) operation(exe string) []string {
	// OFD fcntl locks conflict with the POSIX record lock held by DNF5 for its system repo.
	args := []string{"--fcntl", "--exclusive", "--nonblock", "--no-fork", "--conflict-exit-code", strconv.Itoa(dnfBusyExit), layout.Lock, exe}
	args = append(args, dnfOptions("/var/log")...)
	return append(args, "--installroot=/", "--setopt=cachedir="+layout.Cache, "--setopt=system_cachedir="+layout.Cache, "clean", "packages")
}

func (layout dnfLayout) inventory(ctx context.Context, guard fileGuard, owner uint32) (dnfInventory, error) {
	inventory := dnfInventory{cacheInventory: newCacheInventory(CleanDNF, dnfCategory, dnfImpact)}
	root, err := guard(layout.Cache, owner, true)
	if err != nil {
		return inventory, err
	}
	if root == nil {
		inventory.Detail = "Cache sistem DNF5 belum terbentuk."
		return inventory, nil
	}
	entries, err := cacheDirectoryEntries(ctx, layout.Cache, root, guard, owner)
	if err != nil {
		return inventory, err
	}
	inventory.Scope = append(inventory.Scope, layout.Cache+"/<repositori>/packages")
	inventory.addDirectory(layout.Cache, root)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return inventory, err
		}
		path := filepath.Join(layout.Cache, entry.Name())
		info, err := guard(path, owner, false)
		if err != nil {
			return inventory, err
		}
		if !info.IsDir() {
			continue
		}
		if identity(root).Device != identity(info).Device || len(inventory.dirs) > 256 {
			return inventory, errors.New("cakupan DNF5 terlalu luas atau melintasi filesystem")
		}
		inventory.addDirectory(path, info)
		if err := inventory.scanPackages(ctx, path, guard, owner, identity(root).Device); err != nil {
			return inventory, err
		}
		if err := unchangedCacheDirectory(path, info, guard, owner); err != nil {
			return inventory, err
		}
	}
	return inventory, unchangedCacheDirectory(layout.Cache, root, guard, owner)
}

func (inventory *dnfInventory) scanPackages(ctx context.Context, repo string, guard fileGuard, owner uint32, device uint64) error {
	path := filepath.Join(repo, "packages")
	info, err := guard(path, owner, true)
	if err != nil || info == nil {
		return err
	}
	id := identity(info)
	if id.Device != device {
		return errors.New("direktori paket DNF5 melintasi filesystem")
	}
	entries, err := cacheDirectoryEntries(ctx, path, info, guard, owner)
	if err != nil {
		return err
	}
	inventory.packages = append(inventory.packages, directoryID{path, id.Device, id.Inode, id.UID, id.Mode})
	inventory.Scope = append(inventory.Scope, path)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !dnfArchive.MatchString(entry.Name()) {
			return fmt.Errorf("entri non-RPM di %s memerlukan peninjauan manual", path)
		}
		file := filepath.Join(path, entry.Name())
		info, err := guard(file, owner, false)
		if err != nil {
			return err
		}
		if identity(info).Device != device {
			return errors.New("entri paket DNF5 melintasi filesystem")
		}
		if err := inventory.addFile(file, info); err != nil {
			return err
		}
	}
	return unchangedCacheDirectory(path, info, guard, owner)
}

func (p linuxPlatform) cleanDNF(ctx context.Context, target string) (prepared, error) {
	if target != "" && target != dnfTarget {
		return prepared{}, errors.New("pilih kategori cache DNF5 secara keseluruhan")
	}
	layout, cfgIdentity, err := p.dnfLayout(ctx)
	if err != nil {
		return prepared{}, err
	}
	inventory, err := layout.inventory(ctx, protectedPath, 0)
	if err != nil {
		return prepared{}, err
	}
	if len(inventory.Scope) == 0 {
		return prepared{}, errors.New("cache sistem DNF5 belum terbentuk")
	}
	if err := trustedDNFLog(); err != nil {
		return prepared{}, err
	}
	exe, err := trustedExecutable("dnf5")
	if err != nil {
		return prepared{}, err
	}
	inv, err := newInvocation("flock", layout.operation(exe.path), true)
	if err != nil {
		return prepared{}, err
	}
	inv.children = []executable{exe}
	system, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	var beforeFree *uint64
	return prepared{
		plan: Plan{
			Kind: CleanDNF, Target: dnfTarget, Initial: fmt.Sprintf("%d file RPM di %d direktori packages. Cakupan %s.", inventory.KnownFiles, len(inventory.packages), inventory.Scope[0]),
			Identity:  digest(system, cfgIdentity, inventory.fingerprint(), inventory.packages, inv.identity()),
			Operation: inv.spec, Privilege: inv.privilege, Impact: dnfImpact,
			Risk:           "Penghapusan permanen. Lock repositori sistem DNF5 harus tersedia. DNF5 memakai log sistemnya.",
			EstimatedBytes: &inventory.KnownBytes,
		},
		run: func(ctx context.Context) attempt {
			beforeFree = inventory.freeSpace()
			step := inv.run(ctx, 2*time.Minute)
			if step.exitCode == dnfBusyExit && !step.stopped {
				return attempt{err: errDNFBusy}
			}
			if step.err == nil && !dnfSucceeded.Match(step.output) {
				step.err = errors.New("DNF5 tidak melaporkan penyelesaian pembersihan tanpa error")
			}
			return step
		},
		verify: func(ctx context.Context, step attempt) Result {
			after, err := layout.inventory(ctx, protectedPath, 0)
			same := err == nil && slices.Equal(inventory.dirs, after.dirs)
			if same && len(after.packages) > 0 && after.KnownFiles == 0 && step.err == nil {
				step.err = errors.New("direktori packages DNF5 masih ada")
			}
			result := verifyPackageCache(inventory.cacheInventory, after.cacheInventory, same, step)
			if same {
				result.FreeSpaceDelta = spaceDelta(beforeFree, after.freeSpace())
				for _, dir := range inventory.packages {
					if !slices.ContainsFunc(after.packages, func(other directoryID) bool { return other.Path == dir.Path }) {
						result.Changed = boolPointer(true)
						if result.Status != Verified && result.Status != Partial {
							result.Status, result.Message = Partial, withStepError("Sebagian direktori paket hilang setelah operasi", step)
						}
						break
					}
				}
			}
			return result
		},
	}, nil
}

func trustedDNFLog() error {
	info, err := protectedPath("/var/log/dnf5.log", 0, true)
	if err != nil || (info != nil && (!info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1)) {
		return errors.New("tujuan log DNF5 sistem tidak aman")
	}
	return nil
}

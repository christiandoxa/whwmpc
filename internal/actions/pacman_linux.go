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
	pacmanCategory = "Cache paket Pacman"
	pacmanTarget   = "pacman-cache"
	pacmanConfig   = "/etc/pacman.conf"
	pacmanDBPath   = "/var/lib/pacman"
	pacmanImpact   = "Pacman menghapus arsip paket di direktori cache yang ditampilkan. Paket terpasang dan basis data repositori dipertahankan. Unduhan mungkin diperlukan lagi."
)

var (
	pacmanVersion = regexp.MustCompile(`(?m)^[ .-]+Pacman v7\.1\.0 - libalpm v16\.0\.[0-9]+$`)
	pacmanArchive = regexp.MustCompile(`^[a-zA-Z0-9@_+.-]+\.pkg\.tar(?:\.(?:zst|xz|gz|bz2|lrz|lzo|lz4|lz))?(?:\.(?:sig|part|sig\.part))?$`)
)

type pacmanLayout struct {
	Caches      []string
	DownloadUID uint32
}

func (p linuxPlatform) pacmanLayout(ctx context.Context) (pacmanLayout, string, error) {
	release, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return pacmanLayout{}, "", err
	}
	if distroID(release) != "arch" {
		return pacmanLayout{}, "", fmt.Errorf("backend Pacman memerlukan Arch Linux: %w", errCacheUnsupported)
	}
	if config, err := protectedPath(pacmanConfig, 0, false); err != nil || !config.Mode().IsRegular() {
		return pacmanLayout{}, "", errors.New("konfigurasi Pacman sistem tidak aman atau tidak dapat dibaca")
	}
	version := p.runner.Run(ctx, "pacman", "--version")
	if version.Err != nil {
		return pacmanLayout{}, "", version.Err
	}
	if version.ExitCode != 0 || !pacmanVersion.Match(version.Output) {
		return pacmanLayout{}, "", fmt.Errorf("backend Pacman memerlukan versi 7.1.0 dengan libalpm 16.0: %w", errCacheUnsupported)
	}
	cfg := p.runner.Run(ctx, "pacman-conf", "--config", pacmanConfig, "--verbose", "RootDir", "DBPath", "CacheDir", "DownloadUser")
	if cfg.Err != nil {
		return pacmanLayout{}, "", cfg.Err
	}
	if cfg.ExitCode != 0 {
		return pacmanLayout{}, "", errors.New("Pacman gagal membaca konfigurasi cache sistem")
	}
	layout, err := parsePacmanLayout(string(cfg.Output))
	if err != nil {
		return pacmanLayout{}, "", err
	}
	db, err := protectedPath(pacmanDBPath, 0, false)
	if err != nil || !db.IsDir() {
		return pacmanLayout{}, "", errors.New("direktori basis data Pacman tidak aman atau tidak dapat diperiksa")
	}
	lock, err := protectedPath(filepath.Join(pacmanDBPath, "db.lck"), 0, true)
	if err != nil || lock != nil {
		return pacmanLayout{}, "", errors.New("lock Pacman ada atau tidak dapat diperiksa. Coba kembali setelah transaksi berakhir.")
	}
	return layout, digest(string(version.Output), string(cfg.Output), identity(db)), nil
}

func parsePacmanLayout(output string) (pacmanLayout, error) {
	layout := pacmanLayout{}
	var root, db, user string
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		key, value, ok := strings.Cut(line, " = ")
		if !ok || value == "" || (key != "CacheDir" && seen[key]) {
			return pacmanLayout{}, errors.New("keluaran pacman-conf tidak dikenali")
		}
		seen[key] = true
		switch key {
		case "RootDir":
			root = value
		case "DBPath":
			db = trimSlash(value)
		case "CacheDir":
			path := trimSlash(value)
			if !strictlyWithin("/var/cache/pacman", path) || len(layout.Caches) == 16 {
				return pacmanLayout{}, errors.New("direktori cache Pacman harus berada di bawah /var/cache/pacman")
			}
			for _, other := range layout.Caches {
				if within(path, other) || within(other, path) {
					return pacmanLayout{}, errors.New("direktori cache Pacman tumpang tindih")
				}
			}
			layout.Caches = append(layout.Caches, path)
		case "DownloadUser":
			user = value
		default:
			return pacmanLayout{}, errors.New("kunci pacman-conf tidak dikenali")
		}
	}
	if root != "/" || db != pacmanDBPath || len(layout.Caches) == 0 {
		return pacmanLayout{}, errors.New("root, basis data, atau cakupan cache Pacman nonstandar memerlukan peninjauan manual")
	}
	switch user {
	case "", "root":
		return layout, nil
	case "alpm":
		data, err := os.ReadFile("/etc/passwd")
		if err != nil {
			return pacmanLayout{}, err
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			fields := strings.SplitN(line, ":", 4)
			if len(fields) == 4 && fields[0] == user {
				uid, err := strconv.ParseUint(fields[2], 10, 32)
				if err == nil && uid != 0 {
					layout.DownloadUID = uint32(uid)
					return layout, nil
				}
			}
		}
		return pacmanLayout{}, errors.New("akun sandbox alpm tidak dapat diverifikasi")
	default:
		return pacmanLayout{}, errors.New("akun unduhan Pacman nonstandar memerlukan peninjauan manual")
	}
}

func (layout pacmanLayout) operation() ([]string, string) {
	// Config CacheDir is additive. Supply only the inspected paths to pacman and explicitly decline sync DB deletion.
	args := []string{"--config", "/dev/null", "--logfile", "/dev/null", "--root", "/", "--dbpath", pacmanDBPath, "--sync", "--clean", "--clean"}
	for _, path := range layout.Caches {
		args = append(args, "--cachedir", path)
	}
	return args, strings.Repeat("Y\n", len(layout.Caches)) + "N\n"
}

func (layout pacmanLayout) inventory(ctx context.Context, guard fileGuard, owner uint32) (cacheInventory, error) {
	inventory := newCacheInventory(CleanPacman, pacmanCategory, pacmanImpact)
	for _, path := range layout.Caches {
		info, err := guard(path, owner, false)
		if err != nil {
			return inventory, fmt.Errorf("direktori cache Pacman %s: %w", path, err)
		}
		entries, err := cacheDirectoryEntries(ctx, path, info, guard, owner)
		if err != nil {
			return inventory, err
		}
		inventory.Scope = append(inventory.Scope, path+"/*")
		inventory.addDirectory(path, info)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return inventory, err
			}
			file := filepath.Join(path, entry.Name())
			if !pacmanArchive.MatchString(entry.Name()) {
				return inventory, fmt.Errorf("entri nonpaket di %s memerlukan peninjauan manual", path)
			}
			item, err := guard(file, owner, false)
			if err != nil && layout.DownloadUID != 0 && layout.DownloadUID != owner {
				item, err = guard(file, layout.DownloadUID, false)
			}
			if err != nil {
				return inventory, err
			}
			if err := inventory.addFile(file, item); err != nil {
				return inventory, err
			}
		}
		if err := unchangedCacheDirectory(path, info, guard, owner); err != nil {
			return inventory, err
		}
	}
	return inventory, nil
}

func (p linuxPlatform) cleanPacman(ctx context.Context, target string) (prepared, error) {
	if target != "" && target != pacmanTarget {
		return prepared{}, errors.New("pilih kategori cache Pacman secara keseluruhan")
	}
	layout, cfgIdentity, err := p.pacmanLayout(ctx)
	if err != nil {
		return prepared{}, err
	}
	inventory, err := layout.inventory(ctx, protectedPath, 0)
	if err != nil {
		return prepared{}, err
	}
	if _, err := protectedPath("/dev", 0, false); err != nil {
		return prepared{}, err
	}
	null, err := os.Lstat("/dev/null")
	if err != nil {
		return prepared{}, err
	}
	node := null.Sys().(*syscall.Stat_t)
	if null.Mode()&os.ModeCharDevice == 0 || node.Uid != 0 || unix.Major(uint64(node.Rdev)) != 1 || unix.Minor(uint64(node.Rdev)) != 3 {
		return prepared{}, errors.New("/dev/null bukan perangkat sistem yang diharapkan")
	}
	args, answers := layout.operation()
	inv, err := newInvocation("pacman", args, true)
	if err != nil {
		return prepared{}, err
	}
	system, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	var beforeFree *uint64
	return prepared{
		plan: Plan{
			Kind: CleanPacman, Target: pacmanTarget, Initial: fmt.Sprintf("%d entri. Cakupan %s.", inventory.KnownFiles, strings.Join(inventory.Scope, ", ")),
			Identity:  digest(system, layout, cfgIdentity, identity(null), inventory.fingerprint(), inv.identity(), answers),
			Operation: inv.spec, Privilege: inv.privilege, Impact: pacmanImpact,
			Risk:           "Penghapusan permanen. Pacman mengambil lock basis datanya. Basis data repositori dijaga dengan jawaban penolakan terpisah.",
			EstimatedBytes: &inventory.KnownBytes,
		},
		run: func(ctx context.Context) attempt {
			beforeFree = inventory.freeSpace()
			return inv.runInput(ctx, 2*time.Minute, strings.NewReader(answers))
		},
		verify: func(ctx context.Context, step attempt) Result {
			after, err := layout.inventory(ctx, protectedPath, 0)
			same := err == nil && slices.Equal(inventory.dirs, after.dirs)
			result := verifyPackageCache(inventory, after, same, step)
			if same {
				result.FreeSpaceDelta = spaceDelta(beforeFree, after.freeSpace())
			}
			return result
		},
	}, nil
}

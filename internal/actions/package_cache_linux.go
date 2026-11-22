package actions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"golang.org/x/sys/unix"
)

const maxCacheEntries = 100_000

var errCacheUnsupported = errors.New("backend pembersihan cache tidak didukung")

func distroID(release []byte) string {
	for line := range strings.SplitSeq(string(release), "\n") {
		if id, ok := strings.CutPrefix(line, "ID="); ok {
			return strings.Trim(id, "'\"")
		}
	}
	return ""
}

func (p linuxPlatform) cleanup(ctx context.Context) (Cleanup, error) {
	release, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return cleanupUnavailable("", "Cache paket sistem", err), err
	}
	switch distroID(release) {
	case "arch":
		layout, _, err := p.pacmanLayout(ctx)
		if err == nil {
			var inventory cacheInventory
			inventory, err = layout.inventory(ctx, protectedPath, 0)
			if err == nil {
				return inventory.Cleanup, nil
			}
		}
		return cleanupUnavailable(CleanPacman, pacmanCategory, err), err
	case "fedora":
		layout, _, err := p.dnfLayout(ctx)
		if err == nil {
			var inventory dnfInventory
			inventory, err = layout.inventory(ctx, protectedPath, 0)
			if err == nil {
				return inventory.Cleanup, nil
			}
		}
		return cleanupUnavailable(CleanDNF, dnfCategory, err), err
	default:
		return p.aptCleanup(ctx)
	}
}

func cleanupUnavailable(kind, category string, err error) Cleanup {
	return Cleanup{Kind: kind, Availability: availability(err), Category: category, Detail: safeText(err.Error())}
}

type cacheFile struct {
	Path string
	ID   fileID
}

type cacheInventory struct {
	Cleanup
	dirs  []directoryID
	files []cacheFile
}

func newCacheInventory(kind, category, impact string) cacheInventory {
	return cacheInventory{Cleanup: Cleanup{Kind: kind, Category: category, Availability: domain.Available, Impact: impact}}
}

func (inventory *cacheInventory) addDirectory(path string, info os.FileInfo) {
	id := identity(info)
	inventory.dirs = append(inventory.dirs, directoryID{path, id.Device, id.Inode, id.UID, id.Mode})
}

func (inventory *cacheInventory) addFile(path string, info os.FileInfo) error {
	id := identity(info)
	if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 || id.Size < 0 {
		return fmt.Errorf("entri cache %s harus berupa satu file biasa", path)
	}
	size := uint64(id.Size)
	if len(inventory.files) == maxCacheEntries || inventory.KnownBytes > math.MaxUint64-size {
		return errors.New("inventaris cache melampaui batas pemeriksaan")
	}
	inventory.files = append(inventory.files, cacheFile{path, id})
	inventory.KnownFiles++
	inventory.KnownBytes += size
	return nil
}

func (inventory cacheInventory) fingerprint() string {
	return digest(inventory.Scope, inventory.dirs, inventory.files)
}

func (inventory cacheInventory) freeSpace() *uint64 {
	var total uint64
	seen := make(map[uint64]bool)
	for _, dir := range inventory.dirs {
		if seen[dir.Device] {
			continue
		}
		space := freeSpace(dir.Path)
		if space == nil || total > math.MaxUint64-*space {
			return nil
		}
		total += *space
		seen[dir.Device] = true
	}
	if len(seen) == 0 {
		return nil
	}
	return &total
}

func cacheDirectoryEntries(ctx context.Context, path string, before os.FileInfo, guard fileGuard, owner uint32) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("cakupan %s bukan direktori", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), path)
	defer dir.Close()
	current, err := dir.Stat()
	if err != nil || !os.SameFile(before, current) {
		return nil, fmt.Errorf("direktori cache %s berubah sebelum diperiksa", path)
	}
	entries, err := dir.ReadDir(maxCacheEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxCacheEntries {
		return nil, fmt.Errorf("direktori cache %s melampaui batas pemeriksaan", path)
	}
	if err := unchangedCacheDirectory(path, before, guard, owner); err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func unchangedCacheDirectory(path string, before os.FileInfo, guard fileGuard, owner uint32) error {
	after, err := guard(path, owner, false)
	if err != nil || identity(before) != identity(after) {
		return fmt.Errorf("direktori cache %s berubah saat diperiksa", path)
	}
	return nil
}

func verifyPackageCache(before, after cacheInventory, sameScope bool, step attempt) Result {
	if !sameScope {
		return Result{Status: Unknown, Message: withStepError("Cakupan cache berubah atau tidak dapat dibaca ulang", step)}
	}
	remaining := make(map[string]fileID, len(after.files))
	for _, file := range after.files {
		remaining[file.Path] = file.ID
	}
	var removed uint64
	var removedFiles int
	replaced := false
	for _, file := range before.files {
		if id, ok := remaining[file.Path]; !ok {
			removed += uint64(file.ID.Size)
			removedFiles++
		} else {
			replaced = replaced || id != file.ID
		}
	}
	result := Result{RemovedBytes: &removed}
	changed := removedFiles > 0
	if changed || !replaced {
		result.Changed = boolPointer(changed)
	}
	switch {
	case step.err == nil && !step.stopped && after.KnownFiles == 0:
		result.Status, result.Message = Verified, "Cakupan cache paket diperiksa kosong"
	case changed || (step.err == nil && !step.stopped):
		result.Status, result.Message = Partial, "Sebagian hasil teramati. Masih ada entri tersisa atau operasi berhenti sebelum selesai"
	case replaced || step.stopped:
		result.Status, result.Message = Unknown, "Hasil lengkap tidak dapat dipastikan setelah perubahan atau penghentian proses"
	default:
		result.Status, result.Message = Failed, "Entri cache yang diperiksa masih ada atau pembersihan gagal dimulai"
	}
	result.Message = withStepError(result.Message, step)
	return result
}

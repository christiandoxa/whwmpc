package actions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

const aptImpact = "APT menghapus unduhan paket, unduhan indeks yang belum selesai, dan indeks biner cache sesuai konfigurasi. Paket terpasang dipertahankan. Unduhan mungkin diperlukan lagi."

type aptLayout struct {
	Cache       string
	Archives    string
	State       string
	Lists       string
	PkgCache    string
	SrcPkgCache string
	SandboxUID  uint32
}

type aptFile struct {
	Path string
	ID   fileID
}

type aptInventory struct {
	Cleanup
	files []aptFile
	dirs  []directoryID
}

type directoryID struct {
	Path   string
	Device uint64
	Inode  uint64
	UID    uint32
	Mode   uint32
}

func (p linuxPlatform) aptLayout(ctx context.Context) (aptLayout, string, error) {
	release, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return aptLayout{}, "", fmt.Errorf("identitas distro tidak tersedia: %w", err)
	}
	if !supportsAPT(release) {
		return aptLayout{}, "", fmt.Errorf("pembersihan paket untuk distro ini belum didukung: %w", errAPTUnsupported)
	}
	version := p.runner.Run(ctx, "apt-get", "--version")
	if version.Err != nil {
		return aptLayout{}, "", version.Err
	}
	var major, minor int
	if n, _ := fmt.Sscanf(string(version.Output), "apt %d.%d", &major, &minor); version.ExitCode != 0 ||
		n != 2 || !((major == 2 && (minor == 6 || minor == 8)) || (major == 3 && minor == 0)) {
		return aptLayout{}, "", fmt.Errorf("versi APT di luar backend 2.6, 2.8, dan 3.0: %w", errAPTUnsupported)
	}
	if _, err := protectedPath("/etc/apt", 0, false); err != nil {
		return aptLayout{}, "", fmt.Errorf("direktori konfigurasi APT tidak aman: %w", err)
	}
	cfg := p.runner.Run(ctx, "apt-config", "shell",
		"ROOT", "RootDir", "CACHE", "Dir::Cache/d", "ARCHIVES", "Dir::Cache::archives/d",
		"STATE", "Dir::State/d", "LISTS", "Dir::State::lists/d", "PKG", "Dir::Cache::pkgcache/f",
		"SRC", "Dir::Cache::srcpkgcache/f", "SANDBOX", "APT::Sandbox::User",
		"BINROOT", "Binary::apt-get::RootDir", "BINDIR", "Binary::apt-get::Dir",
		"BINCACHE", "Binary::apt-get::Dir::Cache", "BINARCHIVES", "Binary::apt-get::Dir::Cache::archives",
		"BINSTATE", "Binary::apt-get::Dir::State", "BINLISTS", "Binary::apt-get::Dir::State::lists",
		"BINPKG", "Binary::apt-get::Dir::Cache::pkgcache", "BINSRC", "Binary::apt-get::Dir::Cache::srcpkgcache")
	if cfg.Err != nil {
		return aptLayout{}, "", cfg.Err
	}
	if cfg.ExitCode != 0 {
		return aptLayout{}, "", errors.New("gagal membaca konfigurasi APT sistem")
	}
	layout, err := parseAPTLayout(string(cfg.Output))
	if err != nil {
		return aptLayout{}, "", err
	}
	dump := p.runner.Run(ctx, "apt-config", "dump")
	if dump.Err != nil || dump.ExitCode != 0 {
		return aptLayout{}, "", errors.New("konfigurasi lengkap APT tidak dapat diverifikasi")
	}
	return layout, digest(string(version.Output), string(dump.Output)), nil
}

var errAPTUnsupported = errors.New("backend APT tidak didukung")

func supportsAPT(release []byte) bool {
	for line := range strings.SplitSeq(string(release), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || (key != "ID" && key != "ID_LIKE") {
			continue
		}
		for distro := range strings.FieldsSeq(strings.Trim(value, "\"'")) {
			if distro == "debian" || distro == "ubuntu" {
				return true
			}
		}
	}
	return false
}

func parseAPTLayout(output string) (aptLayout, error) {
	values := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || len(value) < 2 || value[0] != '\'' || value[len(value)-1] != '\'' ||
			strings.ContainsRune(value[1:len(value)-1], '\'') {
			return aptLayout{}, errors.New("keluaran apt-config tidak dikenali")
		}
		if _, duplicate := values[key]; duplicate {
			return aptLayout{}, errors.New("apt-config mengembalikan kunci duplikat")
		}
		values[key] = value[1 : len(value)-1]
	}
	if root := values["ROOT"]; root != "" && root != "/" {
		return aptLayout{}, errors.New("RootDir APT alternatif memerlukan peninjauan manual")
	}
	if root := values["BINROOT"]; root != "" && root != "/" {
		return aptLayout{}, errors.New("RootDir khusus apt-get memerlukan peninjauan manual")
	}
	for _, key := range []string{"BINDIR", "BINCACHE", "BINARCHIVES", "BINSTATE", "BINLISTS", "BINPKG", "BINSRC"} {
		if _, overridden := values[key]; overridden {
			return aptLayout{}, errors.New("lokasi cache atau daftar khusus apt-get memerlukan peninjauan manual")
		}
	}
	layout := aptLayout{
		Cache: trimSlash(values["CACHE"]), Archives: trimSlash(values["ARCHIVES"]),
		State: trimSlash(values["STATE"]), Lists: trimSlash(values["LISTS"]),
		PkgCache: values["PKG"], SrcPkgCache: values["SRC"],
	}
	if values["ROOT"] == "/" {
		if layout.PkgCache == "/" {
			layout.PkgCache = ""
		}
		if layout.SrcPkgCache == "/" {
			layout.SrcPkgCache = ""
		}
	}
	if !within("/var/cache/apt", layout.Cache) || !strictlyWithin("/var/cache/apt", layout.Archives) ||
		!within("/var/lib/apt", layout.State) || !strictlyWithin("/var/lib/apt", layout.Lists) {
		return aptLayout{}, errors.New("arsip dan daftar APT harus berada di direktori APT sistem yang didukung")
	}
	for _, path := range []string{layout.PkgCache, layout.SrcPkgCache} {
		if path != "" && (!strictlyWithin("/var/cache/apt", path) || !strings.HasSuffix(path, ".bin") ||
			!strings.Contains(filepath.Base(path), "cache")) {
			return aptLayout{}, errors.New("lokasi cache biner APT memerlukan peninjauan manual")
		}
	}
	switch values["SANDBOX"] {
	case "", "root":
	case "_apt":
		passwd, err := os.ReadFile("/etc/passwd")
		if err != nil {
			return aptLayout{}, errors.New("akun sandbox APT tidak dapat diverifikasi")
		}
		for line := range strings.SplitSeq(string(passwd), "\n") {
			fields := strings.SplitN(line, ":", 4)
			if len(fields) == 4 && fields[0] == "_apt" {
				uid, err := strconv.ParseUint(fields[2], 10, 32)
				if err == nil && uid != 0 {
					layout.SandboxUID = uint32(uid)
					break
				}
			}
		}
		if layout.SandboxUID == 0 {
			return aptLayout{}, errors.New("akun sandbox _apt tidak ditemukan")
		}
	default:
		return aptLayout{}, errors.New("akun sandbox APT nonstandar memerlukan peninjauan manual")
	}
	return layout, nil
}

func trimSlash(path string) string {
	if path == "/" {
		return path
	}
	return strings.TrimSuffix(path, "/")
}

func within(root, path string) bool {
	return safePath(path) && (path == root || strings.HasPrefix(path, root+"/"))
}

func strictlyWithin(root, path string) bool { return path != root && within(root, path) }

func (layout aptLayout) operation() []string {
	// APT otherwise changes partial-directory metadata and removes failed entries before taking its locks.
	// An empty RootDir uses the system root while keeping disabled binary-cache paths empty.
	return []string{"-o", "RootDir=", "-o", "Debug::NoLocking=false", "-o", "APT::Get::Simulate=false",
		"-o", "Debug::SetupAPTPartialDirectory::AssumeGood=true",
		"-o", "Dir::Cache=" + layout.Cache, "-o", "Dir::Cache::archives=" + layout.Archives,
		"-o", "Dir::State=" + layout.State, "-o", "Dir::State::lists=" + layout.Lists,
		"-o", "Dir::Cache::pkgcache=" + layout.PkgCache, "-o", "Dir::Cache::srcpkgcache=" + layout.SrcPkgCache, "clean"}
}

func (layout aptLayout) inventory(ctx context.Context) (aptInventory, error) {
	return layout.readInventory(ctx, protectedPath, 0)
}

type fileGuard func(string, uint32, bool) (os.FileInfo, error)

func (layout aptLayout) readInventory(ctx context.Context, guard fileGuard, rootUID uint32) (aptInventory, error) {
	inventory := aptInventory{Cleanup: Cleanup{Availability: domain.Available, Category: "Cache paket APT", Impact: aptImpact}}
	protected := map[string]bool{}
	for _, path := range []string{layout.Cache, layout.Archives, layout.State, layout.Lists} {
		if protected[path] {
			continue
		}
		info, err := guard(path, rootUID, false)
		if err != nil {
			return inventory, fmt.Errorf("direktori APT %s: %w", path, err)
		}
		if !info.IsDir() {
			return inventory, fmt.Errorf("target APT %s bukan direktori", path)
		}
		inventory.addDir(path, info)
		protected[path] = true
	}
	seen := make(map[string]bool)
	if err := inventory.scan(ctx, layout.Archives, "", rootUID, rootUID, false, seen, guard); err != nil {
		return inventory, err
	}
	auxfiles := filepath.Join(layout.Lists, "auxfiles")
	info, err := guard(auxfiles, layout.SandboxUID, true)
	if err != nil && layout.SandboxUID != rootUID {
		info, err = guard(auxfiles, rootUID, true)
	}
	if err != nil || (info != nil && !info.IsDir()) {
		return inventory, errors.New("direktori auxiliary APT tidak dapat diverifikasi")
	}
	if info != nil {
		inventory.addDir(auxfiles, info)
	}
	for _, scope := range []string{filepath.Join(layout.Archives, "partial"), filepath.Join(layout.Lists, "partial")} {
		if err := inventory.scan(ctx, scope, "", layout.SandboxUID, rootUID, true, seen, guard); err != nil {
			return inventory, err
		}
	}
	for _, cache := range []string{layout.PkgCache, layout.SrcPkgCache} {
		if cache != "" {
			if err := inventory.scan(ctx, filepath.Dir(cache), filepath.Base(cache), rootUID, rootUID, false, seen, guard); err != nil {
				return inventory, err
			}
		}
	}
	if len(inventory.Unknown) != 0 {
		inventory.Availability = domain.Partial
		inventory.Detail = "Ukuran direktori privat APT belum dapat diukur dengan izin saat ini."
	}
	return inventory, nil
}

func (inventory *aptInventory) addDir(path string, info os.FileInfo) {
	id := identity(info)
	inventory.dirs = append(inventory.dirs, directoryID{path, id.Device, id.Inode, id.UID, id.Mode})
}

func (inventory *aptInventory) scan(ctx context.Context, path, prefix string, owner, rootUID uint32, optional bool, seen map[string]bool, guard fileGuard) error {
	if prefix == "" {
		inventory.Scope = append(inventory.Scope, path+"/*")
	} else {
		cache := filepath.Join(path, prefix)
		inventory.Scope = append(inventory.Scope, cache, cache+".*")
	}
	info, err := guard(path, owner, optional)
	if err != nil && owner != rootUID {
		info, err = guard(path, rootUID, optional)
	}
	if err != nil {
		return fmt.Errorf("cakupan APT %s: %w", path, err)
	}
	if info == nil {
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("cakupan APT %s bukan direktori", path)
	}
	inventory.addDir(path, info)
	root, err := os.OpenRoot(path)
	if errors.Is(err, os.ErrPermission) && optional {
		inventory.Unknown = append(inventory.Unknown, path)
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if errors.Is(err, os.ErrPermission) && optional {
		inventory.Unknown = append(inventory.Unknown, path)
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	opened, err := dir.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("direktori APT berubah saat dibuka")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := dir.ReadDir(256)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		for _, entry := range entries {
			name := entry.Name()
			if !aptRemoves(name, prefix) {
				continue
			}
			fullPath := filepath.Join(path, name)
			if seen[fullPath] {
				continue
			}
			file, err := root.Lstat(name)
			if err != nil {
				return err
			}
			id := identity(file)
			if !safePath(fullPath) || !file.Mode().IsRegular() || (id.UID != rootUID && id.UID != owner) ||
				file.Mode().Perm()&0022 != 0 || id.Size < 0 {
				return fmt.Errorf("entri APT %s tidak aman atau bukan berkas biasa", safeText(fullPath))
			}
			if inventory.KnownFiles == 100_000 || uint64(id.Size) > math.MaxUint64-inventory.KnownBytes {
				return errors.New("inventaris APT melebihi batas preview")
			}
			seen[fullPath] = true
			inventory.files = append(inventory.files, aptFile{fullPath, id})
			inventory.KnownFiles++
			inventory.KnownBytes += uint64(id.Size)
		}
	}
}

func aptRemoves(name, prefix string) bool {
	if prefix != "" {
		return name == prefix || strings.HasPrefix(name, prefix+".")
	}
	switch name {
	case "lock", "partial", "auxfiles", "lost+found", ".", "..":
		return false
	}
	return true
}

func (p linuxPlatform) cleanup(ctx context.Context) (Cleanup, error) {
	layout, _, err := p.aptLayout(ctx)
	if err == nil {
		inventory, err := layout.inventory(ctx)
		if err == nil {
			return inventory.Cleanup, nil
		}
		return Cleanup{Availability: availability(err), Category: "Cache paket APT", Detail: safeText(err.Error())}, err
	}
	return Cleanup{Availability: availability(err), Category: "Cache paket APT", Detail: safeText(err.Error())}, err
}

func availability(err error) domain.Availability {
	switch {
	case errors.Is(err, errAPTUnsupported):
		return domain.Unsupported
	case errors.Is(err, os.ErrPermission):
		return domain.PermissionRequired
	case errors.Is(err, os.ErrNotExist):
		return domain.DependencyMissing
	}
	return domain.Error
}

func (p linuxPlatform) apt(ctx context.Context, target string) (prepared, error) {
	if target != "" && target != "apt-cache" {
		return prepared{}, errors.New("pilih kategori cache APT secara keseluruhan")
	}
	layout, cfgIdentity, err := p.aptLayout(ctx)
	if err != nil {
		return prepared{}, err
	}
	inventory, err := layout.inventory(ctx)
	if err != nil {
		return prepared{}, err
	}
	inv, err := newInvocation("apt-get", layout.operation(), true)
	if err != nil {
		return prepared{}, err
	}
	system, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	slices.SortFunc(inventory.files, func(a, b aptFile) int { return strings.Compare(a.Path, b.Path) })
	initial := fmt.Sprintf("%d entri terukur. Cakupan %s.", inventory.KnownFiles, strings.Join(inventory.Scope, ", "))
	if len(inventory.Unknown) != 0 {
		initial += " Ukuran belum diketahui pada " + strings.Join(inventory.Unknown, ", ") + "."
	}
	var beforeFree *uint64
	return prepared{
		plan: Plan{
			Kind: CleanAPT, Target: "apt-cache", Initial: initial,
			Identity:  digest(system, layout, cfgIdentity, inventory.dirs, inventory.files, inventory.Unknown, inv.identity()),
			Operation: inv.spec, Privilege: inv.privilege,
			Impact: aptImpact, Risk: "Penghapusan permanen. APT mengelola lock sendiri. Angka kandidat mencakup entri yang bisa diukur. Verifikasi direktori privat memakai izin sudo yang sama bila tersedia.",
			EstimatedBytes: &inventory.KnownBytes,
		},
		run: func(ctx context.Context) attempt {
			beforeFree = freeSpace(layout.Archives)
			return inv.run(ctx, 2*time.Minute)
		},
		verify: func(ctx context.Context, step attempt) Result {
			after, err := layout.inventory(ctx)
			sameScope := err == nil && slices.Equal(inventory.dirs, after.dirs)
			if sameScope {
				layout.verifyPrivate(ctx, &after)
			}
			result := verifyAPT(inventory, after, step, err)
			if sameScope {
				result.FreeSpaceDelta = spaceDelta(beforeFree, freeSpace(layout.Archives))
			}
			return result
		},
	}, nil
}

func (layout aptLayout) verifyPrivate(ctx context.Context, inventory *aptInventory) {
	unobserved := inventory.Unknown[:0]
	for _, path := range inventory.Unknown {
		if path != filepath.Join(layout.Archives, "partial") && path != filepath.Join(layout.Lists, "partial") {
			unobserved = append(unobserved, path)
			continue
		}
		identityBefore, err := os.Lstat(path)
		if err != nil || !identityBefore.IsDir() {
			unobserved = append(unobserved, path)
			continue
		}
		inv, err := newInvocation("find", []string{"-P", path, "-mindepth", "1", "-maxdepth", "1",
			"!", "-name", "lock", "!", "-name", "partial", "!", "-name", "auxfiles",
			"!", "-name", "lost+found", "-printf", "1", "-quit"}, true)
		if err != nil {
			unobserved = append(unobserved, path)
			continue
		}
		step := inv.run(ctx, 3*time.Second)
		identityAfter, err := os.Lstat(path)
		if step.err != nil || !step.started || err != nil || !os.SameFile(identityBefore, identityAfter) ||
			(len(step.output) != 0 && string(step.output) != "1") {
			unobserved = append(unobserved, path)
			continue
		}
		if len(step.output) > 0 {
			inventory.KnownFiles++
		}
	}
	inventory.Unknown = unobserved
}

func verifyAPT(before, after aptInventory, step attempt, err error) Result {
	if err != nil || !slices.Equal(before.dirs, after.dirs) {
		return Result{Status: Unknown, Message: withStepError("Cakupan APT berubah atau tidak dapat dibaca ulang", step)}
	}
	remaining := make(map[string]bool, len(after.files))
	for _, file := range after.files {
		remaining[file.Path] = true
	}
	var removed uint64
	observed := false
	for _, file := range before.files {
		if !remaining[file.Path] {
			observed = true
			removed += uint64(file.ID.Size)
		}
	}
	result := Result{RemovedBytes: &removed}
	if observed || (len(before.Unknown) == 0 && len(after.Unknown) == 0) {
		result.Changed = boolPointer(observed)
	}
	switch {
	case step.err == nil && after.KnownFiles == 0 && len(after.Unknown) == 0:
		result.Status, result.Message = Verified, "Seluruh cakupan cache APT yang dapat dibersihkan diperiksa kosong"
	case observed || step.err == nil:
		result.Status, result.Message = Partial, "Sebagian hasil teramati. Masih ada entri tersisa atau cakupan yang belum bisa diverifikasi"
	case step.stopped || len(after.Unknown) != 0:
		result.Status, result.Message = Unknown, "Cakupan penuh belum bisa dipastikan setelah proses berhenti"
	default:
		result.Status, result.Message = Failed, "Entri cache yang diperiksa masih ada"
	}
	if len(before.Unknown) > 0 {
		result.Message += ". Angka byte yang terhapus berasal dari entri yang dapat dibaca sebelum pembersihan"
	}
	result.Message = withStepError(result.Message, step)
	return result
}

func freeSpace(path string) *uint64 {
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) != nil || stat.Bsize <= 0 || stat.Bavail > math.MaxUint64/uint64(stat.Bsize) {
		return nil
	}
	available := stat.Bavail * uint64(stat.Bsize)
	return &available
}

func spaceDelta(before, after *uint64) *int64 {
	if before == nil || after == nil {
		return nil
	}
	hi, lo := max(*before, *after), min(*before, *after)
	if hi-lo > math.MaxInt64 {
		return nil
	}
	delta := int64(hi - lo)
	if *after < *before {
		delta = -delta
	}
	return &delta
}

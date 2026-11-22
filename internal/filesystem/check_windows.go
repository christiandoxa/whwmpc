package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"golang.org/x/sys/windows"
)

const windowsVolumePrefix = `\\?\Volume{`

var (
	windowsDrive     = regexp.MustCompile(`^[A-Za-z]:$`)
	errNotLocalNTFS  = errors.New("volume harus NTFS lokal dengan huruf drive tetap")
	errVolumeMapping = errors.New("identitas GUID volume tidak dapat dipastikan")
)

func (v Volume) CheckCandidate() bool {
	return v.Kind == "volume" && v.Filesystem == "ntfs" && windowsDrive.MatchString(v.Device) && uuid.MatchString(v.UUID)
}

func discover(ctx context.Context, _ sysrun.Runner) Snapshot {
	snapshot := Snapshot{Backend: "chkdsk", Availability: domain.Available}
	drives, err := windows.GetLogicalDrives()
	if err != nil {
		snapshot.Availability, snapshot.Detail = availability(sysrun.Result{Err: err}), "Daftar drive Windows tidak tersedia."
		return snapshot
	}
	for letter := byte('A'); letter <= 'Z'; letter++ {
		if ctx.Err() != nil {
			snapshot.Availability, snapshot.Detail = domain.Partial, "Inventaris volume NTFS belum lengkap."
			return snapshot
		}
		if drives&(uint32(1)<<(letter-'A')) == 0 {
			continue
		}
		v, err := windowsNTFSVolume(string(letter) + ":")
		switch {
		case err == nil:
			snapshot.Volumes = append(snapshot.Volumes, v)
		case !errors.Is(err, errNotLocalNTFS):
			snapshot.Availability = domain.Partial
		}
	}
	switch {
	case snapshot.Availability == domain.Partial:
		snapshot.Detail = "Sebagian metadata volume tidak dapat dibaca. Pemeriksaan NTFS memerlukan Administrator."
	case len(snapshot.Volumes) == 0:
		snapshot.Availability, snapshot.Detail = domain.Unsupported, "Tidak ada volume NTFS lokal dengan huruf drive yang terdeteksi."
	default:
		snapshot.Detail = "Pemeriksaan NTFS memerlukan Administrator. Hasil CHKDSK baca-saja pada volume aktif bersifat parsial."
	}
	return snapshot
}

func windowsNTFSVolume(target string) (Volume, error) {
	root, err := windows.UTF16PtrFromString(target + `\`)
	if err != nil {
		return Volume{}, err
	}
	if windows.GetDriveType(root) != windows.DRIVE_FIXED {
		return Volume{}, errNotLocalNTFS
	}
	var name [64]uint16
	if err := windows.GetVolumeNameForVolumeMountPoint(root, &name[0], uint32(len(name))); err != nil {
		return Volume{}, err
	}
	path := windows.UTF16ToString(name[:])
	identity, valid := strings.CutPrefix(path, windowsVolumePrefix)
	identity, suffix := strings.CutSuffix(identity, `}\`)
	if !valid || !suffix || !uuid.MatchString(identity) {
		return Volume{}, errVolumeMapping
	}
	var filesystem [32]uint16
	var flags uint32
	if err := windows.GetVolumeInformation(&name[0], nil, 0, nil, nil, &flags, &filesystem[0], uint32(len(filesystem))); err != nil {
		return Volume{}, err
	}
	if !strings.EqualFold(windows.UTF16ToString(filesystem[:]), "NTFS") {
		return Volume{}, errNotLocalNTFS
	}
	return Volume{Device: target, Kind: "volume", Filesystem: "ntfs", UUID: identity, Mounts: []string{target + `\`},
		ReadOnly: flags&windows.FILE_READ_ONLY_VOLUME != 0}, nil
}

func Check(ctx context.Context, runner sysrun.Runner, target string) CheckResult {
	if runner == nil {
		runner = sysrun.Native{}
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nativeNotStarted(target, domain.Error, "Direktori sistem Windows tidak dapat dipastikan.")
	}
	return checkNTFS(ctx, runner, target, filepath.Join(system, "chkdsk.exe"), windows.GetCurrentProcessToken().IsElevated(), windowsNTFSVolume)
}

func checkNTFS(ctx context.Context, runner sysrun.Runner, target, path string, elevated bool, probe func(string) (Volume, error)) CheckResult {
	if !windowsDrive.MatchString(target) {
		return nativeNotStarted(target, domain.Unsupported, "Pilih huruf drive NTFS dari inventaris, misalnya C:.")
	}
	target = strings.ToUpper(target)
	if !elevated {
		return nativeNotStarted(target, domain.PermissionRequired, "Pemeriksaan NTFS memerlukan sesi Administrator.")
	}
	before, err := probe(target)
	switch {
	case errors.Is(err, errNotLocalNTFS):
		return nativeNotStarted(target, domain.Unsupported, "Pemeriksaan ini memerlukan volume NTFS lokal dengan huruf drive tetap.")
	case errors.Is(err, os.ErrPermission):
		return nativeNotStarted(target, domain.PermissionRequired, "Izin untuk membaca metadata volume diperlukan.")
	case err != nil || !before.CheckCandidate() || before.Device != target:
		return nativeNotStarted(target, domain.Unknown, "Identitas volume NTFS tidak dapat dipastikan.")
	}
	if _, err := runner.LookPath(path); err != nil {
		return nativeNotStarted(target, availability(sysrun.Result{Err: err}), "CHKDSK bawaan Windows tidak tersedia.")
	}
	// Windows volume names identify the checked volume even if its drive letter changes.
	raw := runNativeCheck(ctx, runner, path, windowsVolumePrefix+before.UUID+"}")
	if errors.Is(raw.Err, windows.ERROR_ELEVATION_REQUIRED) {
		raw.Err = os.ErrPermission
	}
	result := interpretNTFSCheck(raw, target)
	if raw.Err != nil {
		return result
	}
	after, err := probe(target)
	if err != nil || !after.CheckCandidate() || after.Device != target || !strings.EqualFold(before.UUID, after.UUID) || before.ReadOnly != after.ReadOnly {
		return CheckResult{Availability: domain.Unknown, State: "target_changed", Device: target, ExitCode: raw.ExitCode, Detail: "Identitas atau kondisi volume NTFS berubah selama pemeriksaan."}
	}
	return result
}

package filesystem

import (
	"context"
	"errors"
	"os"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"golang.org/x/sys/unix"
)

func (v Volume) CheckCandidate() bool {
	return v.Kind == "volume" && v.Filesystem == "apfs" && !v.Locked && apfsDevice.MatchString(v.Device) && uuid.MatchString(v.UUID)
}

func discover(ctx context.Context, runner sysrun.Runner) Snapshot {
	volumes, status, err := readAPFS(ctx, runner, darwinMounts)
	snapshot := Snapshot{Availability: status, Backend: "fsck_apfs"}
	if err != nil {
		snapshot.Detail = err.Error()
		return snapshot
	}
	if len(volumes) == 0 {
		snapshot.Availability, snapshot.Detail = domain.Unsupported, "Tidak ada volume APFS yang terdeteksi."
		return snapshot
	}
	for _, v := range volumes {
		snapshot.Volumes = append(snapshot.Volumes, v.Volume)
	}
	snapshot.Detail = "Pemeriksaan APFS memerlukan izin root. Verifikasi kontainer aktif dapat membuat sistem berhenti merespons sementara."
	return snapshot
}

func darwinMounts() ([]apfsMount, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	if n < 0 || n > 8192 {
		return nil, errors.New("jumlah mount di luar batas pemeriksaan")
	}
	stats := make([]unix.Statfs_t, n+16)
	n, err = unix.Getfsstat(stats, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	if n < 0 || n >= len(stats) {
		return nil, errors.New("daftar mount berubah saat dibaca")
	}
	var mounts []apfsMount
	for _, stat := range stats[:n] {
		if unix.ByteSliceToString(stat.Fstypename[:]) == "apfs" {
			mounts = append(mounts, apfsMount{unix.ByteSliceToString(stat.Mntfromname[:]), unix.ByteSliceToString(stat.Mntonname[:]), stat.Flags&unix.MNT_RDONLY != 0})
		}
	}
	return mounts, nil
}

func Check(ctx context.Context, runner sysrun.Runner, target string) CheckResult {
	if runner == nil {
		runner = sysrun.Native{}
	}
	return checkAPFS(ctx, runner, target, os.Geteuid() == 0, darwinMounts)
}

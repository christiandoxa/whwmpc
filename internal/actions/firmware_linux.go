package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/firmware"
	"golang.org/x/sys/unix"
)

const cabinetOperand = "{cabinet-LVFS-terverifikasi}"

func (p linuxPlatform) firmwareRemote(ctx context.Context, refresh bool) (firmware.Remote, error) {
	remote, err := firmware.OfficialRemote(ctx, p.runner, refresh)
	if err == nil && remote.ChecksumSig == "" {
		// fwupd 2.0.x can report the new metadata mtime before reloading the signature checksum.
		remote.ChecksumSig = firmwareCacheDigest(remote)
	}
	return remote, err
}

func firmwareCacheDigest(remote firmware.Remote) string {
	metadata := filepath.Join("/var/lib/fwupd/metadata/lvfs", filepath.Base(remote.MetadataURI))
	signature := metadata + ".jcat"
	if remote.FilenameCache != metadata || remote.FilenameCacheSig != signature {
		return ""
	}
	metadataInfo, err := protectedPath(metadata, 0, false)
	if err != nil {
		return ""
	}
	signatureInfo, err := protectedPath(signature, 0, false)
	if err != nil {
		return ""
	}
	return readFirmwareCacheDigest(metadata, signature, remote.Mtime, metadataInfo, signatureInfo)
}

func readFirmwareCacheDigest(metadata, signature string, mtime int64, metadataInfo, signatureInfo os.FileInfo) string {
	const maxSignatureBytes = 1 << 20 // FU_ENGINE_MAX_SIGNATURE_SIZE in fwupd.
	signatureMtime := signatureInfo.ModTime().Unix()
	if !metadataInfo.Mode().IsRegular() || !signatureInfo.Mode().IsRegular() || metadataInfo.ModTime().Unix() != mtime ||
		signatureMtime < mtime || signatureMtime > time.Now().Unix() || signatureInfo.Size() <= 0 || signatureInfo.Size() > maxSignatureBytes {
		return ""
	}
	file, err := os.OpenFile(signature, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || identity(opened) != identity(signatureInfo) {
		return ""
	}
	sum := sha256.New()
	size, err := io.Copy(sum, io.LimitReader(file, maxSignatureBytes+1))
	if err != nil || size != signatureInfo.Size() {
		return ""
	}
	currentMetadata, metadataErr := os.Lstat(metadata)
	currentSignature, signatureErr := os.Lstat(signature)
	if metadataErr != nil || signatureErr != nil || identity(currentMetadata) != identity(metadataInfo) || identity(currentSignature) != identity(signatureInfo) {
		return ""
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func busProperty(ctx context.Context, host linuxPlatform, service, path, key string) (string, error) {
	cmd, err := host.runner.LookPath("busctl")
	if err != nil {
		return "", err
	}
	iface := service
	if service == "org.freedesktop.UPower" && path != "/org/freedesktop/UPower" {
		iface += ".Device"
	}
	result := host.runner.Run(ctx, cmd, "--address=unix:path=/run/dbus/system_bus_socket", "--auto-start=no", "get-property", service, path, iface, key)
	if result.Err != nil || result.ExitCode != 0 {
		return "", fmt.Errorf("properti D-Bus %s belum terverifikasi", key)
	}
	return strings.TrimSpace(string(result.Output)), nil
}

func checkFirmwarePower(ctx context.Context, p linuxPlatform) error {
	ac, err := busProperty(ctx, p, "org.freedesktop.UPower", "/org/freedesktop/UPower", "OnBattery")
	if err != nil || ac != "b false" {
		return errors.New("gunakan daya AC yang dapat diverifikasi melalui UPower")
	}
	level, err1 := busProperty(ctx, p, "org.freedesktop.fwupd", "/", "BatteryLevel")
	threshold, err2 := busProperty(ctx, p, "org.freedesktop.fwupd", "/", "BatteryThreshold")
	if err1 != nil || err2 != nil {
		return errors.New("fwupd belum membuktikan prasyarat daya")
	}
	parse := func(s string) (uint64, bool) {
		raw, ok := strings.CutPrefix(s, "u ")
		v, err := strconv.ParseUint(raw, 10, 8)
		return v, ok && err == nil && v <= 101
	}
	charge, validCharge := parse(level)
	minimum, validMin := parse(threshold)
	if !validCharge || !validMin || (charge <= 100 && minimum <= 100 && charge < minimum) {
		return errors.New("daya baterai di bawah persyaratan fwupd atau pembacaan daya tidak valid")
	}
	// fwupd reports 101 for unknown charge. A desktop without a battery can still be on AC.
	if charge == 101 {
		battery, err := busProperty(ctx, p, "org.freedesktop.UPower", "/org/freedesktop/UPower/devices/DisplayDevice", "IsPresent")
		if err != nil || battery != "b false" {
			return errors.New("kapasitas baterai sistem belum dapat diverifikasi")
		}
	}
	return nil
}

func (p linuxPlatform) firmwareState(ctx context.Context, target string) (firmware.Device, firmware.Release, firmware.Remote, string, error) {
	version, err := firmware.BackendVersion(ctx, p.runner)
	if err != nil || !strings.HasPrefix(version, "2.0.") {
		return firmware.Device{}, firmware.Release{}, firmware.Remote{}, version, errors.New("pembaruan capsule memerlukan fwupd 2.0.x")
	}
	remote, err := p.firmwareRemote(ctx, false)
	if err != nil || !remote.Fresh(time.Now()) {
		return firmware.Device{}, firmware.Release{}, remote, version, errors.New("metadata LVFS resmi wajib tersedia dan berumur paling lama tujuh hari")
	}
	trusted, err := busProperty(ctx, p, "org.freedesktop.fwupd", "/", "OnlyTrusted")
	if err != nil || trusted != "b true" {
		return firmware.Device{}, firmware.Release{}, remote, version, errors.New("daemon fwupd harus mewajibkan payload bertanda tangan")
	}
	device, release, err := firmware.Select(firmware.Collect(ctx, p.runner, true), target)
	if err != nil {
		return device, release, remote, version, err
	}
	return device, release, remote, version, checkFirmwarePower(ctx, p)
}

func (p linuxPlatform) firmwareUpdate(ctx context.Context, target string) (prepared, error) {
	id, wanted, err := firmware.SplitTarget(target)
	if err != nil {
		return prepared{}, err
	}
	device, release, remote, version, err := p.firmwareState(ctx, target)
	if err != nil {
		return prepared{}, err
	}
	inv, err := newInvocation("fwupdmgr", firmware.Args("local-install", cabinetOperand, id), false)
	if err != nil {
		return prepared{}, err
	}
	host, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	fingerprint := digest(host, device, release, remote, version, inv.identity())
	return prepared{
		plan: Plan{
			Kind: "firmware-update", Target: target,
			Initial:  fmt.Sprintf("%s %s · %s → %s. Daya AC terverifikasi. Sumber LVFS stabil. SHA-256 CAB %s.", device.Vendor, device.Name, device.Version, wanted, release.SHA256),
			Identity: fingerprint, Operation: inv.spec, Privilege: inv.privilege,
			Impact: "Unduh CAB yang cocok, verifikasi SHA-256 dan tanda tangan payload, lalu jadwalkan capsule UEFI. Restart dilakukan pengguna. Hasil diperiksa sesudah restart.",
			Risk:   "Firmware dapat membuat perangkat gagal boot jika daya terputus. Simpan cadangan dan kunci pemulihan enkripsi sebelum restart. Pemulihan firmware tidak tersedia dari tindakan ini.",
		},
		run: func(ctx context.Context) attempt {
			dir, err := os.MkdirTemp("", "whwmpc-firmware-*")
			if err != nil {
				return attempt{err: err}
			}
			defer os.RemoveAll(dir)
			path := filepath.Join(dir, "capsule.cab")
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return attempt{err: err}
			}
			err = errors.Join(firmware.Download(ctx, release, file), file.Close())
			if err != nil {
				return attempt{err: err}
			}
			if err := firmware.PackageMatches(ctx, p.runner, path, device, release); err != nil {
				return attempt{err: err}
			}
			nowDevice, nowRelease, nowRemote, nowVersion, err := p.firmwareState(ctx, target)
			if err != nil {
				return attempt{err: err}
			}
			nowHost, err := bootIdentity()
			if err != nil || digest(nowHost, nowDevice, nowRelease, nowRemote, nowVersion, inv.identity()) != fingerprint {
				return attempt{err: errors.New("perangkat, backend, atau paket berubah selama unduhan; buat rencana baru")}
			}
			execInv := inv
			execInv.spec = append([]string(nil), inv.spec...)
			for i, arg := range execInv.spec {
				if arg == cabinetOperand {
					execInv.spec[i] = path
				}
			}
			return execInv.run(ctx, 20*time.Minute)
		},
		verify: func(ctx context.Context, step attempt) Result {
			got := firmware.Outcome(ctx, p.runner, target)
			switch got.State {
			case "verified":
				return Result{Status: Verified, Changed: boolPointer(true), Message: "Versi terpasang cocok dan fwupd mencatat hasil berhasil."}
			case "reboot_required":
				return Result{Status: Partial, Message: "fwupd mencatat capsule menunggu restart. Sambungkan AC dan restart sebelum memeriksa versi aktif."}
			case "failed":
				return Result{Status: Failed, Message: withStepError("fwupd mencatat kegagalan untuk versi terpilih", step)}
			default:
				return Result{Status: Unknown, Message: withStepError("Hasil firmware belum terverifikasi. Periksa hasil sebelum mencoba lagi", step)}
			}
		},
	}, nil
}

func (p linuxPlatform) firmwareRefresh(ctx context.Context, target string) (prepared, error) {
	if target != "" && target != firmware.LVFS {
		return prepared{}, errors.New("pilih LVFS stabil untuk refresh")
	}
	remote, err := p.firmwareRemote(ctx, true)
	if err != nil {
		return prepared{}, err
	}
	inv, err := newInvocation("fwupdmgr", firmware.Args("refresh"), false)
	if err != nil {
		return prepared{}, err
	}
	host, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	return prepared{
		plan: Plan{
			Kind: "firmware-refresh", Target: firmware.LVFS, Initial: "Katalog LVFS stabil tersimpan.",
			Identity: digest(host, remote, inv.identity()), Operation: inv.spec, Privilege: inv.privilege,
			Impact: "fwupd dapat mengunduh metadata resmi bertanda tangan melalui jaringan.",
			Risk:   "Permintaan jaringan memperbarui cache metadata sistem. Tidak ada firmware yang dipasang.",
		},
		run: func(ctx context.Context) attempt { return inv.run(ctx, 3*time.Minute) },
		verify: func(ctx context.Context, step attempt) Result {
			after, err := p.firmwareRemote(ctx, true)
			if err != nil || !after.Fresh(time.Now()) {
				return Result{Status: Unknown, Message: withStepError("Metadata LVFS resmi belum terverifikasi", step)}
			}
			changed := after.ChecksumSig != remote.ChecksumSig || after.Mtime != remote.Mtime
			if changed || step.err == nil {
				return Result{Status: Verified, Changed: &changed, Message: "Metadata LVFS resmi tersedia dan baru diperiksa."}
			}
			return Result{Status: Partial, Changed: &changed, Message: withStepError("Metadata cache masih dapat dipakai; refresh baru belum terbukti", step)}
		},
	}, nil
}

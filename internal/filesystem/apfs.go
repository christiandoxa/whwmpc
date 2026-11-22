package filesystem

import (
	"context"
	"encoding/xml"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const (
	diskutilPath = "/usr/sbin/diskutil"
	fsckAPFSPath = "/sbin/fsck_apfs"
)

var (
	apfsDevice  = regexp.MustCompile(`^/dev/disk[0-9]{1,8}s[0-9]{1,8}$`)
	apfsMountID = regexp.MustCompile(`^(/dev/disk[0-9]{1,8}s[0-9]{1,8})(?:s[0-9]{1,8})?$`)
)

type apfsVolume struct {
	Volume
	containerUUID string
	live          bool
}

type apfsMount struct {
	source, path string
	readOnly     bool
}

type apfsPlist struct {
	XMLName xml.Name
	Text    string      `xml:",chardata"`
	Values  []apfsPlist `xml:",any"`
}

func (p apfsPlist) fields() (map[string]apfsPlist, error) {
	if p.XMLName.Local != "dict" || len(p.Values)%2 != 0 {
		return nil, errors.New("kamus plist APFS tidak lengkap")
	}
	fields := make(map[string]apfsPlist, len(p.Values)/2)
	for i := 0; i < len(p.Values); i += 2 {
		key, value := p.Values[i], p.Values[i+1]
		if _, exists := fields[key.Text]; exists || key.XMLName.Local != "key" || key.Text == "" || value.XMLName.Local == "key" {
			return nil, errors.New("entri plist APFS tidak valid")
		}
		fields[key.Text] = value
	}
	return fields, nil
}

func (p apfsPlist) stringValue() string {
	if p.XMLName.Local != "string" {
		return ""
	}
	return p.Text
}

func parseAPFSVolumes(data []byte, mounts []apfsMount) ([]apfsVolume, error) {
	var plist apfsPlist
	if err := xml.Unmarshal(data, &plist); err != nil || plist.XMLName.Local != "plist" || len(plist.Values) != 1 {
		return nil, errors.New("plist inventaris APFS tidak valid")
	}
	root, err := plist.Values[0].fields()
	if err != nil || root["Containers"].XMLName.Local != "array" {
		return nil, errors.New("daftar kontainer APFS tidak tersedia")
	}
	byDevice := make(map[string][]apfsMount)
	for _, mount := range mounts {
		source := mount.source
		if _, disk, ok := strings.Cut(source, "@/dev/"); ok {
			source = "/dev/" + disk
		}
		m := apfsMountID.FindStringSubmatch(source)
		if m == nil || !strings.HasPrefix(mount.path, "/") {
			return nil, errors.New("identitas mount APFS tidak dapat dipastikan")
		}
		byDevice[m[1]] = append(byDevice[m[1]], mount)
	}
	volumes := make([]apfsVolume, 0)
	seen := make(map[string]bool)
	for _, row := range root["Containers"].Values {
		container, err := row.fields()
		identity := container["APFSContainerUUID"].stringValue()
		if err != nil || !uuid.MatchString(identity) || container["Volumes"].XMLName.Local != "array" {
			return nil, errors.New("inventaris kontainer APFS tidak lengkap")
		}
		start, live := len(volumes), false
		for _, entry := range container["Volumes"].Values {
			fields, err := entry.fields()
			device := "/dev/" + fields["DeviceIdentifier"].stringValue()
			id := fields["APFSVolumeUUID"].stringValue()
			locked := fields["Locked"].XMLName.Local
			if err != nil || !apfsDevice.MatchString(device) || !uuid.MatchString(id) || seen[device] ||
				(locked != "true" && locked != "false") {
				return nil, errors.New("identitas volume APFS tidak dapat dipastikan")
			}
			seen[device] = true
			v := Volume{Device: device, Kind: "volume", Filesystem: "apfs", UUID: id, Locked: locked == "true"}
			deviceMounts := byDevice[device]
			v.ReadOnly = len(deviceMounts) > 0
			for _, mount := range deviceMounts {
				v.Mounts = append(v.Mounts, domain.SafeText(mount.path))
				v.ReadOnly = v.ReadOnly && mount.readOnly
			}
			slices.Sort(v.Mounts)
			live = live || v.Mounted()
			volumes = append(volumes, apfsVolume{Volume: v, containerUUID: identity})
			delete(byDevice, device)
		}
		for i := start; i < len(volumes); i++ {
			volumes[i].live = live
		}
	}
	if len(byDevice) != 0 {
		return nil, errors.New("inventaris APFS dan daftar mount tidak konsisten")
	}
	return volumes, nil
}

func readAPFS(ctx context.Context, runner sysrun.Runner, listMounts func() ([]apfsMount, error)) ([]apfsVolume, domain.Availability, error) {
	if _, err := runner.LookPath(diskutilPath); err != nil {
		return nil, availability(sysrun.Result{Err: err}), errors.New("diskutil bawaan macOS tidak tersedia")
	}
	result := runner.Run(ctx, diskutilPath, "apfs", "list", "-plist")
	if status := availability(result); status != domain.Available {
		return nil, status, errors.New("inventaris volume APFS tidak tersedia")
	}
	mounts, err := listMounts()
	if err != nil {
		return nil, domain.Unknown, errors.New("daftar mount APFS tidak dapat dipastikan")
	}
	volumes, err := parseAPFSVolumes(result.Output, mounts)
	if err != nil {
		return nil, domain.Unknown, err
	}
	return volumes, domain.Available, nil
}

func checkAPFS(ctx context.Context, runner sysrun.Runner, target string, elevated bool, listMounts func() ([]apfsMount, error)) CheckResult {
	if !apfsDevice.MatchString(target) {
		return nativeNotStarted(target, domain.Unsupported, "Pilih path volume APFS dari inventaris, misalnya /dev/disk3s1.")
	}
	if !elevated {
		return nativeNotStarted(target, domain.PermissionRequired, "Pemeriksaan volume APFS memerlukan izin root.")
	}
	volumes, status, err := readAPFS(ctx, runner, listMounts)
	if err != nil {
		return nativeNotStarted(target, status, err.Error())
	}
	before, found := findAPFSVolume(volumes, target)
	if !found {
		return nativeNotStarted(target, domain.Unsupported, "Volume APFS ini tidak terlihat dalam inventaris.")
	}
	if before.Locked {
		return nativeNotStarted(target, domain.PermissionRequired, "Volume APFS terkunci.")
	}
	if _, err := runner.LookPath(fsckAPFSPath); err != nil {
		return nativeNotStarted(target, availability(sysrun.Result{Err: err}), "fsck_apfs bawaan macOS tidak tersedia.")
	}
	args := []string{"-n"}
	if before.live {
		args = append(args, "-l")
	}
	raw := runNativeCheck(ctx, runner, fsckAPFSPath, append(args, "/dev/r"+strings.TrimPrefix(target, "/dev/"))...)
	result := interpretAPFSCheck(raw, target, before.live)
	if raw.Err != nil {
		return result
	}
	volumes, _, err = readAPFS(ctx, runner, listMounts)
	after, found := findAPFSVolume(volumes, target)
	if err != nil || !found || before.UUID != after.UUID || before.containerUUID != after.containerUUID ||
		before.live != after.live || after.Locked || before.ReadOnly != after.ReadOnly || !slices.Equal(before.Mounts, after.Mounts) {
		return CheckResult{Availability: domain.Unknown, State: "target_changed", Device: target, ExitCode: raw.ExitCode, Detail: "Identitas atau keadaan mount APFS tidak dapat dicocokkan setelah pemeriksaan."}
	}
	return result
}

func findAPFSVolume(volumes []apfsVolume, target string) (apfsVolume, bool) {
	for _, v := range volumes {
		if v.Device == target {
			return v, true
		}
	}
	return apfsVolume{}, false
}

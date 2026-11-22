// Package firmware inspects Linux fwupd inventory and the stable LVFS catalog.
package firmware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const LVFS = "lvfs"

var (
	deviceID       = regexp.MustCompile(`^[a-f0-9]{40}$`)
	releaseBackend = regexp.MustCompile(`^2\.0\.[0-9]+$`)
	version        = regexp.MustCompile(`^[0-9][a-zA-Z0-9.+_-]{0,79}$`)
	checksum       = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
	cabName        = regexp.MustCompile(`^[a-zA-Z0-9_.+-]+\.[cC][aA][bB]$`)
)

type Snapshot struct {
	Availability domain.Availability `json:"availability"`
	Updates      domain.Availability `json:"updates_availability,omitempty"`
	Backend      string              `json:"backend,omitempty"`
	Devices      []Device            `json:"devices"`
	Detail       string              `json:"detail,omitempty"`
}

type Device struct {
	ID          string    `json:"DeviceId"`
	Name        string    `json:"Name,omitempty"`
	Vendor      string    `json:"Vendor,omitempty"`
	Plugin      string    `json:"Plugin,omitempty"`
	Version     string    `json:"Version,omitempty"`
	Flags       []string  `json:"Flags,omitempty"`
	GUIDs       []string  `json:"Guid,omitempty"`
	Releases    []Release `json:"Releases,omitempty"`
	UpdateState uint      `json:"UpdateState,omitempty"`
}

type Release struct {
	Version   string   `json:"Version"`
	Remote    string   `json:"RemoteId"`
	Checksums []string `json:"Checksum,omitempty"`
	Locations []string `json:"Locations,omitempty"`
	Flags     []string `json:"Flags,omitempty"`
	SHA256    string   `json:"-"`
	URL       string   `json:"-"`
}

type Remote struct {
	ID               string `json:"Id"`
	Kind             string
	Enabled          bool
	MetadataURI      string `json:"MetadataUri"`
	SignatureURI     string `json:"MetadataUriSig"`
	FirmwareBaseURI  string `json:"FirmwareBaseUri"`
	ChecksumSig      string
	Mtime            int64
	FilenameCache    string `json:"-"`
	FilenameCacheSig string `json:"-"`
}

type Verification struct {
	Availability domain.Availability `json:"availability"`
	Target       string              `json:"target"`
	Installed    string              `json:"installed,omitempty"`
	State        string              `json:"state"`
}

func Args(command string, values ...string) []string {
	return append([]string{"--json", "--no-metadata-check", "--no-unreported-check", "--no-remote-check", "--no-reboot-check", command}, values...)
}

func query(ctx context.Context, run sysrun.Runner, arg []string) sysrun.Result {
	if run == nil {
		run = sysrun.Native{}
	}
	path, err := run.LookPath("fwupdmgr")
	if err != nil {
		return sysrun.Result{ExitCode: -1, Err: errors.Join(os.ErrNotExist, err)}
	}
	return run.Run(ctx, path, arg...)
}

func availability(r sysrun.Result) domain.Availability {
	switch {
	case errors.Is(r.Err, os.ErrNotExist):
		return domain.DependencyMissing
	case errors.Is(r.Err, os.ErrPermission):
		return domain.PermissionRequired
	case r.Err != nil:
		return domain.Error
	case r.ExitCode != 0:
		message := strings.ToLower(string(r.Output) + string(r.Stderr))
		if strings.Contains(message, "permission denied") || strings.Contains(message, "not authorized") || strings.Contains(message, "auth-failed") ||
			strings.Contains(message, "access denied") {
			return domain.PermissionRequired
		}
		return domain.Error
	default:
		return domain.Available
	}
}

func decodeDevices(data []byte) ([]Device, error) {
	var raw struct{ Devices *[]Device }
	if err := json.Unmarshal(data, &raw); err != nil || raw.Devices == nil {
		return nil, errors.New("fwupdmgr tidak mengembalikan inventaris JSON yang valid")
	}
	devices := *raw.Devices
	seen := make(map[string]bool, len(devices))
	for i := range devices {
		d := &devices[i]
		if !deviceID.MatchString(d.ID) || seen[d.ID] {
			return nil, errors.New("identitas perangkat fwupd kosong, duplikat, atau tidak valid")
		}
		seen[d.ID] = true
		d.Name, d.Vendor, d.Plugin, d.Version = domain.SafeText(d.Name), domain.SafeText(d.Vendor), domain.SafeText(d.Plugin), domain.SafeText(d.Version)
		for j := range d.Releases {
			r := &d.Releases[j]
			if !version.MatchString(r.Version) {
				return nil, errors.New("versi firmware dari fwupd tidak valid")
			}
			r.SHA256, r.URL = usablePackage(*r)
		}
	}
	return devices, nil
}

// Collect uses cached metadata only; querying the catalog never implies up to date.
func Collect(ctx context.Context, run sysrun.Runner, updates bool) Snapshot {
	if runtime.GOOS != "linux" {
		return Snapshot{Availability: domain.Unsupported}
	}
	s := Snapshot{Backend: "fwupd"}
	r := query(ctx, run, Args("get-devices"))
	s.Availability = availability(r)
	if s.Availability != domain.Available {
		s.Detail = "Inventaris firmware fwupdmgr tidak tersedia."
		return s
	}
	devices, err := decodeDevices(r.Output)
	if err != nil {
		s.Availability, s.Detail = domain.Error, err.Error()
		return s
	}
	s.Devices = devices
	if !updates {
		return s
	}
	r = query(ctx, run, Args("get-updates"))
	s.Updates = availability(r)
	if s.Updates != domain.Available {
		s.Detail = "Katalog tersimpan tidak dapat dibaca. Inventaris tetap tersedia."
		return s
	}
	upgrades, err := decodeDevices(r.Output)
	if err != nil {
		s.Updates, s.Detail = domain.Error, err.Error()
		return s
	}
	for i := range s.Devices {
		d := &s.Devices[i]
		match := slices.IndexFunc(upgrades, func(u Device) bool { return d.ID == u.ID && d.Version == u.Version && sameGUIDs(d.GUIDs, u.GUIDs) })
		if match >= 0 {
			d.Releases = upgrades[match].Releases
		}
	}
	// fwupdmgr can omit a device on a GetUpgrades error; an empty list is not a clean bill.
	if len(upgrades) == 0 {
		s.Updates = domain.Partial
		s.Detail = "Katalog tersimpan tidak menampilkan kandidat. Cakupan pembaruan perangkat belum dikonfirmasi."
	}
	return s
}

func sameGUIDs(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

func SplitTarget(target string) (id, wanted string, err error) {
	id, wanted, found := strings.Cut(target, "@")
	if !found || !deviceID.MatchString(id) || !version.MatchString(wanted) {
		return "", "", errors.New("target perlu DeviceId fwupd lengkap dan versi, dalam bentuk ID@VERSI")
	}
	return id, wanted, nil
}

func usablePackage(r Release) (string, string) {
	if r.Remote != LVFS || !slices.Contains(r.Flags, "trusted-metadata") || !slices.Contains(r.Flags, "is-upgrade") ||
		slices.Contains(r.Flags, "is-alternate-branch") || slices.Contains(r.Flags, "is-downgrade") ||
		slices.Contains(r.Flags, "blocked-version") || slices.Contains(r.Flags, "blocked-approval") {
		return "", ""
	}
	var digest string
	for _, c := range r.Checksums {
		if !checksum.MatchString(c) {
			continue
		}
		c = strings.ToLower(c)
		if digest != "" && digest != c {
			return "", ""
		}
		digest = c
	}
	for _, loc := range r.Locations {
		if cabName.MatchString(loc) {
			loc = "https://fwupd.org/downloads/" + loc
		}
		if digest != "" && officialURI(loc, true) {
			return digest, loc
		}
	}
	return "", ""
}

func officialURI(value string, isCAB bool) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || (u.Host != "cdn.fwupd.org" && u.Host != "fwupd.org") ||
		u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.RawPath != "" {
		return false
	}
	tail, ok := strings.CutPrefix(u.Path, "/downloads/")
	if !ok || tail == "" || strings.Contains(tail, "/") {
		return false
	}
	if isCAB {
		return cabName.MatchString(tail)
	}
	switch tail {
	case "firmware.xml.gz", "firmware.xml.xz", "firmware.xml.zst", "firmware.xml.gz.jcat", "firmware.xml.xz.jcat", "firmware.xml.zst.jcat":
		return true
	}
	return false
}

func (d Device) CanUpdate(r Release) bool {
	return d.Plugin == "uefi_capsule" && d.Version != "" && d.Version != r.Version && len(d.GUIDs) > 0 &&
		slices.Contains(d.Flags, "supported") && slices.Contains(d.Flags, "updatable") &&
		!slices.Contains(d.Flags, "locked") && !slices.Contains(d.Flags, "emulated") &&
		!slices.Contains(d.Flags, "needs-activation") && d.UpdateState != 1 && d.UpdateState != 4 && r.SHA256 != "" && r.URL != ""
}

func Select(snapshot Snapshot, target string) (Device, Release, error) {
	id, wanted, err := SplitTarget(target)
	if err != nil {
		return Device{}, Release{}, err
	}
	if snapshot.Availability != domain.Available || snapshot.Updates != domain.Available {
		return Device{}, Release{}, errors.New("inventaris atau katalog pembaruan tersimpan tidak tersedia")
	}
	for _, d := range snapshot.Devices {
		if d.ID != id {
			continue
		}
		var selected *Release
		for _, r := range d.Releases {
			if wanted != r.Version || !d.CanUpdate(r) {
				continue
			}
			if selected != nil && (selected.SHA256 != r.SHA256 || selected.URL != r.URL) {
				return Device{}, Release{}, errors.New("lebih dari satu paket berbeda untuk versi ini")
			}
			selected = &r
		}
		if selected != nil {
			return d, *selected, nil
		}
		return Device{}, Release{}, errors.New("rilis tidak memenuhi syarat UEFI capsule dan paket LVFS resmi")
	}
	return Device{}, Release{}, errors.New("perangkat firmware terpilih tidak lagi terdeteksi")
}

func BackendVersion(ctx context.Context, run sysrun.Runner) (string, error) {
	r := query(ctx, run, Args("--version"))
	var doc struct {
		Versions []struct{ Type, AppstreamID, Version string }
	}
	if availability(r) != domain.Available || json.Unmarshal(r.Output, &doc) != nil {
		return "", errors.New("versi daemon fwupd belum terverifikasi")
	}
	for _, entry := range doc.Versions {
		if entry.Type == "runtime" && entry.AppstreamID == "org.freedesktop.fwupd" && releaseBackend.MatchString(entry.Version) {
			return entry.Version, nil
		}
	}
	return "", errors.New("versi runtime fwupd tidak tersedia")
}

func OfficialRemote(ctx context.Context, run sysrun.Runner, refresh bool) (Remote, error) {
	r := query(ctx, run, Args("get-remotes"))
	var doc struct {
		Remotes []struct {
			Remote
			FilenameCache, FilenameCacheSig string
		}
	}
	if availability(r) != domain.Available || json.Unmarshal(r.Output, &doc) != nil {
		return Remote{}, errors.New("konfigurasi remote fwupd belum terverifikasi")
	}
	var result Remote
	for _, item := range doc.Remotes {
		remote := item.Remote
		if !remote.Enabled || remote.Kind != "download" {
			continue
		}
		if remote.ID != LVFS {
			if refresh {
				return Remote{}, errors.New("refresh hanya tersedia saat LVFS stabil adalah satu-satunya remote unduhan aktif")
			}
			continue
		}
		if result.ID != "" {
			return Remote{}, errors.New("remote LVFS ditemukan lebih dari satu kali")
		}
		remote.FilenameCache, remote.FilenameCacheSig = item.FilenameCache, item.FilenameCacheSig
		result = remote
	}
	if result.ID == "" || !officialURI(result.MetadataURI, false) ||
		!officialURI(result.SignatureURI, false) || result.SignatureURI != result.MetadataURI+".jcat" ||
		result.FirmwareBaseURI != "https://fwupd.org/downloads" {
		return Remote{}, errors.New("metadata dan tanda tangan LVFS stabil tidak menggunakan sumber resmi")
	}
	return result, nil
}

func (r Remote) Fresh(now time.Time) bool {
	at := time.Unix(r.Mtime, 0)
	return checksum.MatchString(r.ChecksumSig) && r.Mtime > 0 && !at.After(now) && now.Sub(at) <= 7*24*time.Hour
}

func Outcome(ctx context.Context, run sysrun.Runner, target string) Verification {
	verification := Verification{Target: target, State: "unknown", Availability: domain.Unknown}
	id, wanted, err := SplitTarget(target)
	if err != nil {
		verification.Availability = domain.Error
		return verification
	}
	catalog := Collect(ctx, run, false)
	verification.Availability = catalog.Availability
	for _, d := range catalog.Devices {
		if d.ID == id && !slices.Contains(d.Flags, "emulated") {
			verification.Installed = d.Version
		}
	}
	if catalog.Availability != domain.Available {
		return verification
	}
	if verification.Installed == "" {
		verification.Availability = domain.Unknown
		return verification
	}
	// fwupd's get-results only returns terminal outcomes. get-history also exposes a staged capsule.
	r := query(ctx, run, Args("get-history"))
	status := availability(r)
	if status == domain.PermissionRequired || status == domain.DependencyMissing {
		verification.Availability = status
		return verification
	}
	verification.Availability = domain.Unknown
	var history struct{ Devices []Device }
	if status != domain.Available || json.Unmarshal(r.Output, &history) != nil {
		return verification
	}
	var record *Device
	for i := range history.Devices {
		item := &history.Devices[i]
		if item.ID != id {
			continue
		}
		if record != nil {
			return verification
		}
		record = item
	}
	// A historical device Version is the previous version, and its release holds the attempted one.
	if record == nil || len(record.Releases) != 1 || record.Releases[0].Version != wanted {
		return verification
	}
	switch record.UpdateState {
	case 1, 4: // FWUPD_UPDATE_STATE_PENDING and NEEDS_REBOOT
		verification.State, verification.Availability = "reboot_required", domain.Partial
	case 3, 5: // FWUPD_UPDATE_STATE_FAILED and FAILED_TRANSIENT
		verification.State, verification.Availability = "failed", domain.Error
	case 2: // FWUPD_UPDATE_STATE_SUCCESS
		if verification.Installed == wanted {
			verification.State, verification.Availability = "verified", domain.Available
			break
		}
		verification.Availability = domain.Partial
	}
	return verification
}

func (v Verification) Detail() string {
	return fmt.Sprintf("%s · versi berjalan %s", v.State, v.Installed)
}

// Package filesystem discovers offline candidates for Linux e2fsprogs.
package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

var (
	devicePath    = regexp.MustCompile(`^/dev/[A-Za-z0-9_-]{1,64}$`)
	e2fsckVersion = regexp.MustCompile(`(?m)^e2fsck (1\.47\.[0-9]+)(?:[[:space:]-]|$)`)
	numericID     = regexp.MustCompile(`^[0-9]{1,10}:[0-9]{1,10}$`)
	uuid          = regexp.MustCompile(`^[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$`)
)

type Snapshot struct {
	Availability domain.Availability `json:"availability"`
	Backend      string              `json:"backend,omitempty"`
	Volumes      []Volume            `json:"volumes"`
	Detail       string              `json:"detail,omitempty"`
}

type Volume struct {
	Device     string   `json:"name"`
	ID         string   `json:"maj:min"`
	Kind       string   `json:"type"`
	Filesystem string   `json:"fstype"`
	UUID       string   `json:"uuid,omitempty"`
	ReadOnly   bool     `json:"ro"`
	Mounts     []string `json:"mountpoints,omitempty"`
	Size       uint64   `json:"size"`
}

type CheckResult struct {
	Availability domain.Availability `json:"availability"`
	State        string              `json:"state"`
	Device       string              `json:"device,omitempty"`
	ExitCode     int                 `json:"exit_code"`
	Detail       string              `json:"detail,omitempty"`
}

func ExtFamily(fs string) bool { return fs == "ext2" || fs == "ext3" || fs == "ext4" }

func (v Volume) Mounted() bool {
	for _, mount := range v.Mounts {
		if mount != "" {
			return true
		}
	}
	return false
}

func (v Volume) OfflineCandidate() bool {
	return v.Kind == "part" && ExtFamily(v.Filesystem) && devicePath.MatchString(v.Device) &&
		uuid.MatchString(v.UUID) && !v.Mounted()
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
		return domain.Error
	default:
		return domain.Available
	}
}

func Discover(ctx context.Context, runner sysrun.Runner) Snapshot {
	if runtime.GOOS != "linux" {
		return Snapshot{Availability: domain.Unsupported}
	}
	if runner == nil {
		runner = sysrun.Native{}
	}
	path, err := runner.LookPath("lsblk")
	if err != nil {
		return Snapshot{Availability: domain.DependencyMissing, Detail: "lsblk dari util-linux tidak tersedia."}
	}
	got := runner.Run(ctx, path, "--json", "--paths", "--list", "--bytes", "--output", "NAME,MAJ:MIN,TYPE,FSTYPE,UUID,RO,MOUNTPOINTS,SIZE")
	s := Snapshot{Availability: availability(got), Backend: "e2fsprogs"}
	if s.Availability != domain.Available {
		s.Detail = "Inventaris volume lsblk tidak tersedia."
		return s
	}
	var data struct {
		Volumes *[]Volume `json:"blockdevices"`
	}
	if err := json.Unmarshal(got.Output, &data); err != nil || data.Volumes == nil {
		s.Availability, s.Detail = domain.Error, "JSON inventaris volume lsblk tidak lengkap."
		return s
	}
	seen := make(map[string]bool)
	for _, v := range *data.Volumes {
		if !devicePath.MatchString(v.Device) || !numericID.MatchString(v.ID) || seen[v.ID] {
			s.Availability = domain.Partial
			continue
		}
		seen[v.ID] = true
		v.Kind, v.Filesystem = domain.SafeText(v.Kind), domain.SafeText(v.Filesystem)
		for i, mount := range v.Mounts {
			v.Mounts[i] = domain.SafeText(mount)
		}
		s.Volumes = append(s.Volumes, v)
	}
	return s
}

func (s Snapshot) Select(target string) (Volume, error) {
	if !devicePath.MatchString(target) {
		return Volume{}, errors.New("gunakan path /dev yang kanonik, langsung, dan terdeteksi")
	}
	for _, v := range s.Volumes {
		if v.Device != target {
			continue
		}
		switch {
		case v.Mounted():
			return Volume{}, errors.New("volume terpasang; lepas dari sistem terlebih dahulu atau boot media pemulihan untuk memeriksa root")
		case !v.OfflineCandidate():
			return Volume{}, errors.New("pemeriksaan memerlukan partisi ext2/3/4 biasa dan UUID terdeteksi")
		default:
			return v, nil
		}
	}
	return Volume{}, errors.New("volume terpilih tidak ada dalam inventaris")
}

var (
	envArgs    = []string{"E2FSCK_CONFIG=/dev/null", "E2FSPROGS_UNDO_DIR=none"}
	checkArgs  = []string{"-f", "-n", "--"}
	repairArgs = []string{"-p", "-E", "fixes_only,nodiscard", "--"}
)

// E2fsckArgs silences local log and undo directives and pins the semantic mode.
func E2fsckArgs(path, device string, repair bool) []string {
	args := append(append([]string(nil), envArgs...), path)
	mode := checkArgs
	if repair {
		mode = repairArgs
	}
	return append(append(args, mode...), device)
}

// CheckVersion gates the command arguments and output interpretation to e2fsprogs 1.47.x.
func CheckVersion(ctx context.Context, runner sysrun.Runner, path string) (string, error) {
	result := runner.Run(ctx, path, "-V")
	parts := e2fsckVersion.FindStringSubmatch(string(result.Stderr) + "\n" + string(result.Output))
	if result.Err != nil || result.ExitCode != 0 || len(parts) != 2 {
		return "", errors.New("versi e2fsck tidak terverifikasi: memerlukan e2fsprogs 1.47.x")
	}
	return parts[1], nil
}

func Interpret(r sysrun.Result, target string) CheckResult {
	check := CheckResult{Availability: availability(r), Device: target, State: "unknown", ExitCode: r.ExitCode}
	text := strings.ToLower(string(r.Stderr) + string(r.Output))
	if strings.Contains(text, "permission denied") || strings.Contains(text, "not authorized") || strings.Contains(text, "password is required") {
		check.Availability = domain.PermissionRequired
		return check
	}
	switch {
	case r.Err != nil:
		check.Detail = "Pemeriksaan e2fsck tidak menghasilkan hasil terminal yang valid."
	case strings.Contains(text, "is mounted") || strings.Contains(text, "is in use"):
		check.Availability, check.State = domain.Unknown, "mount_changed"
		check.Detail = "e2fsck melaporkan volume terpasang atau sedang dipakai."
	case strings.Contains(text, "skipping journal recovery"):
		check.Availability, check.State = domain.Partial, "journal_recovery_needed"
		check.Detail = "Jurnal perlu replay; pemeriksaan read-only belum dapat menyimpulkan keadaan filesystem."
	case r.ExitCode == 4 || (r.ExitCode == 0 && (strings.Contains(text, "fix? no") ||
		strings.Contains(text, "clear? no") || strings.Contains(text, "filesystem still has errors"))):
		check.Availability, check.State = domain.Available, "errors_detected"
	case r.ExitCode == 0:
		check.State = "clean"
	default:
		check.Detail = "Hasil read-only belum dapat diverifikasi."
	}
	return check
}

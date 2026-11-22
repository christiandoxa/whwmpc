package filesystem

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"golang.org/x/sys/unix"
)

type Identity struct {
	DiskSequence uint64
	DeviceNumber uint64
	Inode        uint64
}

// Pin opens the exact block device for subsequent check/repair by an fd operand.
func Pin(v Volume) (*os.File, Identity, error) {
	if !v.OfflineCandidate() {
		return nil, Identity{}, errors.New("partisi belum memenuhi prasyarat e2fsck")
	}
	if err := requireOffline(v.ID); err != nil {
		return nil, Identity{}, err
	}
	base := "/sys/dev/block/" + v.ID
	node, err := filepath.EvalSymlinks(base)
	if err != nil || !strings.HasPrefix(node, "/sys/devices/") {
		return nil, Identity{}, errors.New("identitas sysfs partition tidak tersedia")
	}
	part, err := readSmall(filepath.Join(node, "partition"))
	if err != nil || part == "" {
		return nil, Identity{}, errors.New("target bukan partisi sysfs biasa")
	}
	pin, err := os.OpenFile(v.Device, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, Identity{}, err
	}
	id, err := pinIdentity(pin, v.ID)
	var seq string
	if err == nil {
		seq, err = readSmall(filepath.Join(filepath.Dir(node), "diskseq"))
		if err == nil && strconv.FormatUint(id.DiskSequence, 10) != seq {
			err = errors.New("disk berubah setelah inventaris")
		}
	}
	if err == nil {
		parent := filepath.Dir(node)
		err = requireNoHolders(node, parent)
	}
	if err != nil {
		pin.Close()
		return nil, Identity{}, err
	}
	return pin, id, nil
}

func pinIdentity(file *os.File, expected string) (Identity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return Identity{}, err
	}
	num := uint64(stat.Rdev)
	if stat.Mode&unix.S_IFMT != unix.S_IFBLK || stat.Uid != 0 || stat.Mode&0002 != 0 ||
		fmt.Sprintf("%d:%d", unix.Major(num), unix.Minor(num)) != expected {
		return Identity{}, errors.New("node perangkat tidak cocok dengan partisi terpilih")
	}
	var seq uint64
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, file.Fd(), unix.BLKGETDISKSEQ, uintptr(unsafe.Pointer(&seq))); errno != 0 || seq == 0 {
		return Identity{}, errors.New("kernel tidak dapat memverifikasi nomor urut perangkat blok")
	}
	return Identity{seq, num, stat.Ino}, nil
}

// Operand gives e2fsck a pinned block inode rather than reopening the /dev name.
func Operand(f *os.File) string {
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), f.Fd())
}

// ExclusiveRead refuses a block volume claimed by a mount in any namespace.
func ExclusiveRead(file *os.File) (*os.File, error) {
	return os.OpenFile(Operand(file), os.O_RDONLY|unix.O_NONBLOCK|unix.O_EXCL, 0)
}

func requireOffline(id string) error {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return errors.New("status mount kernel tidak dapat diperiksa")
	}
	defer f.Close()
	return parseMounts(f, id)
}

func parseMounts(file io.Reader, id string) error {
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 4096), 128<<10)
	lines := 0
	for scan.Scan() {
		line := scan.Text()
		fields := strings.Fields(line)
		if len(fields) < 10 || !numericID.MatchString(fields[2]) || !strings.Contains(line, " - ") {
			return errors.New("status mount kernel tidak lengkap")
		}
		lines++
		if fields[2] == id {
			return errors.New("partisi sedang terpasang; tidak dapat diperiksa atau diperbaiki")
		}
	}
	if scan.Err() != nil || lines == 0 {
		return errors.New("status mount kernel tidak lengkap")
	}
	return nil
}

func requireNoHolders(part, disk string) error {
	for _, node := range []string{part, disk} {
		entries, err := os.ReadDir(filepath.Join(node, "holders"))
		if err != nil || len(entries) > 0 {
			return errors.New("partisi atau disk induk mempunyai holder aktif atau tidak dapat dipastikan")
		}
	}
	return nil
}

func readSmall(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 128))
	return strings.TrimSpace(string(b)), err
}

func Probe(ctx context.Context, runner sysrun.Runner, v Volume, operand string) error {
	path, err := runner.LookPath("blkid")
	if err != nil {
		return errors.Join(os.ErrNotExist, err)
	}
	r := runner.Run(ctx, path, "--probe", "--output", "export", "--", operand)
	if r.Err != nil {
		return r.Err
	}
	if r.ExitCode != 0 {
		return errors.New("blkid tidak dapat memverifikasi filesystem dari metadata langsung pada perangkat")
	}
	id, kind := "", ""
	for _, line := range strings.Split(string(r.Output), "\n") {
		k, val, _ := strings.Cut(line, "=")
		switch k {
		case "UUID":
			if id != "" {
				return errors.New("UUID ambigu pada hasil blkid")
			}
			id = val
		case "TYPE":
			if kind != "" {
				return errors.New("tipe ambigu pada hasil blkid")
			}
			kind = val
		}
	}
	if !uuid.MatchString(id) || !strings.EqualFold(id, v.UUID) || kind != v.Filesystem {
		return errors.New("UUID atau tipe filesystem berubah; periksa kembali target")
	}
	return nil
}

// Check runs an offline, read-only check. Its pinned fd remains open until e2fsck exits.
func Check(ctx context.Context, runner sysrun.Runner, target string) CheckResult {
	if runner == nil {
		runner = sysrun.Native{}
	}
	s := Discover(ctx, runner)
	v, err := s.Select(target)
	if err != nil {
		status := s.Availability
		if status == domain.Available {
			status = domain.Unsupported
		}
		return CheckResult{Availability: status, Device: target, State: "not_started", ExitCode: -1, Detail: err.Error()}
	}
	tool, err := runner.LookPath("e2fsck")
	if err != nil {
		return unavailableCheck(target, errors.Join(os.ErrNotExist, err))
	}
	if _, err := CheckVersion(ctx, runner, tool); err != nil {
		return CheckResult{Availability: domain.Unsupported, State: "not_started", Device: target, ExitCode: -1, Detail: err.Error()}
	}
	env, err := runner.LookPath("env")
	if err != nil {
		return unavailableCheck(target, errors.Join(os.ErrNotExist, err))
	}
	file, _, err := Pin(v)
	if err != nil {
		return unavailableCheck(target, err)
	}
	defer file.Close()
	operand := Operand(file)
	if err := Probe(ctx, runner, v, operand); err != nil {
		return unavailableCheck(target, err)
	}
	locked, err := ExclusiveRead(file)
	if err != nil {
		return unavailableCheck(target, err)
	}
	defer locked.Close()
	checker := runner
	if native, ok := runner.(sysrun.Native); ok && native.Timeout == 0 {
		checker = sysrun.Native{Timeout: 2 * time.Minute}
	}
	result := Interpret(checker.Run(ctx, env, E2fsckArgs(tool, operand, false)...), target)
	if err := requireOffline(v.ID); err != nil {
		return CheckResult{Availability: domain.Unknown, State: "mount_changed", Device: target, ExitCode: result.ExitCode, Detail: "Mount berubah selama pemeriksaan."}
	}
	return result
}

func unavailableCheck(target string, err error) CheckResult {
	return CheckResult{Availability: availability(sysrun.Result{Err: err}), State: "not_started", Device: target, ExitCode: -1, Detail: domain.SafeText(err.Error())}
}

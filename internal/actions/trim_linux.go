package actions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

func (p linuxPlatform) trimTargets(ctx context.Context) ([]TrimTarget, error) {
	if _, err := p.manualTrimInvocation(ctx); err != nil {
		return nil, err
	}
	audit := (storage.Collector{Runner: p.runner}).InspectTrim(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var targets []TrimTarget
	for _, mount := range audit.Mounts {
		if mount.Discard == domain.Available && !mount.ReadOnly {
			if _, err := checkMount(mount); err == nil {
				targets = append(targets, TrimTarget{mount.Point, mount.FSType, mount.DeviceID})
			}
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("belum ada mount yang bisa diverifikasi aman untuk TRIM manual")
	}
	return targets, nil
}

func checkMount(mount storage.Mount) (directoryID, error) {
	fd, id, err := openCheckedMount(mount)
	if err == nil {
		fd.Close()
	}
	return id, err
}

func openCheckedMount(mount storage.Mount) (*os.File, directoryID, error) {
	if !safePath(mount.Point) || mount.Discard != domain.Available || mount.ReadOnly || mount.ID == 0 || len(mount.Layers) == 0 {
		return nil, directoryID{}, errors.New("jalur blok atau mount belum dapat diverifikasi untuk TRIM")
	}
	info, err := protectedPath(mount.Point, 0, false)
	if err != nil {
		return nil, directoryID{}, fmt.Errorf("mount harus berada di jalur terlindungi root: %w", err)
	}
	if !info.IsDir() {
		return nil, directoryID{}, errors.New("mountpoint tidak merujuk direktori")
	}
	fd, err := os.Open(mount.Point)
	if err != nil {
		return nil, directoryID{}, err
	}
	valid := false
	defer func() {
		if !valid {
			fd.Close()
		}
	}()
	opened, err := fd.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, directoryID{}, errors.New("mount berubah saat dibuka")
	}
	data, err := os.ReadFile("/proc/self/fdinfo/" + strconv.FormatUint(uint64(fd.Fd()), 10))
	if err != nil {
		return nil, directoryID{}, errors.New("identitas mount untuk direktori terbuka tidak tersedia")
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, found := strings.CutPrefix(line, "mnt_id:"); found && strings.TrimSpace(value) == strconv.FormatUint(mount.ID, 10) {
			id := identity(opened)
			valid = true
			return fd, directoryID{mount.Point, id.Device, id.Inode, id.UID, id.Mode}, nil
		}
	}
	return nil, directoryID{}, errors.New("identitas mount yang terbuka berubah")
}

const descriptorOperand = "/proc/{executor_pid}/fd/{mount_fd}"

func (p linuxPlatform) manualTrimInvocation(ctx context.Context) (invocation, error) {
	inv, err := newInvocation("busybox", []string{"fstrim", "-v", "--", descriptorOperand}, true)
	if err != nil {
		return invocation{}, err
	}
	help := p.runner.Run(ctx, inv.exe.path, "fstrim", "--help")
	if help.Err != nil || help.ExitCode != 0 || !busyboxTrimHelp(string(help.Output)+string(help.Stderr)) {
		return invocation{}, fmt.Errorf("TRIM manual memerlukan applet BusyBox fstrim 1.35.0, 1.36.1, atau 1.37.0: %w", os.ErrNotExist)
	}
	return inv, nil
}

func busyboxTrimHelp(help string) bool {
	header, _, _ := strings.Cut(strings.TrimSpace(help), "\n")
	fields := strings.Fields(header)
	return len(fields) >= 2 && fields[0] == "BusyBox" &&
		slices.Contains([]string{"v1.35.0", "v1.36.1", "v1.37.0"}, fields[1]) &&
		strings.Contains(help, "\nUsage: fstrim [OPTIONS] MOUNTPOINT")
}

func descriptorPath(pid int, fd uintptr) (string, error) {
	if pid <= 0 || fd < 3 || fd > math.MaxInt32 {
		return "", errors.New("PID atau descriptor mount tidak valid")
	}
	return fmt.Sprintf("/proc/%d/fd/%d", pid, fd), nil
}

func descriptorInvocation(inv invocation, pid int, fd uintptr) (invocation, string, error) {
	expected := []string{inv.exe.path, "fstrim", "-v", "--", descriptorOperand}
	if inv.privilege == Sudo {
		expected = append([]string{inv.sudo.path, "--non-interactive", "--"}, expected...)
	} else if inv.privilege != Root {
		return invocation{}, "", errors.New("privilege TRIM tidak cocok dengan rencana")
	}
	if filepath.Base(inv.exe.path) != "busybox" || !slices.Equal(inv.spec, expected) {
		return invocation{}, "", errors.New("operasi TRIM tidak cocok dengan rencana")
	}
	operand, err := descriptorPath(pid, fd)
	if err != nil {
		return invocation{}, "", err
	}
	inv.spec = slices.Clone(inv.spec)
	inv.spec[len(inv.spec)-1] = operand
	return inv, operand, nil
}

func checkDescriptorReference(file *os.File, operand string) error {
	const procFSMagic = 0x9fa0
	self, err := os.Readlink("/proc/self")
	if err != nil || self != strconv.Itoa(os.Getpid()) || filepath.Dir(operand) != "/proc/"+self+"/fd" {
		return errors.New("namespace PID lokal tidak cocok dengan proc")
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(operand), &stat); err != nil || stat.Type != procFSMagic {
		return errors.New("namespace proc lokal tidak menyediakan descriptor mount")
	}
	pinned, err := file.Stat()
	if err != nil {
		return err
	}
	opened, err := os.Stat(operand)
	if err != nil || !os.SameFile(pinned, opened) {
		return errors.New("referensi descriptor tidak menunjuk mount yang disetujui")
	}
	return nil
}

func (p linuxPlatform) trimMount(ctx context.Context, target string) (prepared, error) {
	if !safePath(target) {
		return prepared{}, errors.New("pilih mountpoint absolut dan kanonik dari daftar TRIM")
	}
	audit := (storage.Collector{Runner: p.runner}).InspectTrim(ctx)
	system, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	for _, mount := range audit.Mounts {
		if mount.Point != target || mount.Discard != domain.Available {
			continue
		}
		dir, err := checkMount(mount)
		if err != nil {
			return prepared{}, err
		}
		inv, err := p.manualTrimInvocation(ctx)
		if err != nil {
			return prepared{}, err
		}
		seq := make([]string, len(mount.Layers))
		for i, layer := range mount.Layers {
			seq[i] = layer.DeviceID + "/" + layer.DiskSequence
		}
		var operand string
		return prepared{
			plan: Plan{
				Kind: TrimMount, Target: target,
				Initial:   fmt.Sprintf("Mount %d, perangkat %s, %s, discard tersedia", mount.ID, mount.DeviceID, mount.FSType),
				Identity:  digest(system, dir, mount.ID, mount.DeviceID, mount.Root, mount.Source, mount.FSType, mount.MountOptions, mount.Layers, seq, inv.identity()),
				Operation: inv.spec, Privilege: inv.privilege,
				Impact: "Filesystem ini dapat mengajukan blok bebas untuk discard. Pelaksana mengganti placeholder dengan PID whwmpc dan descriptor mount yang dibuka ulang serta diperiksa.",
				Risk:   "I/O dapat melambat selama TRIM. Operasi discard tidak bisa diurungkan.",
			},
			run: func(ctx context.Context) attempt {
				fd, current, err := openCheckedMount(mount)
				if err != nil {
					return attempt{err: errors.New("mount berubah setelah pemeriksaan, buat preview baru")}
				}
				defer fd.Close()
				if current != dir {
					return attempt{err: errors.New("mount berubah setelah pemeriksaan, buat preview baru")}
				}
				command, name, err := descriptorInvocation(inv, os.Getpid(), fd.Fd())
				if err != nil {
					return attempt{err: err}
				}
				if err := checkDescriptorReference(fd, name); err != nil {
					return attempt{err: err}
				}
				operand = name
				return command.run(ctx, 5*time.Minute)
			},
			verify: func(_ context.Context, step attempt) Result {
				count, found := trimCount(operand, string(step.output))
				current, err := checkMount(mount)
				switch {
				case found && (err != nil || current != dir):
					return Result{Status: Partial, Message: withStepError("FITRIM teramati pada descriptor asal dan mountpoint berubah", step), PotentialDiscardBytes: &count}
				case found && step.err == nil:
					return Result{Status: Verified, Message: "fstrim mengembalikan jumlah byte yang berpotensi diajukan untuk discard pada descriptor yang diperiksa", PotentialDiscardBytes: &count}
				case found:
					return Result{Status: Partial, Message: withStepError("FITRIM teramati pada descriptor yang diperiksa", step), PotentialDiscardBytes: &count}
				default:
					return Result{Status: Unknown, Message: withStepError("Hasil FITRIM tidak dapat dipastikan dari bukti yang tersedia", step)}
				}
			},
		}, nil
	}
	return prepared{}, errors.New("mountpoint tersebut tidak memiliki bukti discard yang cukup untuk tindakan manual")
}

func trimCount(target, output string) (uint64, bool) {
	line, rightTarget := strings.CutPrefix(strings.TrimSpace(output), target+": ")
	countText, hasSuffix := strings.CutSuffix(line, " bytes trimmed")
	if target == "" || !rightTarget || !hasSuffix || len(countText) > 20 {
		return 0, false
	}
	count, err := strconv.ParseUint(countText, 10, 64)
	return count, err == nil
}

type timerState struct {
	Enabled     string
	Active      string
	Definition  string
	ActiveSince string
}

func (p linuxPlatform) timer(ctx context.Context) (timerState, error) {
	timer := (storage.Collector{Runner: p.runner}).InspectTrim(ctx).Timer
	if timer.Availability != domain.Available || timer.Scheduler != "systemd" || timer.Unit != "fstrim.service" ||
		timer.LoadState != "loaded" || timer.ServiceLoadState != "loaded" || timer.NeedDaemonReload ||
		timer.DropInPaths != "" || timer.ServiceDropInPaths != "" {
		return timerState{}, errors.New("kelola jadwal melalui scheduler sistem. Aksi otomatis memerlukan unit vendor systemd yang dimuat tanpa override")
	}
	timerFile, timerID, err := vendorUnit(timer.FragmentPath, "fstrim.timer")
	if err != nil {
		return timerState{}, err
	}
	serviceFile, serviceID, err := vendorUnit(timer.ServiceFragmentPath, "fstrim.service")
	if err != nil {
		return timerState{}, err
	}
	if !standardTimer(timerFile) {
		return timerState{}, errors.New("jadwal fstrim.timer berbeda dari unit mingguan yang didukung")
	}
	execLine, ok := standardService(serviceFile)
	if !ok || !loadedServiceMatches(timer.ServiceExecStart, execLine) {
		return timerState{}, errors.New("operasi fstrim.service berbeda dari unit vendor yang didukung")
	}
	file, err := trustedExecutable(strings.Fields(execLine)[0])
	if err != nil {
		return timerState{}, err
	}
	system, err := bootIdentity()
	if err != nil {
		return timerState{}, err
	}
	if _, err := strconv.ParseUint(timer.ActiveEnterMonotonic, 10, 64); err != nil {
		return timerState{}, errors.New("identitas aktivasi timer tidak dapat diverifikasi")
	}
	return timerState{timer.UnitFileState, timer.ActiveState, digest(system, timerID, serviceID, timerFile, serviceFile, file.id), timer.ActiveEnterMonotonic}, nil
}

func vendorUnit(path, name string) (string, fileID, error) {
	if path != "/usr/lib/systemd/system/"+name && path != "/lib/systemd/system/"+name {
		return "", fileID{}, errors.New("lokasi unit fstrim berbeda dari unit vendor")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fileID{}, err
	}
	if canonical != "/usr/lib/systemd/system/"+name && canonical != "/lib/systemd/system/"+name {
		return "", fileID{}, errors.New("unit vendor mengarah ke lokasi lain")
	}
	info, err := protectedPath(canonical, 0, false)
	if err != nil {
		return "", fileID{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8192 {
		return "", fileID{}, errors.New("definisi unit fstrim tidak valid")
	}
	data, err := os.ReadFile(canonical)
	return string(data), identity(info), err
}

func unitFields(content string) (map[string]string, bool) {
	values := make(map[string]string)
	section := ""
	for raw := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			continue
		}
		key, value, found := strings.Cut(line, "=")
		field := section + "." + key
		if _, duplicate := values[field]; section == "" || !found || duplicate ||
			strings.HasSuffix(value, "\\") {
			return nil, false
		}
		values[field] = value
	}
	return values, true
}

func standardTimer(content string) bool {
	values, ok := unitFields(content)
	if !ok || values["Timer.OnCalendar"] != "weekly" || values["Install.WantedBy"] != "timers.target" {
		return false
	}
	for key := range values {
		switch key {
		case "Unit.Description", "Unit.Documentation", "Unit.ConditionVirtualization", "Unit.ConditionPathExists",
			"Timer.OnCalendar", "Timer.AccuracySec", "Timer.Persistent", "Timer.RandomizedDelaySec", "Install.WantedBy":
		default:
			return false
		}
	}
	return true
}

func standardService(content string) (string, bool) {
	values, ok := unitFields(content)
	command := values["Service.ExecStart"]
	parts := strings.Fields(command)
	if !ok || values["Service.Type"] != "oneshot" || len(parts) < 4 ||
		(parts[0] != "/usr/sbin/fstrim" && parts[0] != "/sbin/fstrim") {
		return "", false
	}
	if flags := strings.Join(parts[1:], " "); flags != "--listed-in /etc/fstab:/proc/self/mountinfo --verbose --quiet-unsupported" &&
		flags != "--fstab --verbose --quiet-unsupported" {
		return "", false
	}
	for key := range values {
		switch key {
		case "Unit.Description", "Unit.Documentation", "Unit.ConditionVirtualization", "Service.Type", "Service.ExecStart",
			"Service.PrivateDevices", "Service.PrivateNetwork", "Service.PrivateUsers", "Service.ProtectKernelTunables",
			"Service.ProtectKernelModules", "Service.ProtectControlGroups", "Service.MemoryDenyWriteExecute", "Service.SystemCallFilter":
		default:
			return "", false
		}
	}
	return strings.Join(parts, " "), true
}

func loadedServiceMatches(status, command string) bool {
	_, args, found := strings.Cut(status, "argv[]=")
	loaded, _, terminated := strings.Cut(args, " ; ")
	return found && terminated && strings.Join(strings.Fields(loaded), " ") == command &&
		strings.Contains(status, " ; ignore_errors=no ; ")
}

func (p linuxPlatform) trimTimer(ctx context.Context, kind, target string) (prepared, error) {
	if target != "" && target != "fstrim.timer" {
		return prepared{}, errors.New("unit yang didukung hanya fstrim.timer")
	}
	before, err := p.timer(ctx)
	if err != nil {
		return prepared{}, err
	}
	expected := timerState{Enabled: "enabled", Active: "active", Definition: before.Definition}
	verb := "enable"
	if kind == RestoreTrimTimer {
		expected = timerState{Enabled: "disabled", Active: "inactive", Definition: before.Definition}
		verb = "disable"
	}
	if (kind == EnableTrimTimer && (before.Enabled != "disabled" || before.Active != "inactive")) ||
		(kind == RestoreTrimTimer && (before.Enabled != "enabled" || before.Active != "active")) {
		return prepared{}, fmt.Errorf("timer berstatus %s/%s, tinjau jadwal aktif sebelum mengubahnya", before.Enabled, before.Active)
	}
	inv, err := newInvocation("systemctl", []string{"--system", "--no-ask-password", verb, "--now", "fstrim.timer"}, true)
	if err != nil {
		return prepared{}, err
	}
	timerIdentity := func(state timerState) string { return digest(state, inv.identity()) }
	impact := "Mengaktifkan jadwal TRIM mingguan. systemd dapat menjalankan jadwal yang terlewat saat timer dimulai."
	if kind == RestoreTrimTimer {
		impact = "Mengembalikan timer ke keadaan nonaktif sebelumnya. Discard yang sudah terjadi tetap berlaku."
	}
	return prepared{
		plan: Plan{
			Kind: kind, Target: "fstrim.timer", Initial: before.Enabled + "/" + before.Active,
			Identity: timerIdentity(before), Operation: inv.spec, Privilege: inv.privilege, Impact: impact,
			Risk:     "Jadwal memproses filesystem sesuai konfigurasi fstrim vendor. TRIM yang terjadi tidak memiliki undo.",
			Rollback: kind == EnableTrimTimer,
		},
		run: func(ctx context.Context) attempt { return inv.run(ctx, 45*time.Second) },
		verify: func(ctx context.Context, step attempt) Result {
			after, err := p.timer(ctx)
			return verifyTimer(before, expected, after, err, timerIdentity(after), step)
		},
	}, nil
}

func verifyTimer(before, expected, after timerState, err error, id string, step attempt) Result {
	if err != nil || before.Definition != after.Definition {
		return Result{Status: Unknown, Message: withStepError("Definisi timer berubah atau keadaannya belum dapat dibaca", step)}
	}
	changed := before != after
	message := fmt.Sprintf("Timer saat diperiksa ulang berstatus %s/%s", safeText(after.Enabled), safeText(after.Active))
	switch {
	case after.Enabled == expected.Enabled && after.Active == expected.Active && step.err == nil:
		return Result{Status: Verified, Message: message, Changed: boolPointer(changed), AfterIdentity: id}
	case changed:
		return Result{Status: Partial, Message: withStepError(message, step), Changed: boolPointer(changed)}
	case step.stopped:
		return Result{Status: Unknown, Message: withStepError(message, step)}
	default:
		return Result{Status: Failed, Message: withStepError(message, step), Changed: boolPointer(false)}
	}
}

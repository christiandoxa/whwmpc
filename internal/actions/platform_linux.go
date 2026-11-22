package actions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type linuxPlatform struct {
	runner sysrun.Runner
}

func newPlatform() platform {
	return linuxPlatform{runner: trustedRunner{}}
}

func (p linuxPlatform) prepare(ctx context.Context, kind, target string) (prepared, error) {
	switch kind {
	case PowerProfile, RestorePower:
		return p.profile(ctx, kind, target)
	case TrimMount:
		return p.trimMount(ctx, target)
	case EnableTrimTimer, RestoreTrimTimer:
		return p.trimTimer(ctx, kind, target)
	case FirmwareUpdate:
		return p.firmwareUpdate(ctx, target)
	case FirmwareRefresh:
		return p.firmwareRefresh(ctx, target)
	case FilesystemRepair:
		return p.filesystemRepair(ctx, target)
	case CleanAPT:
		return p.apt(ctx, target)
	case CleanPacman:
		return p.cleanPacman(ctx, target)
	case CleanDNF:
		return p.cleanDNF(ctx, target)
	case hardware.PowerLimit, hardware.CoreMax, hardware.VoltageOffset, hardware.RestorePowerLimit, hardware.RestoreCoreMax, hardware.RestoreVoltageOffset:
		return p.gpu(ctx, kind, target)
	default:
		return prepared{}, errors.New("tindakan tidak dikenal")
	}
}

func (linuxPlatform) authenticate(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	terminal, ok := stdin.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(terminal.Fd()) {
		return errors.New("autentikasi sudo interaktif memerlukan terminal")
	}
	sudo, err := trustedExecutable("sudo")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := command(ctx, sudo.path, []string{"--validate"})
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("autentikasi sudo gagal: %w", err)
	}
	return nil
}

var executablePaths = map[string][]string{
	"apt-config":  {"/usr/bin/apt-config"},
	"apt-get":     {"/usr/bin/apt-get"},
	"blkid":       {"/usr/sbin/blkid", "/sbin/blkid"},
	"busctl":      {"/usr/bin/busctl", "/bin/busctl"},
	"busybox":     {"/usr/bin/busybox", "/bin/busybox"},
	"e2fsck":      {"/usr/sbin/e2fsck", "/sbin/e2fsck"},
	"env":         {"/usr/bin/env", "/bin/env"},
	"find":        {"/usr/bin/find", "/bin/find"},
	"dd":          {"/usr/bin/dd", "/bin/dd"},
	"dnf5":        {"/usr/bin/dnf5"},
	"flock":       {"/usr/bin/flock", "/bin/flock"},
	"nvidia-smi":  {"/usr/bin/nvidia-smi"},
	"fstrim":      {"/usr/sbin/fstrim", "/sbin/fstrim"},
	"fwupdmgr":    {"/usr/bin/fwupdmgr"},
	"journalctl":  {"/usr/bin/journalctl", "/bin/journalctl"},
	"lsblk":       {"/usr/bin/lsblk", "/bin/lsblk"},
	"pacman":      {"/usr/bin/pacman"},
	"pacman-conf": {"/usr/bin/pacman-conf"},
	"sudo":        {"/usr/bin/sudo", "/bin/sudo"},
	"systemctl":   {"/usr/bin/systemctl", "/bin/systemctl"},
}

type fileID struct {
	Device uint64
	Inode  uint64
	UID    uint32
	Mode   uint32
	Size   int64
	CTime  syscall.Timespec
	MTime  syscall.Timespec
}

func identity(info os.FileInfo) fileID {
	stat := info.Sys().(*syscall.Stat_t)
	return fileID{uint64(stat.Dev), stat.Ino, stat.Uid, stat.Mode, stat.Size, stat.Ctim, stat.Mtim}
}

type executable struct {
	path string
	id   fileID
}

func trustedExecutable(name string) (executable, error) {
	base := filepath.Base(name)
	for _, path := range executablePaths[base] {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		allowed := false
		for _, candidate := range executablePaths[base] {
			allowed = allowed || canonical == candidate
		}
		if !allowed || (name != base && name != path && name != canonical) {
			continue
		}
		info, err := protectedPath(canonical, 0, false)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		return executable{canonical, identity(info)}, nil
	}
	return executable{}, fmt.Errorf("%s: executable sistem tepercaya tidak tersedia: %w", base, os.ErrNotExist)
}

// protectedPath refuses write access by group or others, including along ancestors.
func protectedPath(path string, owner uint32, allowMissingLeaf bool) (os.FileInfo, error) {
	if !safePath(path) {
		return nil, errors.New("path bukan absolut, kanonik, atau mengandung karakter khusus")
	}
	leaf, err := os.Lstat(path)
	if err != nil && !(allowMissingLeaf && errors.Is(err, os.ErrNotExist)) {
		return nil, err
	}
	if leaf != nil {
		stat := identity(leaf)
		if stat.UID != owner || leaf.Mode()&(os.ModeSymlink|0022) != 0 {
			return nil, errors.New("kepemilikan, izin, atau symlink target tidak aman")
		}
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || identity(info).UID != 0 || info.Mode().Perm()&0022 != 0 {
			return nil, errors.New("ancestor bukan direktori yang dilindungi root")
		}
		if parent == "/" {
			break
		}
	}
	return leaf, nil
}

func safePath(path string) bool {
	return path != "" && len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		utf8.ValidString(path) && !strings.ContainsFunc(path, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	})
}

func bootIdentity() (string, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || len(bytes.TrimSpace(boot)) != 36 {
		return "", errors.New("identitas boot tidak tersedia")
	}
	mountNS, err := os.Stat("/proc/self/ns/mnt")
	if err != nil {
		return "", errors.New("identitas namespace mount tidak tersedia")
	}
	id := identity(mountNS)
	return digest(string(bytes.TrimSpace(boot)), id.Device, id.Inode, os.Getuid()), nil
}

func digest(values ...any) string {
	data, _ := json.Marshal(values)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type invocation struct {
	spec      []string
	exe       executable
	sudo      executable
	children  []executable
	privilege string
}

func newInvocation(name string, args []string, needsRoot bool) (invocation, error) {
	exe, err := trustedExecutable(name)
	if err != nil {
		return invocation{}, err
	}
	inv := invocation{exe: exe, spec: append([]string{exe.path}, args...), privilege: Polkit}
	if !needsRoot {
		return inv, nil
	}
	if os.Geteuid() == 0 {
		inv.privilege = Root
		return inv, nil
	}
	sudo, err := trustedExecutable("sudo")
	if err != nil {
		return invocation{}, err
	}
	inv.privilege, inv.sudo = Sudo, sudo
	inv.spec = append([]string{sudo.path, "--non-interactive", "--"}, inv.spec...)
	return inv, nil
}

func (inv invocation) identity() string {
	if len(inv.children) == 0 {
		return digest(inv.exe.id, inv.sudo.id)
	}
	ids := make([]fileID, len(inv.children))
	for i, child := range inv.children {
		ids[i] = child.id
	}
	return digest(inv.exe.id, inv.sudo.id, ids)
}

func (inv invocation) run(ctx context.Context, timeout time.Duration) attempt {
	return inv.runInput(ctx, timeout, nil)
}

func (inv invocation) runInput(ctx context.Context, timeout time.Duration, stdin io.Reader) attempt {
	paths := []executable{inv.exe}
	if inv.privilege == Sudo {
		paths = append(paths, inv.sudo)
	}
	paths = append(paths, inv.children...)
	for _, expected := range paths {
		current, err := trustedExecutable(expected.path)
		if err != nil || current != expected {
			return attempt{err: errors.New("executable berubah setelah pemeriksaan, buat preview baru")}
		}
	}
	step, stderr, exit := runInput(ctx, timeout, inv.spec[0], inv.spec[1:], stdin)
	if inv.privilege == Sudo && exit == 1 {
		message := strings.ToLower(string(stderr))
		if strings.HasPrefix(message, "sudo: ") && (strings.Contains(message, "password is required") ||
			strings.Contains(message, "not allowed") || strings.Contains(message, "terminal is required") ||
			strings.Contains(message, "sudoers")) {
			return attempt{err: fmt.Errorf("otorisasi sudo diperlukan: %s", safeText(string(stderr)))}
		}
	}
	if step.err == nil && exit != 0 {
		detail := safeText(string(stderr))
		if detail == "" {
			detail = fmt.Sprintf("exit %d", exit)
		}
		step.err = errors.New(detail)
	}
	return step
}

type trustedRunner struct{}

func (trustedRunner) LookPath(name string) (string, error) {
	exe, err := trustedExecutable(name)
	return exe.path, err
}

func (trustedRunner) Run(ctx context.Context, file string, args ...string) sysrun.Result {
	exe, err := trustedExecutable(file)
	if err != nil {
		return sysrun.Result{ExitCode: -1, Err: err}
	}
	step, stderr, exit := run(ctx, 6*time.Second, exe.path, args)
	return sysrun.Result{Output: step.output, Stderr: stderr, ExitCode: exit, Err: step.err}
}

func command(ctx context.Context, path string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = "/"
	cmd.Env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "TERM=dumb", "HOME=/"}
	cmd.WaitDelay = time.Second
	return cmd
}

const outputLimit = 1 << 20

type limitedOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (b *limitedOutput) Write(data []byte) (int, error) {
	size := min(len(data), outputLimit-b.buffer.Len())
	b.truncated = b.truncated || size < len(data)
	_, _ = b.buffer.Write(data[:size])
	return len(data), nil
}

func run(ctx context.Context, timeout time.Duration, path string, args []string) (attempt, []byte, int) {
	return runInput(ctx, timeout, path, args, nil)
}

func runInput(ctx context.Context, timeout time.Duration, path string, args []string, stdin io.Reader) (attempt, []byte, int) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(ctx, path, args)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	var stdout, stderr limitedOutput
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return attempt{err: err}, nil, -1
	}
	err := cmd.Wait()
	exit := -1
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		err = ctx.Err()
	case stdout.truncated || stderr.truncated:
		err = errors.New("output subprocess melampaui batas")
	case errors.As(err, &exitErr) && exit >= 0:
		err = nil
	}
	return attempt{output: stdout.buffer.Bytes(), exitCode: exit, started: true, stopped: ctx.Err() != nil, err: err}, stderr.buffer.Bytes(), exit
}

func boolPointer(value bool) *bool { return &value }

func withStepError(message string, step attempt) string {
	if step.err != nil {
		message += ". Proses melaporkan " + safeText(step.err.Error())
	}
	return message
}

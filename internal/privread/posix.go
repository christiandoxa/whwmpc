//go:build linux || darwin

package privread

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"

	"github.com/charmbracelet/x/term"
)

const systemPath = "/usr/bin:/usr/sbin:/bin:/sbin"

func elevated() bool { return os.Geteuid() == 0 }

func nativeCapability() error {
	if elevated() {
		return nil
	}
	if _, err := protectedExecutable("/usr/bin/sudo", 0); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	return nil
}

func nativeStart(ctx context.Context, scope Scope, terminal *os.File, stderr io.Writer) (io.ReadWriteCloser, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	self, err = protectedExecutable(self, uint32(os.Geteuid()))
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if runtime.GOOS == "linux" {
		running, runErr := os.Stat("/proc/self/exe")
		candidate, statErr := os.Stat(self)
		if runErr != nil || statErr != nil || !os.SameFile(running, candidate) {
			return nil, errors.Join(ErrUnavailable, errors.New("the running executable has changed"))
		}
	}
	executable, args := self, []string{HelperFlag, string(scope)}
	if !elevated() {
		executable, err = protectedExecutable("/usr/bin/sudo", 0)
		if err != nil {
			return nil, errors.Join(ErrUnavailable, err)
		}
		mode := "--non-interactive"
		if terminal != nil {
			if !term.IsTerminal(terminal.Fd()) {
				return nil, ErrUnavailable
			}
			tty, err := os.Open("/dev/tty")
			if err != nil {
				return nil, errors.Join(ErrUnavailable, err)
			}
			tty.Close()
			mode = "-k" // Authenticate this command without refreshing the sudo timestamp.
		}
		args = append([]string{mode, "--", self}, args...)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, args...)
	cmd.Env, cmd.Dir, cmd.Stderr = []string{"PATH=" + systemPath, "LC_ALL=C"}, "/", stderr
	toChild, fromParent, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer toChild.Close()
	fromChild, toParent, err := os.Pipe()
	if err != nil {
		fromParent.Close()
		return nil, err
	}
	defer toParent.Close()
	cmd.Stdin, cmd.Stdout = toChild, toParent
	if err := cmd.Start(); err != nil {
		fromParent.Close()
		fromChild.Close()
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return &posixPipe{Reader: fromChild, Writer: fromParent, stop: func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}}, nil
}

type posixPipe struct {
	Reader, Writer *os.File
	stop           func()
}

func (p *posixPipe) Read(b []byte) (int, error)  { return p.Reader.Read(b) }
func (p *posixPipe) Write(b []byte) (int, error) { return p.Writer.Write(b) }
func (p *posixPipe) Close() error {
	err := errors.Join(p.Writer.Close(), p.Reader.Close())
	if p.stop != nil {
		p.stop()
	}
	return err
}

func helperPipe(args []string) (Scope, io.ReadWriteCloser, error) {
	if len(args) != 1 {
		return "", nil, ErrProtocol
	}
	for _, file := range []*os.File{os.Stdin, os.Stdout} {
		if info, err := file.Stat(); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			return "", nil, ErrProtocol
		}
	}
	return Scope(args[0]), &posixPipe{Reader: os.Stdin, Writer: os.Stdout}, nil
}

func helperEnvironment() error {
	os.Clearenv()
	if err := os.Setenv("PATH", systemPath); err != nil {
		return err
	}
	return os.Chdir("/")
}

func protectedExecutable(path string, uid uint32) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(canonical) {
		return "", os.ErrNotExist
	}
	for p := canonical; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		owner := info.Sys().(*syscall.Stat_t).Uid
		if owner != 0 && owner != uid || info.Mode()&os.ModeSymlink != 0 {
			return "", os.ErrPermission
		}
		if p == canonical {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
				return "", os.ErrPermission
			}
		} else if !info.IsDir() || info.Mode().Perm()&0022 != 0 && (owner != 0 || info.Mode()&os.ModeSticky == 0) {
			return "", os.ErrPermission
		}
		if p == "/" {
			return canonical, nil
		}
	}
}

var linuxTools = map[Operation][]string{
	SMART: {"smartctl"}, TRIM: {"systemctl", "journalctl", "fstrim"}, Power: {"systemctl", "busctl", "powerprofilesctl"},
	GPU: {"nvidia-smi"}, Firmware: {"fwupdmgr"}, Filesystem: {"lsblk"},
}

func trustedTool(op Operation, name string) (string, error) {
	base := filepath.Base(name)
	dirs := []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin"}
	if runtime.GOOS == "darwin" {
		dirs = []string{"/usr/sbin"}
		if !((op == SMART || op == Filesystem) && base == "diskutil" || op == TRIM && base == "system_profiler") {
			return "", os.ErrNotExist
		}
	} else if !slices.Contains(linuxTools[op], base) {
		return "", os.ErrNotExist
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, base)
		if name != base && name != path {
			continue
		}
		canonical, err := protectedExecutable(path, 0)
		if err == nil && filepath.Base(canonical) == base && slices.Contains(dirs, filepath.Dir(canonical)) {
			return canonical, nil
		}
	}
	return "", os.ErrNotExist
}

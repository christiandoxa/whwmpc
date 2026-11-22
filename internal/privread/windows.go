//go:build windows

package privread

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"github.com/charmbracelet/x/term"
	"github.com/christiandoxa/whwmpc/internal/storage"
	"golang.org/x/sys/windows"
)

var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

const pipePrefix = `\\.\pipe\whwmpc-read-`

func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

func nativeCapability() error {
	if err := shellExecuteEx.Find(); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	return nil
}

func localExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(path) || len(filepath.VolumeName(path)) != 2 || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return "", ErrUnavailable
	}
	root, _ := windows.UTF16PtrFromString(filepath.VolumeName(path) + "\\")
	if windows.GetDriveType(root) != windows.DRIVE_FIXED {
		return "", ErrUnavailable
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", ErrUnavailable
	}
	return path, nil
}

func nativeStart(ctx context.Context, scope Scope, terminal *os.File, _ io.Writer) (io.ReadWriteCloser, error) {
	verb := "runas"
	if terminal == nil {
		if !elevated() {
			return nil, errors.Join(ErrDenied, errors.New("UAC requires an interactive Windows session"))
		}
		verb = "open"
	} else if !term.IsTerminal(terminal.Fd()) {
		return nil, ErrUnavailable
	}
	self, err := localExecutable()
	if err != nil {
		return nil, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	acl, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	name := hex.EncodeToString(nonce[:])
	path, _ := windows.UTF16PtrFromString(pipePrefix + name)
	security := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: acl}
	pipe, err := windows.CreateNamedPipe(path, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 65536, 65536, 0, &security)
	if err != nil {
		return nil, err
	}
	file := false
	defer func() {
		if !file {
			windows.CloseHandle(pipe)
		}
	}()
	params := fmt.Sprintf("%s %s %s %d", HelperFlag, scope, name, os.Getpid())
	process, err := startWindowsProcess(ctx, self, verb, params)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(process)
	if err := acceptWindowsPipe(ctx, pipe, process); err != nil {
		return nil, err
	}
	want, err := windows.GetProcessId(process)
	var pid uint32
	if err != nil || windows.GetNamedPipeClientProcessId(pipe, &pid) != nil || pid != want {
		return nil, ErrProtocol
	}
	file = true
	return os.NewFile(uintptr(pipe), "whwmpc authorized reads"), nil
}

type shellExecuteInfo struct {
	Size, Mask                        uint32
	Window                            windows.Handle
	Verb, File, Parameters, Directory *uint16
	Show                              int32
	Instance, IDList                  windows.Handle
	Class                             *uint16
	ClassKey                          windows.Handle
	HotKey                            uint32
	IconOrMonitor, Process            windows.Handle
}

func startWindowsProcess(ctx context.Context, exe, action, args string) (windows.Handle, error) {
	type result struct {
		handle windows.Handle
		err    error
	}
	finished := make(chan result)
	go func() {
		handle, err := launchWindowsProcess(exe, action, args)
		select {
		case finished <- result{handle, err}:
		case <-ctx.Done():
			if handle != 0 {
				windows.CloseHandle(handle)
			}
		}
	}()
	select {
	case r := <-finished:
		return r.handle, r.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func launchWindowsProcess(exe, action, args string) (windows.Handle, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil && err != windows.Errno(1) {
		return 0, err
	}
	defer windows.CoUninitialize()
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return 0, err
	}
	verb, _ := windows.UTF16PtrFromString(action)
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(args)
	cwd, _ := windows.UTF16PtrFromString(dir)
	const noCloseProcess, noAsync, noErrorUI = 0x40, 0x100, 0x400
	info := shellExecuteInfo{Mask: noCloseProcess | noAsync | noErrorUI, Verb: verb, File: file, Parameters: params, Directory: cwd}
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(&info)
	if ok == 0 || info.Process == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, ErrCancelled
		}
		return 0, errors.Join(ErrDenied, callErr)
	}
	return info.Process, nil
}

func acceptWindowsPipe(ctx context.Context, pipe, process windows.Handle) error {
	ready, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ready)
	over := windows.Overlapped{HEvent: ready}
	if err := windows.ConnectNamedPipe(pipe, &over); err != windows.ERROR_IO_PENDING {
		if err == windows.ERROR_PIPE_CONNECTED {
			return nil
		}
		return err
	}
	defer func() {
		var n uint32
		_ = windows.CancelIoEx(pipe, &over)
		_ = windows.GetOverlappedResult(pipe, &over, &n, true)
	}()
	for ctx.Err() == nil {
		result, err := windows.WaitForMultipleObjects([]windows.Handle{ready, process}, false, 100)
		if err != nil {
			return err
		}
		switch result {
		case windows.WAIT_OBJECT_0:
			var n uint32
			return windows.GetOverlappedResult(pipe, &over, &n, false)
		case windows.WAIT_OBJECT_0 + 1:
			return ErrDenied
		}
	}
	return ctx.Err()
}

func helperPipe(args []string) (Scope, io.ReadWriteCloser, error) {
	if len(args) != 3 || !Scope(args[0]).valid() || len(args[1]) != 32 {
		return "", nil, ErrProtocol
	}
	if _, err := hex.DecodeString(args[1]); err != nil {
		return "", nil, ErrProtocol
	}
	parentID, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil || parentID == 0 {
		return "", nil, ErrProtocol
	}
	path, _ := windows.UTF16PtrFromString(pipePrefix + args[1])
	pipe, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
	if err != nil {
		return "", nil, err
	}
	var owner uint32
	if windows.GetNamedPipeServerProcessId(pipe, &owner) != nil || owner != uint32(parentID) || !sameWindowsImage(owner) {
		windows.CloseHandle(pipe)
		return "", nil, ErrProtocol
	}
	return Scope(args[0]), os.NewFile(uintptr(pipe), "whwmpc authorized reads"), nil
}

func sameWindowsImage(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	name := make([]uint16, 32768)
	n := uint32(len(name))
	if err := windows.QueryFullProcessImageName(handle, 0, &name[0], &n); err != nil {
		return false
	}
	self, err := localExecutable()
	if err != nil {
		return false
	}
	parent, err := os.Stat(windows.UTF16ToString(name[:n]))
	file, selfErr := os.Stat(self)
	return err == nil && selfErr == nil && os.SameFile(parent, file)
}

func helperEnvironment() error {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	root, err := windows.GetWindowsDirectory()
	if err != nil {
		return err
	}
	os.Clearenv()
	return errors.Join(os.Setenv("SystemRoot", root), os.Setenv("WINDIR", root), os.Setenv("PATH", system), os.Chdir(system))
}

func trustedTool(op Operation, name string) (string, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	var path string
	switch op {
	case SMART:
		path = filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	case TRIM:
		path = filepath.Join(system, "fsutil.exe")
	}
	if path == "" || !strings.EqualFold(path, name) {
		return "", os.ErrNotExist
	}
	return path, nil
}

func readArgs(op Operation, _ string, args []string) bool {
	switch op {
	case SMART:
		return slices.Equal(args, storage.WindowsSMARTReadArgs())
	case TRIM:
		return slices.Equal(args, []string{"behavior", "query", "DisableDeleteNotify"})
	default:
		return false
	}
}

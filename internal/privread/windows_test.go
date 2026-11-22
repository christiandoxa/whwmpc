//go:build windows

package privread

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsLocalPipeValidatesItsPeer(t *testing.T) {
	const nonce = "0123456789abcdef89abcdef01234567"
	name, err := windows.UTF16PtrFromString(pipePrefix + nonce)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if pipe != 0 {
			windows.CloseHandle(pipe)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	connected := make(chan error, 1)
	release := make(chan struct{})
	go func() {
		scope, peer, err := helperPipe([]string{string(Check), nonce, fmt.Sprint(os.Getpid())})
		if err == nil {
			defer peer.Close()
			if scope != Check {
				err = ErrProtocol
			} else {
				_, err = io.WriteString(peer, "fixture\n")
			}
			connected <- err
			select {
			case <-release:
			case <-ctx.Done():
			}
			return
		}
		connected <- err
	}()
	if err := acceptWindowsPipe(ctx, pipe, windows.CurrentProcess()); err != nil {
		t.Fatal(err)
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(pipe, &pid); err != nil || pid != uint32(os.Getpid()) {
		t.Fatalf("unexpected pipe peer %d: %v", pid, err)
	}
	file := os.NewFile(uintptr(pipe), "test read pipe")
	pipe = 0
	defer file.Close()
	if err := file.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [8]byte
	if _, err := io.ReadFull(file, data[:]); err != nil || string(data[:]) != "fixture\n" {
		t.Fatalf("pipe read %q: %v", data, err)
	}
	if err := <-connected; err != nil {
		t.Fatal(err)
	}
	close(release)
}

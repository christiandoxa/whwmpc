//go:build !linux && !darwin && !windows

package privread

import (
	"context"
	"io"
	"os"
)

func elevated() bool          { return false }
func nativeCapability() error { return ErrUnavailable }
func nativeStart(context.Context, Scope, *os.File, io.Writer) (io.ReadWriteCloser, error) {
	return nil, ErrUnavailable
}
func helperPipe([]string) (Scope, io.ReadWriteCloser, error) { return "", nil, ErrUnavailable }
func helperEnvironment() error                               { return ErrUnavailable }
func trustedTool(Operation, string) (string, error)          { return "", ErrUnavailable }
func readArgs(Operation, string, []string) bool              { return false }

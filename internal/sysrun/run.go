package sysrun

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, file string, args ...string) Result
}

type Result struct {
	Output   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

type Native struct {
	// Timeout optionally raises the default eight-second command bound.
	Timeout time.Duration
}

func (Native) LookPath(name string) (string, error) { return exec.LookPath(name) }

const outputLimit = 2 << 20

var errOutputLimit = errors.New("subprocess output limit exceeded")

type limitedBuffer struct {
	data      bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	size := min(len(p), outputLimit-b.data.Len())
	b.truncated = b.truncated || size < len(p)
	_, _ = b.data.Write(p[:size])
	return len(p), nil
}

func (n Native) Run(ctx context.Context, file string, args ...string) Result {
	timeout := n.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, min(timeout, 2*time.Minute))
	defer cancel()
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.WaitDelay = 300 * time.Millisecond
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "LC_ALL=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	var out, errOut limitedBuffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	result := Result{Output: out.data.Bytes(), Stderr: errOut.data.Bytes(), ExitCode: -1, Err: err}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		result.Err = ctx.Err()
	case out.truncated || errOut.truncated:
		result.Err = errOutputLimit
	case result.ExitCode >= 0 && errors.As(err, &exitErr):
		result.Err = nil
	}
	return result
}

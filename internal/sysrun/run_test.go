package sysrun

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func TestRunSeparatesOutputAndExitStatus(t *testing.T) {
	if os.Getenv("WHWMPC_RUN_HELPER") == "1" {
		_, _ = os.Stdout.WriteString("json")
		_, _ = os.Stderr.WriteString("diagnostic")
		os.Exit(24)
	}
	t.Setenv("WHWMPC_RUN_HELPER", "1")
	runner := Native{}
	got := runner.Run(context.Background(), os.Args[0], "-test.run=^TestRunSeparatesOutputAndExitStatus$")
	if got.Err != nil || got.ExitCode != 24 || string(got.Output) != "json" || string(got.Stderr) != "diagnostic" {
		t.Fatalf("unexpected result: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got = runner.Run(ctx, os.Args[0])
	if !errors.Is(got.Err, context.Canceled) || got.ExitCode != -1 {
		t.Fatalf("unexpected canceled result: %+v", got)
	}
}

func TestRunBoundsCommandOutput(t *testing.T) {
	if os.Getenv("WHWMPC_BOUNDED_HELPER") == "1" {
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), outputLimit+4096))
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("y"), outputLimit+4096))
		os.Exit(0)
	}
	t.Setenv("WHWMPC_BOUNDED_HELPER", "1")
	got := (Native{}).Run(context.Background(), os.Args[0], "-test.run=^TestRunBoundsCommandOutput$")
	if !errors.Is(got.Err, errOutputLimit) || len(got.Output) != outputLimit || len(got.Stderr) != outputLimit {
		t.Fatalf("cap failed: stdout=%d, stderr=%d, err=%v", len(got.Output), len(got.Stderr), got.Err)
	}
}

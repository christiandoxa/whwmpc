package sysrun

import (
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

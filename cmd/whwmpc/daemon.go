package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/christiandoxa/whwmpc/internal/daemon"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func daemonCommand(ctx context.Context, args []string, scanner *inspection.Scanner, store state.Store, demo bool, lang string, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 || args[0] != "daemon" {
		return 0, false
	}
	if len(args) < 2 || args[1] == "--help" {
		fmt.Fprintln(stdout, "whwmpc daemon install [--interval 15m] | enable | start | status | stop | disable | uninstall | run")
		return 0, true
	}
	verb := args[1]
	flags := flag.NewFlagSet("daemon "+verb, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var interval time.Duration
	if verb == "install" {
		flags.DurationVar(&interval, "interval", 15*time.Minute, "jarak observasi")
	}
	if err := flags.Parse(args[2:]); err != nil {
		if err == flag.ErrHelp {
			return 0, true
		}
		return 2, true
	}
	if flags.NArg() != 0 {
		return 2, true
	}
	if demo {
		fmt.Fprintln(stderr, "DEMO · daemon terkunci")
		return 1, true
	}
	if verb == "run" {
		release, busy, err := store.BeginObservation()
		if err != nil {
			return printError(stderr, err), true
		}
		if busy {
			fmt.Fprintln(stdout, "Observasi lain masih berlangsung.")
			return 0, true
		}
		defer release()
		snapshot, err := workflow.Collect(ctx, scanner, domain.Overview{}, lang, false, false, false, nil)
		if err != nil {
			return printError(stderr, err), true
		}
		if err = store.RecordScan("daemon", snapshot.Input.At, snapshot.Input.Findings); err != nil {
			return printError(stderr, err), true
		}
		fmt.Fprintln(stdout, "Observasi tersimpan.")
		return 0, true
	}
	manager, err := daemon.Default()
	if err != nil {
		return printError(stderr, err), true
	}
	result, err := manager.Manage(ctx, verb, interval)
	if err != nil {
		return printError(stderr, err), true
	}
	if result != "" {
		fmt.Fprintln(stdout, result)
	}
	return 0, true
}

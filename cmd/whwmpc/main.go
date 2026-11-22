package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/tui"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdin *os.File, stdout *os.File, stderr io.Writer) int {
	flags := flag.NewFlagSet("whwmpc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := tui.Options{}
	flags.BoolVar(&options.Demo, "demo", false, "pakai data contoh tanpa tindakan sistem")
	flags.StringVar(&options.Language, "lang", "id", "bahasa: id atau en")
	flags.StringVar(&options.Theme, "theme", "dark", "tema: dark, light, terminal")
	flags.BoolVar(&options.ASCII, "ascii", false, "pakai simbol ASCII")
	motionMode := flags.String("motion", "", "gerak Piko: full, reduced, off")
	noMotion := flags.Bool("no-animations", false, "matikan animasi")
	reduced := flags.Bool("reduced-motion", false, "kurangi animasi")
	showVersion := flags.Bool("version", false, "tampilkan versi")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "whwmpc [opsi] [check --json | doctor | clean | power | trim | apply | restore | report | history | gpu | daemon | benchmark | demo animations]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err == flag.ErrHelp {
		return 0
	} else if err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "whwmpc", version)
		return 0
	}
	if (options.Language != "id" && options.Language != "en") ||
		(options.Theme != "dark" && options.Theme != "light" && options.Theme != "terminal") {
		flags.Usage()
		return 2
	}
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		options.Theme = "terminal"
	}
	args = flags.Args()
	if len(args) == 2 && args[0] == "demo" && args[1] == "animations" {
		options.Demo, options.Gallery = true, true
		args = nil
	}
	options.Motion = motion.Mode(*motionMode)
	switch {
	case *motionMode != "" && !options.Motion.Valid():
		flags.Usage()
		return 2
	case *noMotion:
		options.Motion = motion.Off
	case *reduced:
		options.Motion = motion.Reduced
	}
	source := inspection.New(options.Demo)
	store, stateErr := state.Default()
	maintenance := workflow.NewMaintenance(store, options.Demo)
	if status, handled := gpuCommand(ctx, args, maintenance, stdout, stderr); handled {
		return status
	}
	if status, handled := daemonCommand(ctx, args, source, store, options.Demo, options.Language, stdout, stderr); handled {
		return status
	}
	if status, handled := benchmarkCommand(ctx, args, source, store, options.Demo, stdout, stderr); handled {
		return status
	}
	if status, handled := reportCommand(ctx, args, source, store, options.Demo, options.Language, stdout, stderr); handled {
		return status
	}
	if status, handled := readCommand(ctx, args, source, maintenance, stdout, stderr); handled {
		return status
	}
	if status, handled := actionCommand(ctx, args, maintenance, stdin, stdout, stderr); handled {
		return status
	}
	if len(args) > 0 {
		flags.Usage()
		return 2
	}
	if !term.IsTerminal(stdin.Fd()) || !term.IsTerminal(stdout.Fd()) {
		if options.Gallery {
			fmt.Fprintln(stdout, "DEMO · Galeri Piko memerlukan terminal interaktif.")
			return 0
		}
		o := source.Overview(ctx)
		if options.Demo {
			fmt.Fprintln(stdout, "DEMO")
		}
		fmt.Fprintf(stdout, "whwmpc · %s %s · %s\n", o.OS, o.Arch, o.Availability)
		return 0
	}
	if os.Geteuid() == 0 && !options.Demo {
		fmt.Fprintln(stderr, "Jalankan TUI whwmpc sebagai user biasa.")
		return 1
	}
	if stateErr == nil && !options.Demo {
		if options.Motion == "" {
			saved, err := store.Preference("motion")
			if err != nil {
				fmt.Fprintln(stderr, "whwmpc:", err)
			}
			options.Motion = motion.Mode(saved)
		}
		options.SaveMotion = func(mode motion.Mode) error { return store.SetPreference("motion", string(mode)) }
		if saved, err := store.Preference("workload"); err == nil {
			options.Workload = assistant.Context(saved)
		}
	}
	scheduler := motion.NewScheduler()
	options.Scheduler = scheduler
	options.Authenticate = maintenance.Authenticate
	var p *tea.Program
	options.Send = func(msg tea.Msg) { p.Send(msg) }
	// Keep the event loop available for a verification result after cancellation.
	uiCtx, stopUI := context.WithCancel(context.WithoutCancel(ctx))
	defer stopUI()
	workCtx, stopWork := context.WithCancel(ctx)
	defer stopWork()
	p = tea.NewProgram(tui.New(workCtx, source, tui.Inspector(source, store, maintenance), options),
		tea.WithContext(uiCtx), tea.WithoutSignalHandler(), tea.WithInput(stdin), tea.WithOutput(stdout))
	go scheduler.Run(uiCtx, func(tick motion.Tick) { p.Send(tick) })
	go func() {
		select {
		case <-ctx.Done():
			p.Send(tui.ShutdownMsg{})
		case <-uiCtx.Done():
		}
	}()
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

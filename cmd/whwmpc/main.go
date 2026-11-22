package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/tui"
	"github.com/christiandoxa/whwmpc/internal/tui/motion"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
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
		fmt.Fprintln(stderr, "whwmpc [opsi] [check --json | doctor | power | trim | demo animations]")
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
	if len(args) == 2 && args[0] == "check" && args[1] == "--json" {
		data := struct {
			Demo     bool `json:"demo"`
			Overview any  `json:"overview"`
		}{options.Demo, source.Overview(ctx)}
		if err := json.NewEncoder(stdout).Encode(data); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if status, handled := readCommand(ctx, args, source, options.Demo, stdout, stderr); handled {
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
		fmt.Fprintf(stdout, "whwmpc · %s %s · %s\n", o.OS, o.Arch, o.Availability)
		return 0
	}
	store, err := state.Default()
	if err == nil && !options.Gallery {
		if options.Motion == "" {
			saved, err := store.Preference("motion")
			if err != nil {
				fmt.Fprintln(stderr, "whwmpc:", err)
			}
			options.Motion = motion.Mode(saved)
		}
		options.SaveMotion = func(mode motion.Mode) error { return store.SetPreference("motion", string(mode)) }
	}
	scheduler := motion.NewScheduler()
	options.Scheduler = scheduler
	p := tea.NewProgram(tui.New(ctx, source, tui.Inspector(source), options), tea.WithContext(ctx), tea.WithInput(stdin), tea.WithOutput(stdout))
	motionCtx, stopMotion := context.WithCancel(ctx)
	defer stopMotion()
	go scheduler.Run(motionCtx, func(tick motion.Tick) { p.Send(tick) })
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

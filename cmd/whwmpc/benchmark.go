package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/christiandoxa/whwmpc/internal/benchmark"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/state"
)

func benchmarkCommand(ctx context.Context, args []string, scanner *inspection.Scanner, store state.Store, demo bool, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 || args[0] != "benchmark" {
		return 0, false
	}
	if len(args) < 2 || args[1] == "--help" {
		fmt.Fprintln(stdout, "whwmpc benchmark cpu|stress|read|write [--duration 3s] [--mib 16] [--file FILE | --dir DIRECTORY]")
		return 0, true
	}
	flags := flag.NewFlagSet("benchmark "+args[1], flag.ContinueOnError)
	flags.SetOutput(stderr)
	duration := flags.Duration("duration", 3*time.Second, "durasi CPU atau stress")
	mib := flags.Int("mib", 16, "ukuran tes file dalam MiB")
	file := flags.String("file", "", "file reguler untuk tes baca")
	dir := flags.String("dir", "", "direktori untuk file sementara")
	if err := flags.Parse(args[2:]); err != nil {
		if err == flag.ErrHelp {
			return 0, true
		}
		return 2, true
	}
	if flags.NArg() != 0 || *mib < 1 || *mib > 64 || (*file != "" && *dir != "") {
		fmt.Fprintln(stderr, "Opsi beban tidak valid")
		return 2, true
	}
	if demo {
		fmt.Fprintln(stderr, "DEMO · beban sistem terkunci")
		return 1, true
	}
	mode := benchmark.Mode(args[1])
	path := *file
	if mode == benchmark.Write {
		path = *dir
	}
	c := benchmark.Config{Mode: mode, Path: path, Bytes: int64(*mib) << 20, Duration: *duration}
	if mode == benchmark.Stress {
		c.Temperature = func(ctx context.Context) *float64 { return scanner.Overview(ctx).Temperature.Celsius }
	}
	r, err := benchmark.Run(ctx, c)
	code := printJSON(stdout, stderr, r)
	if r.ProcessedBytes > 0 {
		availability := domain.Available
		if err != nil {
			availability = domain.Partial
		}
		if historyErr := store.RecordScan("benchmark", scanner.Now(), []diagnostics.Finding{{Availability: availability}}); historyErr != nil {
			fmt.Fprintln(stderr, "Riwayat benchmark gagal disimpan:", domain.SafeText(historyErr.Error()))
		}
	}
	if err != nil {
		return printError(stderr, err), true
	}
	return code, true
}

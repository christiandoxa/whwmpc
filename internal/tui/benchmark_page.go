package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/benchmark"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func benchmarkPage(lang string) Page {
	l := translator(lang)
	return Page{Found: []string{l("Beban apa yang ingin diuji?", "What workload would you like to test?")},
		Meaning: []string{l("Tes CPU memakai satu worker. Tes file memakai maksimum 16 MiB di lokasi yang kamu pilih.",
			"CPU tests use one worker. File tests use up to 16 MiB at a location you choose.")},
		Choices: []Choice{
			{l("Uji CPU · 3 detik", "CPU benchmark · 3 seconds"), "bench-review", "cpu"},
			{l("Stress CPU · 15 detik dengan sensor suhu", "CPU stress · 15 seconds with thermal sensor"), "bench-review", "stress"},
			{l("Baca file · 16 MiB", "Read file · 16 MiB"), "bench-path", "read"},
			{l("Tulis file sementara · 16 MiB", "Write temporary file · 16 MiB"), "bench-path", "write"},
		}}
}

func runBenchmark(ctx context.Context, scanner *inspection.Scanner, store state.Store, req Request, demo bool) Page {
	l := translator(req.Language)
	if demo {
		return Page{Availability: domain.Unsupported, Found: []string{l("DEMO · beban sistem terkunci", "DEMO · system workloads locked")}}
	}
	mode := benchmark.Mode(req.Choice.Value)
	c := benchmark.Config{Mode: mode, Bytes: 16 << 20, Path: req.Output, Duration: 3 * time.Second}
	if mode == benchmark.Stress {
		c.Duration = 15 * time.Second
		c.Temperature = func(ctx context.Context) *float64 { return scanner.Overview(ctx).Temperature.Celsius }
	}
	result, err := benchmark.Run(ctx, c)
	status := domain.Available
	headline := l("Pengujian selesai", "Benchmark complete")
	if err != nil {
		headline = l("Pengujian belum selesai", "Benchmark incomplete")
		status = domain.Partial
	}
	observation := l("Belum ada sampel beban yang terukur.", "No workload sample was measured.")
	if result.ProcessedBytes > 0 {
		observation = fmt.Sprintf("%s · %.2f s · %.1f MiB/s · %d B", mode, result.DurationSec, result.MiBPerSec, result.ProcessedBytes)
	}
	detail := fmt.Sprintf("%s/%s · %d CPU", result.OS, result.Architecture, result.LogicalCPUs)
	page := Page{Availability: status, Found: []string{headline, observation},
		Meaning:  []string{l("Bandingkan hanya dengan beban dan kondisi pengujian yang sama.", "Compare only with the same workload and test conditions.")},
		Evidence: []string{observation, detail}}
	if result.PeakCelsius != nil {
		page.Evidence = append(page.Evidence, fmt.Sprintf("Peak %.1f °C", *result.PeakCelsius))
	}
	if err != nil {
		page.Advice = append(page.Advice, domain.SafeText(err.Error()))
	}
	f := diagnostics.Finding{ID: "benchmark.measurement", Category: "benchmark", Availability: status, Severity: diagnostics.Info,
		Observation: observation, Meaning: detail, At: scanner.Now(), Source: "whwmpc benchmark",
		Limits: page.Meaning[0]}
	page.Snapshot = &workflow.Snapshot{Input: report.Input{At: scanner.Now(), Findings: []diagnostics.Finding{f}}}
	if result.ProcessedBytes > 0 {
		if err := store.RecordScan(string(assistant.Benchmark), scanner.Now(), []diagnostics.Finding{f}); err != nil {
			page.Advice = append(page.Advice, l("Riwayat tidak tersimpan.", "History was not saved."))
		}
	}
	return page
}

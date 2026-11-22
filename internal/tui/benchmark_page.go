package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/benchmark"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
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
		c.Temperature = scanner.CPUTemperatureProbe()
	}
	result, err := benchmark.Run(ctx, c)
	at := scanner.Now()
	summary := benchmarkSummary(result, at, req.Language, err)
	status := domain.Available
	if err != nil || !result.Complete {
		status = domain.Partial
	}
	observation := l("Belum ada sampel beban yang terukur.", "No workload sample was measured.")
	if result.ProcessedBytes > 0 {
		observation = fmt.Sprintf("%s · %.2f s · %.1f MiB/s · %d B", mode, result.DurationSec, result.MiBPerSec, result.ProcessedBytes)
	}
	detail := fmt.Sprintf("%s/%s · %d CPU", result.OS, result.Architecture, result.LogicalCPUs)
	page := Page{Availability: status, Found: []string{summary.Headline, observation}, Summary: summary,
		Meaning:  []string{l("Bandingkan hanya dengan beban dan kondisi pengujian yang sama.", "Compare only with the same workload and test conditions.")},
		Evidence: []string{observation, detail}}
	if errors.Is(err, context.Canceled) {
		page.Pose = mascot.Cancelled
	}
	if result.PeakCelsius != nil {
		page.Evidence = append(page.Evidence, fmt.Sprintf("Peak %.1f °C", *result.PeakCelsius))
	}
	if err != nil {
		page.Advice = append(page.Advice, domain.SafeText(err.Error()))
	}
	f := diagnostics.Finding{ID: "benchmark.measurement", Category: "benchmark", Availability: status, Severity: diagnostics.Info,
		Observation: observation, Meaning: detail, At: at, Source: "whwmpc benchmark",
		Limits: page.Meaning[0]}
	page.Snapshot = &workflow.Snapshot{Input: report.Input{At: at, Findings: []diagnostics.Finding{f}}}
	if result.ProcessedBytes > 0 {
		if err := store.RecordScan(string(assistant.Benchmark), at, []diagnostics.Finding{f}); err != nil {
			page.Advice = append(page.Advice, l("Riwayat tidak tersimpan.", "History was not saved."))
		}
	}
	return page
}

func benchmarkSummary(r benchmark.Result, at time.Time, lang string, err error) *ResultSummary {
	l := translator(lang)
	status, headline := domain.Available, l("Pengujian selesai", "Benchmark complete")
	if !r.Complete || err != nil {
		status, headline = domain.Partial, l("Pengujian belum selesai", "Benchmark incomplete")
	}
	if errors.Is(err, context.Canceled) {
		headline = l("Pengujian dibatalkan", "Benchmark cancelled")
	}
	s := &ResultSummary{Headline: headline, At: at, Statuses: []resultStatus{{Label: l("Beban", "Workload"), Value: string(r.Mode), Availability: domain.Available}}}
	if r.DurationSec > 0 && !math.IsInf(r.DurationSec, 0) {
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Durasi", "Duration"), Value: number(r.DurationSec, 3), Unit: "s", Availability: status})
	}
	if r.ProcessedBytes > 0 {
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Data diproses", "Processed data"), Value: bytesText(uint64(r.ProcessedBytes)), Availability: status})
		if r.MiBPerSec > 0 && !math.IsInf(r.MiBPerSec, 0) {
			label := l("Laju SHA-256", "SHA-256 throughput")
			if r.Mode == benchmark.Read || r.Mode == benchmark.Write {
				label = l("Laju I/O", "I/O throughput")
			}
			s.Metrics = append(s.Metrics, resultMetric{Label: label, Value: number(r.MiBPerSec, 1), Unit: "MiB/s", Availability: status})
		}
	} else {
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Sampel", "Sample"), Availability: domain.Unknown})
	}
	if t := r.PeakCelsius; t != nil && !math.IsNaN(*t) && !math.IsInf(*t, 0) {
		s.Metrics = append(s.Metrics, resultMetric{Label: l("Suhu puncak", "Peak temperature"), Value: number(*t, 1), Unit: "°C", Availability: status, Secondary: true})
	}
	if err != nil {
		s.Statuses = append(s.Statuses, resultStatus{Label: l("Penyebab", "Reason"), Value: err.Error(), Availability: status})
	}
	return s
}

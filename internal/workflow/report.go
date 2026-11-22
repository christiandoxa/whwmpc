package workflow

import (
	"context"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/state"
)

func ScanReport(ctx context.Context, scanner *inspection.Scanner, overview domain.Overview, lang string, demo bool, entries []state.Entry) (report.Report, error) {
	if overview.CapturedAt.IsZero() || scanner.Now().Sub(overview.CapturedAt) > 15*time.Second {
		overview = scanner.Overview(ctx)
	}
	p := scanner.Power(ctx)
	t := scanner.TRIM(ctx)
	s := scanner.SMART(ctx, false)
	if err := ctx.Err(); err != nil {
		return report.Report{}, err
	}
	now := scanner.Now()
	a := diagnostics.Analyzer{Language: lang, Now: now}
	findings := append(a.Slow(overview, p), a.SMART(s)...)
	findings = append(findings, a.TRIM(t)...)
	findings = append(findings, a.Power(p)...)
	seen := make(map[string]bool)
	deduplicated := make([]diagnostics.Finding, 0, len(findings))
	for _, finding := range findings {
		if !seen[finding.ID] {
			seen[finding.ID] = true
			deduplicated = append(deduplicated, finding)
		}
	}
	var results []actions.Result
	for _, entry := range entries {
		if action := entry.Action; action != nil {
			results = append(results, actions.Result{
				Plan: actions.Plan{Kind: entry.Category}, Status: action.Status, FinishedAt: entry.At,
				Started: action.Started, CancellationRequested: action.CancellationRequested, Changed: action.Changed,
				PotentialDiscardBytes: action.PotentialDiscardBytes, RemovedBytes: action.RemovedBytes, FreeSpaceDelta: action.FreeSpaceDelta,
			})
		}
	}
	return report.Build(report.Input{
		At: now, Demo: demo, Overview: &overview, SMART: &s, Trim: &t, Power: &p,
		Findings: deduplicated, Actions: results,
	}), nil
}

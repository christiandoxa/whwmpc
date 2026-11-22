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

type Check string

const (
	SystemCheck Check = "system"
	DiskCheck   Check = "disk"
	PowerCheck  Check = "power"
	SSDCheck    Check = "ssd"
	TRIMCheck   Check = "trim"
)

type Coverage struct {
	ID     Check
	Status domain.Availability // Empty means the requested check has not started.
}

type ScanProgress struct {
	Check       Check
	Step, Total int
}

type Snapshot struct {
	Input    report.Input
	Coverage []Coverage
}

func Collect(ctx context.Context, scanner *inspection.Scanner, overview domain.Overview, lang string, demo, full, fresh bool, progress func(ScanProgress)) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	total := 2
	if full {
		total = 4
	}
	step := func(check Check, n int) {
		if progress != nil && ctx.Err() == nil {
			progress(ScanProgress{check, n, total})
		}
	}
	step(SystemCheck, 1)
	if overview.CapturedAt.IsZero() || fresh || scanner.Now().Sub(overview.CapturedAt) > 15*time.Second {
		overview = scanner.Overview(ctx)
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	step(PowerCheck, 2)
	p := scanner.Power(ctx)
	snap := Snapshot{
		Input: report.Input{Demo: demo, Overview: &overview, Power: &p},
		Coverage: []Coverage{
			{SystemCheck, combine(overview.CPU.Availability, overview.Memory.Availability)},
			{DiskCheck, overview.DiskStatus},
			{PowerCheck, combine(p.PowerStatus, p.Profiles.Status, p.CPUFreqStatus)},
		},
	}
	if full && ctx.Err() == nil {
		step(SSDCheck, 3)
		s := scanner.SMART(ctx, fresh)
		step(TRIMCheck, 4)
		t := scanner.TRIM(ctx)
		snap.Input.SMART, snap.Input.Trim = &s, &t
		snap.Coverage = append(snap.Coverage, Coverage{SSDCheck, s.Availability}, Coverage{TRIMCheck, t.Availability})
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	snap.Input.At = scanner.Now()
	a := diagnostics.Analyzer{Language: lang, Now: snap.Input.At}
	findings := a.Slow(overview, p)
	if full {
		findings = append(findings, a.SMART(*snap.Input.SMART)...)
		findings = append(findings, a.TRIM(*snap.Input.Trim)...)
	}
	findings = append(findings, a.Power(p)...)
	seen := make(map[string]bool)
	for _, f := range findings {
		if !seen[f.ID] {
			seen[f.ID] = true
			snap.Input.Findings = append(snap.Input.Findings, f)
		}
	}
	return snap, nil
}

func combine(status ...domain.Availability) domain.Availability {
	first := status[0]
	for _, value := range status[1:] {
		if first != value {
			return domain.Partial
		}
	}
	return first
}

func ScanReport(ctx context.Context, scanner *inspection.Scanner, overview domain.Overview, lang string, demo bool, entries []state.Entry) (report.Report, error) {
	snapshot, err := Collect(ctx, scanner, overview, lang, demo, true, false, nil)
	if err != nil {
		return report.Report{}, err
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
	snapshot.Input.Actions = results
	return report.Build(snapshot.Input), nil
}

package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/privread"
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
	snap := Snapshot{Input: report.Input{Demo: demo, Overview: &overview, Power: &p}}
	if full && ctx.Err() == nil {
		step(SSDCheck, 3)
		s := scanner.SMART(ctx, fresh)
		step(TRIMCheck, 4)
		t := scanner.TRIM(ctx)
		snap.Input.SMART, snap.Input.Trim = &s, &t
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	snap.finish(scanner.Now(), lang)
	return snap, nil
}

// Complete keeps the other readings in the original snapshot at their original times.
func Complete(ctx context.Context, scanner *inspection.Scanner, snap Snapshot, op privread.Operation, lang string, progress func(ScanProgress)) (Snapshot, error) {
	if snap.Input.Overview == nil || snap.Input.Power == nil {
		return Snapshot{}, errors.New("original overview is missing")
	}
	ctx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	step := func(check Check) {
		if progress != nil && ctx.Err() == nil {
			progress(ScanProgress{check, 1, 1})
		}
	}
	switch op {
	case privread.SMART:
		step(SSDCheck)
		r := scanner.SMART(ctx, true)
		snap.Input.SMART = &r
	case privread.TRIM:
		step(TRIMCheck)
		r := scanner.TRIM(ctx)
		snap.Input.Trim = &r
	case privread.Power:
		step(PowerCheck)
		r := scanner.Power(ctx)
		snap.Input.Power = &r
	case privread.Temperature:
		step(SystemCheck)
		if reading, ok := scanner.PrivilegedTemperature(ctx); ok {
			copy := *snap.Input.Overview
			copy.Temperature = reading
			snap.Input.Overview = &copy
		}
	default:
		return Snapshot{}, errors.New("check is outside the overview")
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	snap.finish(scanner.Now(), lang)
	return snap, nil
}

func (snap *Snapshot) finish(at time.Time, lang string) {
	in := &snap.Input
	in.At = at
	powerCoverage := in.Power.PowerStatus
	if in.Power.LinuxInterfaces() {
		powerCoverage = combine(powerCoverage, in.Power.Profiles.Status, in.Power.CPUFreqStatus)
	}
	snap.Coverage = []Coverage{
		{SystemCheck, combine(in.Overview.CPU.Availability, in.Overview.Memory.Availability)},
		{DiskCheck, in.Overview.DiskStatus},
		{PowerCheck, powerCoverage},
	}
	a := diagnostics.Analyzer{Language: lang, Now: at}
	findings := a.Slow(*in.Overview, *in.Power)
	if in.SMART != nil {
		findings = append(findings, a.SMART(*in.SMART)...)
		snap.Coverage = append(snap.Coverage, Coverage{SSDCheck, in.SMART.Availability})
	}
	if in.Trim != nil {
		findings = append(findings, a.TRIM(*in.Trim)...)
		snap.Coverage = append(snap.Coverage, Coverage{TRIMCheck, in.Trim.Availability})
	}
	findings = append(findings, a.Power(*in.Power)...)
	seen := make(map[string]bool)
	in.Findings = nil
	for _, f := range findings {
		if !seen[f.ID] {
			seen[f.ID] = true
			in.Findings = append(in.Findings, f)
		}
	}
}

func combine(status ...domain.Availability) domain.Availability {
	available, withData := 0, 0
	inaccessible := domain.Availability("")
	for _, value := range status {
		switch value {
		case domain.Available:
			available++
			withData++
		case domain.Partial:
			withData++
		case domain.Error:
			inaccessible = domain.Error
		case domain.PermissionRequired:
			if inaccessible != domain.Error {
				inaccessible = value
			}
		default:
			if inaccessible == "" {
				inaccessible = value
			}
		}
	}
	if available == len(status) && available > 0 {
		return domain.Available
	}
	if withData > 0 {
		return domain.Partial
	}
	return inaccessible
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

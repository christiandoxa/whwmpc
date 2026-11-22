package workflow

import (
	"context"
	"time"

	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/report"
)

const ObservationWindow = 12 * time.Second

func Observe(ctx context.Context, scanner *inspection.Scanner, lang string, demo bool, window time.Duration, progress func(ScanProgress)) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, window+20*time.Second)
	defer cancel()
	start := time.Now()
	stop := reportObservationTime(ctx, start, window, progress)
	overview := scanner.Observe(ctx, window/3)
	stop()
	elapsed := time.Since(start)
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	if progress != nil {
		progress(ScanProgress{Check: PowerCheck, Step: 2, Total: 2})
	}
	p := scanner.Power(ctx)
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	snap := Snapshot{Input: report.Input{Demo: demo, Overview: &overview, Power: &p}, ObservedFor: elapsed}
	snap.finish(scanner.Now(), lang)
	return snap, nil
}

func reportObservationTime(ctx context.Context, start time.Time, window time.Duration, progress func(ScanProgress)) func() {
	if progress == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	progress(ScanProgress{Check: SystemCheck, Window: window})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case at := <-ticker.C:
				progress(ScanProgress{Check: SystemCheck, Window: window, Elapsed: at.Sub(start)})
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

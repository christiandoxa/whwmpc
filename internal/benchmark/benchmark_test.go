package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestBoundedAndIsolatedWorkloads(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	got, err := Run(ctx, Config{Mode: Write, Path: dir, Bytes: block})
	entries, _ := os.ReadDir(dir)
	if err != nil || !got.Complete || got.ProcessedBytes != block || len(entries) != 0 {
		t.Fatalf("write=%+v, %v, entries=%v", got, err, entries)
	}
	file := filepath.Join(dir, "sample")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = Run(ctx, Config{Mode: Read, Path: file, Bytes: 4})
	if err != nil || got.ProcessedBytes != 4 {
		t.Fatalf("read=%+v %v", got, err)
	}
	if _, err := Run(ctx, Config{Mode: Read, Path: dir, Bytes: 4}); err == nil {
		t.Fatal("read accepted directory")
	}
	if _, err := Run(ctx, Config{Mode: Write, Path: dir, Bytes: maxBytes + 1}); err == nil {
		t.Fatal("accepted oversized write")
	}
}

func TestCPUClockAndThermalPrecondition(t *testing.T) {
	ctx := context.Background()
	var now time.Time
	clock := func() time.Time { now = now.Add(time.Millisecond); return now }
	r, err := run(ctx, Config{Mode: CPU, Duration: 4 * time.Millisecond}, clock)
	if err != nil || r.ProcessedBytes == 0 || !r.Complete {
		t.Fatalf("cpu=%+v %v", r, err)
	}
	for _, temp := range []*float64{nil, new(85.0)} {
		got, err := run(ctx, Config{Mode: Stress, Duration: time.Second, Temperature: func(context.Context) *float64 { return temp }}, clock)
		if err == nil || got.ProcessedBytes != 0 {
			t.Fatal("unsafe stress started")
		}
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err := run(cancelled, Config{Mode: CPU, Duration: time.Second}, clock); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestStressStopsAfterThermalGuardFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		next func(context.Context, context.CancelFunc) *float64
		err  error
		peak float64
	}{
		{"hot", func(context.Context, context.CancelFunc) *float64 { return new(86.0) }, errTemperatureLimit, 86},
		{"missing", func(context.Context, context.CancelFunc) *float64 { return nil }, errTemperatureUnavailable, 55},
		{"timeout", func(ctx context.Context, _ context.CancelFunc) *float64 {
			<-ctx.Done()
			return new(60.0)
		}, errTemperatureTimeout, 55},
		{"cancel", func(_ context.Context, stop context.CancelFunc) *float64 {
			stop()
			return new(60.0)
		}, context.Canceled, 55},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, stop := context.WithCancel(t.Context())
				defer stop()
				var reads atomic.Int32
				var now time.Time
				got, err := run(ctx, Config{
					Mode: Stress, Duration: 3 * time.Second,
					Temperature: func(ctx context.Context) *float64 {
						if reads.Add(1) == 1 {
							return new(55.0)
						}
						return tc.next(ctx, stop)
					},
				}, func() time.Time { now = now.Add(250 * time.Millisecond); return now })
				if !errors.Is(err, tc.err) || got.Complete || !got.ThermalGuard ||
					got.ProcessedBytes != 3*block || got.PeakCelsius == nil || *got.PeakCelsius != tc.peak || reads.Load() != 2 {
					t.Fatalf("unsafe continuation after second reading: %+v, err=%v, reads=%d", got, err, reads.Load())
				}
			})
		})
	}
}

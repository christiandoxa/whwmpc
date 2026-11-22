package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

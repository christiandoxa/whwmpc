package benchmark

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"github.com/christiandoxa/whwmpc/internal/state"
	"io"
	"os"
	"runtime"
	"time"
)

type Mode string

const (
	CPU              Mode = "cpu"
	Stress           Mode = "stress"
	Read             Mode = "read"
	Write            Mode = "write"
	block                 = 1 << 20
	maxBytes              = 64 * block
	temperatureLimit      = 85.0
)

type Config struct {
	Mode        Mode
	Duration    time.Duration
	Bytes       int64
	Path        string
	Temperature func(context.Context) *float64
}

type Result struct {
	Mode           Mode     `json:"mode"`
	OS             string   `json:"os"`
	Architecture   string   `json:"architecture"`
	LogicalCPUs    int      `json:"logical_cpus"`
	DurationSec    float64  `json:"duration_seconds"`
	ProcessedBytes int64    `json:"processed_bytes"`
	MiBPerSec      float64  `json:"mib_per_second"`
	Digest         [32]byte `json:"digest,omitzero"`
	ThermalGuard   bool     `json:"thermal_guard"`
	PeakCelsius    *float64 `json:"peak_celsius,omitempty"`
	Complete       bool     `json:"complete"`
}

func Run(ctx context.Context, c Config) (Result, error) { return run(ctx, c, time.Now) }

func run(ctx context.Context, c Config, now func() time.Time) (Result, error) {
	out := Result{Mode: c.Mode, OS: runtime.GOOS, Architecture: runtime.GOARCH, LogicalCPUs: runtime.NumCPU()}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	begin := now()
	var err error
	switch c.Mode {
	case CPU, Stress:
		if c.Duration <= 0 || c.Duration > 30*time.Second || (c.Mode == CPU && c.Duration > 5*time.Second) {
			return out, errors.New("batas CPU 5 detik dan stress 30 detik")
		}
		var temperatures <-chan *float64
		if c.Mode == Stress {
			if c.Temperature == nil {
				return out, errors.New("stress memerlukan sensor suhu CPU")
			}
			temp := c.Temperature(ctx)
			if !validTemperature(temp) {
				return out, errors.New("stress memerlukan data suhu CPU")
			}
			out.ThermalGuard, out.PeakCelsius = true, temp
			if *temp >= temperatureLimit {
				return out, errors.New("suhu CPU mencapai batas penghentian 85 °C")
			}
			guardCtx, stop := context.WithCancel(ctx)
			defer stop()
			temperatures = sampleTemperature(guardCtx, c.Temperature)
		}
		begin = now()
		data := make([]byte, block)
		for now().Sub(begin) < c.Duration {
			var stop error
			select {
			case <-ctx.Done():
				stop = ctx.Err()
			case current := <-temperatures:
				if !validTemperature(current) {
					stop = errors.New("sensor suhu tidak lagi tersedia")
				} else {
					if *current > *out.PeakCelsius {
						out.PeakCelsius = current
					}
					if *current >= temperatureLimit {
						stop = errors.New("stress dihentikan pada batas 85 °C")
					}
				}
			default:
			}
			if stop != nil {
				err = stop
				break
			}
			out.Digest = sha256.Sum256(data)
			copy(data, out.Digest[:])
			out.ProcessedBytes += int64(len(data))
		}
	case Read, Write:
		if c.Bytes <= 0 || c.Bytes > maxBytes || c.Path == "" {
			return out, errors.New("pilih lokasi dan ukuran antara 1 byte dan 64 MiB")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if c.Mode == Read {
			out.ProcessedBytes, err = read(ctx, c.Path, c.Bytes)
		} else {
			out.ProcessedBytes, err = write(ctx, c.Path, c.Bytes)
		}
	default:
		return out, errors.New("beban benchmark tidak dikenal")
	}
	if err == nil {
		err = ctx.Err()
	}
	out.DurationSec = now().Sub(begin).Seconds()
	if out.DurationSec > 0 {
		out.MiBPerSec = float64(out.ProcessedBytes) / float64(block) / out.DurationSec
	}
	out.Complete = err == nil
	return out, err
}

func validTemperature(t *float64) bool { return t != nil && *t >= -30 && *t <= 150 }

func sampleTemperature(ctx context.Context, probe func(context.Context) *float64) <-chan *float64 {
	results := make(chan *float64, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t := probe(ctx)
				select {
				case <-ctx.Done():
					return
				case results <- t:
				}
			}
		}
	}()
	return results
}

func read(ctx context.Context, path string, n int64) (int64, error) {
	// OpenRegular rejects device nodes and FIFOs before copying.
	file, err := state.OpenRegular(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() < n {
		return 0, errors.New("file lebih kecil dari beban baca")
	}
	return transfer(ctx, io.Discard, file, n)
}

func write(ctx context.Context, path string, n int64) (written int64, err error) {
	dir, err := os.OpenRoot(path)
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	name := ".whwmpc-benchmark-" + rand.Text()
	file, err := dir.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, dir.Remove(name)) }()
	written, err = transfer(ctx, file, rand.Reader, n)
	if err == nil {
		err = file.Sync()
	}
	return written, errors.Join(err, file.Close())
}

func transfer(ctx context.Context, output io.Writer, input io.Reader, n int64) (int64, error) {
	buffer := make([]byte, block)
	var count int64
	for count < n {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		chunk := buffer[:min(n-count, block)]
		if _, err := io.ReadFull(input, chunk); err != nil {
			return count, err
		}
		written, err := output.Write(chunk)
		count += int64(written)
		if err != nil {
			return count, err
		}
		if written != len(chunk) {
			return count, io.ErrShortWrite
		}
	}
	return count, nil
}

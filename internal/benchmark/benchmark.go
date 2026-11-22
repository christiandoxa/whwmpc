package benchmark

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/christiandoxa/whwmpc/internal/state"
)

type Mode string

const (
	CPU                 Mode = "cpu"
	Stress              Mode = "stress"
	Read                Mode = "read"
	Write               Mode = "write"
	block                    = 1 << 20
	maxBytes                 = 64 * block
	temperatureLimit         = 85.0
	temperatureInterval      = time.Second
)

var (
	errTemperatureUnavailable = errors.New("pembacaan sensor suhu CPU tidak tersedia")
	errTemperatureTimeout     = errors.New("pembacaan sensor suhu CPU melewati batas waktu")
	errTemperatureLimit       = errors.New("suhu CPU mencapai batas penghentian 85 °C")
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
		if c.Mode == Stress {
			if err := out.checkTemperature(ctx, c.Temperature, temperatureInterval); err != nil {
				return out, err
			}
		}
		begin = now()
		nextTemperature := begin.Add(temperatureInterval)
		data := make([]byte, block)
		for at := now(); at.Sub(begin) < c.Duration; at = now() {
			if err = ctx.Err(); err != nil {
				break
			}
			if c.Mode == Stress && !at.Before(nextTemperature) {
				// Hold the workload while a fresh temperature reading is pending.
				remaining := c.Duration - at.Sub(begin)
				if err = out.checkTemperature(ctx, c.Temperature, min(remaining, temperatureInterval)); err != nil {
					break
				}
				nextTemperature = at.Add(temperatureInterval)
				continue
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

func (out *Result) checkTemperature(ctx context.Context, probe func(context.Context) *float64, timeout time.Duration) error {
	if probe == nil {
		return errTemperatureUnavailable
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results := make(chan *float64, 1)
	go func() { results <- probe(probeCtx) }()
	var current *float64
	select {
	case current = <-results:
	case <-probeCtx.Done():
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if probeCtx.Err() != nil {
		return errTemperatureTimeout
	}
	if !validTemperature(current) {
		return errTemperatureUnavailable
	}
	out.ThermalGuard = true
	if out.PeakCelsius == nil || *current > *out.PeakCelsius {
		out.PeakCelsius = new(*current)
	}
	if *current >= temperatureLimit {
		return errTemperatureLimit
	}
	return nil
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

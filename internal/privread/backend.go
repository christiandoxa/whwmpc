package privread

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"runtime"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func Native() Backend {
	var ops []Operation
	switch runtime.GOOS {
	case "linux":
		ops = []Operation{SMART, TRIM, Power, Temperature, GPU, Firmware, Filesystem}
	case "darwin", "windows":
		ops = []Operation{SMART, TRIM, Filesystem}
	}
	return Backend{Operations: ops, Check: nativeCapability, Start: nativeStart}
}

func RunHelper(args []string) int {
	if !elevated() {
		return 1
	}
	scope, pipe, err := helperPipe(args)
	if err != nil || !allowed(scope, SMART) {
		return 1
	}
	defer pipe.Close()
	if err := helperEnvironment(); err != nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), Lifetime)
	defer cancel()
	// The helper must expire even if its peer stops reading a pipe.
	watchdog := time.AfterFunc(Lifetime+2*time.Second, func() { os.Exit(1) })
	defer watchdog.Stop()
	err = serve(ctx, pipe, scope, Native().Operations, collect)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.DeadlineExceeded) {
		return 1
	}
	return 0
}

func Elevated() bool { return elevated() }

func collect(ctx context.Context, op Operation) any {
	timeout := 18 * time.Second
	if op == SMART {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r := readRunner{op: op, native: sysrun.Native{Timeout: timeout}}
	c := storage.Collector{Runner: r}
	switch op {
	case SMART:
		return c.InspectSMART(ctx)
	case TRIM:
		audit := c.InspectTrim(ctx)
		wire := TrimReading{Audit: audit}
		for _, m := range audit.Mounts {
			wire.Paths = append(wire.Paths, TrimPath{m.ID, m.Point, m.Source, m.Root, m.MountOptions})
		}
		return wire
	case Power:
		snapshot := power.Scan(ctx, os.DirFS("/"), r)
		return PowerReading{Snapshot: snapshot, Target: snapshot.Profiles.Target, API: snapshot.Profiles.API}
	case Temperature:
		if sensor, ok := any(platform.Native{}).(interface{ Temperature() domain.Temperature }); ok {
			return sensor.Temperature()
		}
		return domain.Temperature{Availability: domain.Unsupported}
	case GPU:
		snap := hardware.Scan(ctx, r)
		wire := GPUReading{Snapshot: snap}
		for _, d := range snap.Devices {
			wire.IDs = append(wire.IDs, d.ID)
		}
		return wire
	case Firmware:
		return firmware.Collect(ctx, r, false)
	case Filesystem:
		return filesystem.Discover(ctx, r)
	default:
		return nil
	}
}

type TrimReading struct {
	Audit storage.TrimAudit
	Paths []TrimPath
}

type TrimPath struct {
	ID                  uint64
	Point, Source, Root string
	Options             []string
}

type GPUReading struct {
	Snapshot hardware.Snapshot
	IDs      []string
}

type PowerReading struct {
	Snapshot    power.Snapshot
	Target, API string
}

func validResult(op Operation, out any) bool {
	status := domain.Availability("")
	switch v := out.(type) {
	case *storage.SMARTAudit:
		if op != SMART || v.CapturedAt.IsZero() {
			return false
		}
		status = v.Availability
	case *TrimReading:
		if op != TRIM || v.Audit.CapturedAt.IsZero() || len(v.Paths) != len(v.Audit.Mounts) {
			return false
		}
		for i, path := range v.Paths {
			if path.ID != v.Audit.Mounts[i].ID {
				return false
			}
		}
		status = v.Audit.Availability
	case *PowerReading:
		if op != Power || v.Snapshot.At.IsZero() {
			return false
		}
		status = v.Snapshot.CPUFreqStatus
	case *domain.Temperature:
		if op != Temperature || v.Availability == domain.Available && v.Celsius == nil ||
			v.Celsius != nil && (math.IsNaN(*v.Celsius) || *v.Celsius < -30 || *v.Celsius > 150) {
			return false
		}
		status = v.Availability
	case *GPUReading:
		if op != GPU || v.Snapshot.At.IsZero() || len(v.IDs) != len(v.Snapshot.Devices) {
			return false
		}
		status = v.Snapshot.Availability
	case *firmware.Snapshot:
		if op != Firmware {
			return false
		}
		status = v.Availability
	case *filesystem.Snapshot:
		if op != Filesystem {
			return false
		}
		status = v.Availability
	}
	switch status {
	case domain.Available, domain.Partial, domain.Unknown, domain.Unsupported, domain.PermissionRequired, domain.DependencyMissing, domain.Error, domain.Stale:
		return true
	}
	return false
}

type readRunner struct {
	op     Operation
	native sysrun.Native
}

func (r readRunner) LookPath(name string) (string, error) { return trustedTool(r.op, name) }

func (r readRunner) Run(ctx context.Context, file string, args ...string) sysrun.Result {
	path, err := trustedTool(r.op, file)
	if err != nil || !readArgs(r.op, path, args) {
		return sysrun.Result{Err: os.ErrPermission, ExitCode: -1}
	}
	return r.native.Run(ctx, path, args...)
}

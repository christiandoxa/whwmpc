package platform

import (
	"errors"
	"io/fs"
	"math"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func nativeAvailability(err error) domain.Availability {
	switch {
	case err == nil:
		return domain.Available
	case errors.Is(err, fs.ErrPermission):
		return domain.PermissionRequired
	case errors.Is(err, fs.ErrNotExist):
		return domain.DependencyMissing
	default:
		return domain.Error
	}
}

func nativeDisk(mount, kind string, root bool, total, free uint64) domain.Disk {
	d := domain.Disk{Mount: mount, FSType: strings.ToLower(kind), Root: root, Availability: domain.Unknown}
	if total != 0 && free <= total {
		d.Availability, d.TotalBytes, d.FreeBytes = domain.Available, total, free
		d.UsedPercent = 100 * float64(total-free) / float64(total)
	}
	return d
}

func topNativeProcesses(processes []domain.Process) []domain.Process {
	slices.SortStableFunc(processes, func(a, b domain.Process) int {
		switch {
		case a.CPU > b.CPU || a.CPU == b.CPU && a.RSS > b.RSS:
			return -1
		case a.CPU < b.CPU || a.CPU == b.CPU && a.RSS < b.RSS:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})
	return processes[:min(len(processes), 5)]
}

func percentSample(free, total uint64) []float64 {
	if free > total || total == 0 {
		return nil
	}
	return []float64{100 * float64(total-free) / float64(total)}
}

func goodPercent(x float64) bool {
	return !math.IsNaN(x) && x >= 0 && x <= 100
}

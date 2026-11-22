package storage

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func (c Collector) runner() sysrun.Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return sysrun.Native{}
}

func (c Collector) findAdminTool(name string) (string, error) {
	runner := c.runner()
	path, err := runner.LookPath(name)
	if err == nil {
		return path, nil
	}
	for _, dir := range []string{"/usr/sbin", "/sbin"} {
		if fallback, err := runner.LookPath(filepath.Join(dir, name)); err == nil {
			return fallback, nil
		}
	}
	return "", err
}

func (c Collector) InspectSMART(ctx context.Context) SMARTAudit {
	ctx, cancel := context.WithTimeout(ctx, 22*time.Second)
	defer cancel()
	audit := SMARTAudit{CapturedAt: c.now()}
	graph, err := c.topology(ctx)
	if err != nil {
		audit.Availability = filesystemAvailability(err)
		audit.Detail = "Daftar perangkat block di sysfs tidak dapat dibaca."
		return audit
	}
	mounts, mountState, _ := c.mounts(ctx, graph)
	path, pathErr := c.findAdminTool("smartctl")
	for _, name := range slices.Sorted(maps.Keys(graph.byName)) {
		node := graph.byName[name]
		if !node.physical {
			continue
		}
		info := Drive{Availability: domain.Unknown, Name: name, DeviceID: node.DeviceID,
			Transport: node.transport, Rotational: node.rotational, CapturedAt: c.now()}
		for _, mount := range mounts {
			if slices.ContainsFunc(mount.Layers, func(layer BlockLayer) bool { return layer.Name == name }) {
				info.MountIDs = append(info.MountIDs, mount.ID)
			}
		}
		switch {
		case ctx.Err() != nil:
			info.Availability, info.Detail = domain.Unknown, "Perangkat belum diperiksa sebelum batas waktu scan."
		case node.transport == "virtual" || strings.HasPrefix(name, "mmcblk"):
			info.Availability, info.Detail = domain.Unsupported, "Perangkat ini tidak memiliki backend SMART yang didukung aplikasi."
		case pathErr != nil:
			info.Availability, info.Detail = domain.DependencyMissing, "smartctl dari paket smartmontools dibutuhkan."
		default:
			c.inspectDevice(ctx, path, node, &info)
		}
		audit.Drives = append(audit.Drives, info)
	}
	if len(audit.Drives) == 0 {
		audit.Availability, audit.Detail = domain.Unsupported, "Tidak ada perangkat fisik yang terlihat di sysfs."
		return audit
	}
	audit.Availability = audit.Drives[0].Availability
	for _, drive := range audit.Drives[1:] {
		if drive.Availability != audit.Availability {
			audit.Availability = domain.Partial
			break
		}
	}
	if mountState != domain.Available {
		if audit.Availability == domain.Available {
			audit.Availability = domain.Partial
		}
		audit.Detail = "Pemetaan SMART ke mount belum lengkap."
	}
	return audit
}

func (c Collector) inspectDevice(ctx context.Context, executable string, node blockDevice, drive *Drive) {
	args := []string{"--json=v", "--info", "--health", "--attributes", "--quietmode=noserial"}
	if node.transport == "NVMe" {
		args = append(args, "--device=nvme")
	} else if node.rotational != nil && *node.rotational {
		args = append(args, "--nocheck=standby")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	result := c.runner().Run(probeCtx, executable, append(args, "/dev/"+node.Name)...)
	drive.CapturedAt = c.now()
	devicePath := c.path(filepath.Join("/sys/class/block", node.Name))
	if readText(filepath.Join(devicePath, "dev")) != node.DeviceID ||
		(node.DiskSequence != "" && readText(filepath.Join(devicePath, "diskseq")) != node.DiskSequence) {
		drive.Availability, drive.Detail = domain.Stale, "Identitas perangkat berubah saat pembacaan SMART."
		return
	}
	if result.Err != nil {
		drive.Availability = domain.Error
		drive.Detail = "Pembacaan smartctl gagal atau terputus."
		if errors.Is(result.Err, os.ErrPermission) {
			drive.Availability, drive.Detail = domain.PermissionRequired, "Izin menjalankan smartctl dibutuhkan."
		}
		return
	}
	parsed, err := decodeSMART(result.Output, result.ExitCode)
	if err != nil {
		drive.Availability, drive.Detail = domain.Error, "Keluaran JSON smartctl tidak dapat dipakai."
		if denied(string(result.Stderr)) {
			drive.Availability, drive.Detail = domain.PermissionRequired, "Izin untuk membaca data SMART dibutuhkan."
		}
		return
	}
	drive.Availability, drive.Detail = parsed.Availability, parsed.Detail
	drive.Model, drive.Health, drive.Protocol = parsed.Model, parsed.Health, parsed.Protocol
}

func (c Collector) InspectTrim(ctx context.Context) TrimAudit {
	audit := TrimAudit{CapturedAt: c.now(), Manual: domain.Available}
	if _, err := c.findAdminTool("fstrim"); err != nil {
		audit.Manual = domain.DependencyMissing
	}
	graph, err := c.topology(ctx)
	audit.Mounts, audit.Availability, audit.Detail = c.mounts(ctx, graph)
	if audit.Availability == domain.Available && err != nil {
		audit.Availability, audit.Detail = domain.Partial, "Jalur block dari sysfs belum tersedia."
	}
	audit.Timer = c.inspectTimer(ctx, audit.Mounts, graph)
	if audit.Availability == domain.Available && (audit.Manual != domain.Available ||
		audit.Timer.Availability != domain.Available || slices.ContainsFunc(audit.Mounts, func(mount Mount) bool {
		return mount.Discard == domain.Unknown || mount.Discard == domain.Partial
	})) {
		audit.Availability = domain.Partial
	}
	return audit
}

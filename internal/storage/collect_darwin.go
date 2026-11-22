package storage

import (
	"context"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func (c Collector) InspectSMART(ctx context.Context) SMARTAudit {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	runner := c.Runner
	if runner == nil {
		runner = sysrun.Native{}
	}
	audit := SMARTAudit{Source: DiskUtilitySource, CapturedAt: c.now()}
	result := runner.Run(ctx, "/usr/sbin/diskutil", "list", "-plist", "physical")
	audit.Availability = nativeCommandStatus(result)
	if audit.Availability != domain.Available {
		return audit
	}
	names, err := parseDarwinDisks(result.Output)
	if err != nil {
		audit.Availability = domain.Error
		return audit
	}
	for _, name := range names[:min(len(names), 16)] {
		if ctx.Err() != nil {
			break
		}
		out := runner.Run(ctx, "/usr/sbin/diskutil", "info", "-plist", name)
		status := nativeCommandStatus(out)
		if status != domain.Available {
			audit.Drives = append(audit.Drives, Drive{Name: name, Availability: status, CapturedAt: c.now()})
			continue
		}
		audit.Drives = append(audit.Drives, parseDarwinSMART(name, out.Output, c.now()))
	}
	audit.Availability = nativeDriveStatus(audit.Drives)
	if len(audit.Drives) < len(names) {
		audit.Availability = domain.Partial
		audit.Detail = "Tidak semua perangkat selesai diperiksa dalam batas waktu dan jumlah."
	}
	return audit
}

func (c Collector) InspectTrim(ctx context.Context) TrimAudit {
	runner := c.Runner
	if runner == nil {
		runner = sysrun.Native{}
	}
	audit := TrimAudit{Source: DarwinTRIMSource, CapturedAt: c.now(), Manual: domain.Unsupported,
		Timer: Timer{Availability: domain.Unsupported, Journal: domain.Unsupported}}
	result := runner.Run(ctx, "/usr/sbin/system_profiler", "-json", "-detailLevel", "full", "SPNVMeDataType", "SPSerialATADataType")
	audit.Availability = nativeCommandStatus(result)
	if audit.Availability != domain.Available {
		return audit
	}
	policy, err := parseDarwinTrim(result.Output)
	if err != nil {
		audit.Availability = domain.Error
		return audit
	}
	audit.Policy = policy
	audit.Availability = domain.Partial
	if len(policy) == 0 {
		audit.Availability = domain.Unknown
	}
	audit.Detail = "System Information hanya melaporkan dukungan yang terlihat, tanpa riwayat discard per volume."
	return audit
}

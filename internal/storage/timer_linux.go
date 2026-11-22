package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func showProperties(output []byte) map[string]string {
	values := make(map[string]string)
	for line := range strings.SplitSeq(string(output), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

func optionalProperty(raw string) string {
	if raw == "" || raw == "n/a" || raw == "0" || raw == "infinity" {
		return ""
	}
	return cleanLabel(raw)
}

func (c Collector) inspectTimer(ctx context.Context, mounts []Mount, graph topology) Timer {
	timer := Timer{Availability: domain.Unknown, Journal: domain.Unknown}
	systemd, err := os.Stat(c.path("/run/systemd/system"))
	if errors.Is(err, os.ErrPermission) {
		timer.Availability, timer.Detail = domain.PermissionRequired, "Izin untuk mendeteksi manager systemd dibutuhkan."
		return timer
	}
	if err != nil || !systemd.IsDir() {
		timer.Scheduler = "unknown"
		timer.Detail = "Manager systemd tidak terdeteksi. Jadwal alternatif dan hasil TRIM perlu diperiksa secara terpisah."
		timer.Availability = domain.Unsupported
		for _, executable := range []string{"cron", "crond"} {
			if _, err := c.findAdminTool(executable); err == nil {
				timer.Scheduler = "cron"
				timer.Availability = domain.Unknown
				timer.Detail = "Cron tersedia. Entri jadwal fstrim dan riwayat eksekusinya belum terverifikasi."
				break
			}
		}
		return timer
	}
	timer.Scheduler = "systemd"
	systemctl, err := c.runner().LookPath("systemctl")
	if err != nil {
		timer.Availability, timer.Detail = domain.DependencyMissing, "systemctl diperlukan untuk membaca timer fstrim."
		return timer
	}
	state := c.runner().Run(ctx, systemctl, "--no-pager", "show",
		"--property=LoadState,UnitFileState,ActiveState,ActiveEnterTimestampMonotonic,SubState,Unit,NextElapseUSecRealtime,NextElapseUSecMonotonic,LastTriggerUSec,FragmentPath,DropInPaths,NeedDaemonReload",
		"fstrim.timer")
	props := showProperties(state.Output)
	timer.LoadState = cleanLabel(props["LoadState"])
	timer.UnitFileState = cleanLabel(props["UnitFileState"])
	timer.ActiveState = cleanLabel(props["ActiveState"])
	timer.ActiveEnterMonotonic = props["ActiveEnterTimestampMonotonic"]
	timer.SubState = cleanLabel(props["SubState"])
	timer.Unit = props["Unit"]
	timer.NextElapse = optionalProperty(props["NextElapseUSecRealtime"])
	timer.NextMonotonic = optionalProperty(props["NextElapseUSecMonotonic"])
	timer.LastTrigger = optionalProperty(props["LastTriggerUSec"])
	timer.FragmentPath = props["FragmentPath"]
	timer.DropInPaths = props["DropInPaths"]
	timer.NeedDaemonReload = props["NeedDaemonReload"] == "yes"
	switch {
	case timer.LoadState == "not-found":
		timer.Availability, timer.Detail = domain.Unsupported, "fstrim.timer tidak terpasang pada systemd ini."
		return timer
	case timer.LoadState == "masked":
		timer.Availability, timer.Detail = domain.Unsupported, "fstrim.timer diblokir oleh konfigurasi systemd."
		return timer
	case state.Err != nil || state.ExitCode != 0 || timer.LoadState != "loaded":
		timer.Availability, timer.Detail = domain.Error, "Status fstrim.timer tidak dapat dibaca dari systemd."
		if denied(string(state.Stderr)) {
			timer.Availability = domain.PermissionRequired
		}
		return timer
	case timer.Unit != "fstrim.service":
		timer.Availability, timer.Detail = domain.Partial, "Timer memakai service selain fstrim.service."
		return timer
	}

	service := c.runner().Run(ctx, systemctl, "--no-pager", "show",
		"--property=LoadState,ActiveState,Result,ExecMainStatus,ExecMainStartTimestamp,ExecMainExitTimestamp,FragmentPath,DropInPaths,ExecStart,InvocationID,NeedDaemonReload",
		"fstrim.service")
	props = showProperties(service.Output)
	timer.ServiceLoadState = cleanLabel(props["LoadState"])
	timer.ServiceActiveState = cleanLabel(props["ActiveState"])
	timer.LastServiceStart = optionalProperty(props["ExecMainStartTimestamp"])
	timer.LastServiceExit = optionalProperty(props["ExecMainExitTimestamp"])
	timer.ServiceFragmentPath = props["FragmentPath"]
	timer.ServiceDropInPaths = props["DropInPaths"]
	timer.ServiceExecStart = props["ExecStart"]
	timer.NeedDaemonReload = timer.NeedDaemonReload || props["NeedDaemonReload"] == "yes"
	if service.Err != nil || service.ExitCode != 0 || timer.ServiceLoadState != "loaded" {
		timer.Availability, timer.Detail = domain.Partial, "Timer terlihat, tetapi status fstrim.service belum tersedia."
		return timer
	}
	timer.Availability = domain.Available
	if timer.LastServiceExit != "" {
		timer.ServiceResult = cleanLabel(props["Result"])
		if code, err := strconv.Atoi(props["ExecMainStatus"]); err == nil {
			timer.ServiceExitCode = &code
		}
	}
	timer.Journal = c.inspectTrimJournal(ctx, props["InvocationID"], mounts, graph)
	return timer
}

var invocationID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var trimJournalMessage = regexp.MustCompile(`^(.+): [^:\r\n]+ \(([0-9]+) bytes\) trimmed on (/dev/[^\r\n]+)$`)

func (c Collector) inspectTrimJournal(ctx context.Context, invocation string, mounts []Mount, graph topology) domain.Availability {
	if !invocationID.MatchString(invocation) || invocation == strings.Repeat("0", 32) {
		return domain.Unknown
	}
	journalctl, err := c.runner().LookPath("journalctl")
	if err != nil {
		return domain.DependencyMissing
	}
	// Journal messages are display evidence only. A matching path cannot prove that the mount has never changed.
	result := c.runner().Run(ctx, journalctl, "--quiet", "--no-pager", "--output=json",
		"--output-fields=MESSAGE,__REALTIME_TIMESTAMP", "--lines=128",
		"_SYSTEMD_UNIT=fstrim.service", "_UID=0", "_TRANSPORT=stdout", "_SYSTEMD_INVOCATION_ID="+invocation)
	if result.Err != nil || result.ExitCode != 0 {
		if denied(string(result.Stderr)) {
			return domain.PermissionRequired
		}
		return domain.Error
	}
	reader := bufio.NewScanner(bytes.NewReader(result.Output))
	found := false
	for reader.Scan() {
		var row struct {
			Message string `json:"MESSAGE"`
			Stamp   string `json:"__REALTIME_TIMESTAMP"`
		}
		if json.Unmarshal(reader.Bytes(), &row) != nil {
			continue
		}
		match := trimJournalMessage.FindStringSubmatch(row.Message)
		stamp, err := strconv.ParseInt(row.Stamp, 10, 64)
		if len(match) == 0 || err != nil || stamp <= 0 {
			continue
		}
		count, err := strconv.ParseUint(match[2], 10, 64)
		if err != nil {
			continue
		}
		source := c.sourceDevice(match[3], graph)
		if source == "" {
			continue
		}
		for i := range mounts {
			mount := &mounts[i]
			if mount.Point != match[1] || c.sourceDevice(mount.Source, graph) != source {
				continue
			}
			at := time.UnixMicro(stamp).UTC()
			if mount.LastTrim == nil || at.After(mount.LastTrim.RecordedAt) {
				mount.LastTrim = &TrimRecord{RecordedAt: at, PotentialDiscardBytes: count}
			}
			found = true
		}
	}
	if reader.Err() != nil {
		return domain.Partial
	}
	if found {
		return domain.Available
	}
	return domain.Unknown
}

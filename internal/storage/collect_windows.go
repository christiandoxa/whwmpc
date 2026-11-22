package storage

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"golang.org/x/sys/windows"
)

// Static, local inspection through the system's Storage module. No device-supplied string enters the command.
const windowsStorageRead = `
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$ErrorActionPreference = 'Stop'
function Classify($record) {
  if ($record.CategoryInfo.Category -eq 'PermissionDenied' -or $record.Exception.HResult -eq -2147024891) { return 'permission' }
  if ($record.CategoryInfo.Category -eq 'ObjectNotFound' -or $record.Exception -is [System.Management.Automation.CommandNotFoundException]) { return 'missing' }
  return 'error'
}
try {
  Import-Module (Join-Path $PSHOME 'Modules\Storage\Storage.psd1') -ErrorAction Stop
  $found = @(Storage\Get-PhysicalDisk | Select-Object -First 65)
  $records = @(foreach ($disk in $found) {
    $counters = $null
    $status = 'unknown'
    try {
      $counter = @(Storage\Get-StorageReliabilityCounter -PhysicalDisk $disk)[0]
      if ($null -ne $counter) {
        $status = 'available'
        $fields = $counter.CimInstanceProperties
        $counters = @{temperature=$fields['Temperature'].Value; wear=$fields['Wear'].Value; hours=$fields['PowerOnHours'].Value;
          read=$fields['ReadErrorsUncorrected'].Value; write=$fields['WriteErrorsUncorrected'].Value}
      }
    } catch { $status = Classify $_ }
    $diskFields = $disk.CimInstanceProperties
    @{ model=$diskFields['FriendlyName'].Value; bus=$diskFields['BusType'].Value; media=$diskFields['MediaType'].Value;
      health=$diskFields['HealthStatus'].Value; counterStatus=$status; counters=$counters }
  })
  ConvertTo-Json -InputObject $records -Depth 4 -Compress
} catch {
  switch (Classify $_) {
    'permission' { exit 13 }
    'missing' { exit 127 }
    default { exit 1 }
  }
}
`

func (c Collector) InspectSMART(ctx context.Context) SMARTAudit {
	audit := SMARTAudit{Source: WindowsStorageSource, CapturedAt: c.now()}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		audit.Availability = domain.Error
		return audit
	}
	runner := c.Runner
	if runner == nil {
		runner = sysrun.Native{}
	}
	if native, ok := runner.(sysrun.Native); ok && native.Timeout <= 0 {
		runner = sysrun.Native{Timeout: 20 * time.Second}
	}
	result := runner.Run(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"),
		"-NoProfile", "-NoLogo", "-NonInteractive", "-Command", windowsStorageRead)
	audit.Availability = nativeCommandStatus(result)
	if audit.Availability != domain.Available {
		if errors.Is(result.Err, context.DeadlineExceeded) {
			audit.Detail = "Windows Storage provider belum selesai dalam batas waktu."
		}
		return audit
	}
	audit.Drives, err = parseWindowsSMART(result.Output, audit.CapturedAt)
	if err != nil {
		audit.Availability = domain.Error
	} else {
		audit.Availability = nativeDriveStatus(audit.Drives)
	}
	return audit
}

func (c Collector) InspectTrim(ctx context.Context) TrimAudit {
	audit := TrimAudit{Source: WindowsTRIMSource, CapturedAt: c.now(), Manual: domain.Unsupported,
		Timer: Timer{Availability: domain.Unsupported, Journal: domain.Unsupported}}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		audit.Availability = domain.Error
		return audit
	}
	runner := c.Runner
	if runner == nil {
		runner = sysrun.Native{}
	}
	result := runner.Run(ctx, filepath.Join(system, "fsutil.exe"), "behavior", "query", "DisableDeleteNotify")
	audit.Availability = nativeCommandStatus(result)
	if audit.Availability != domain.Available {
		if result.Err == nil && !windows.GetCurrentProcessToken().IsElevated() {
			audit.Availability = domain.PermissionRequired
		}
		return audit
	}
	audit.Policy = parseWindowsTrim(string(result.Output))
	audit.Availability = domain.Partial
	if len(audit.Policy) == 0 {
		audit.Availability = domain.Unknown
	}
	audit.Detail = "Kebijakan notifikasi penghapusan NTFS/ReFS berbeda dari bukti TRIM per volume."
	return audit
}

package diagnostics

import (
	"fmt"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

const smartSource = "smartctl JSON (SMART status, exit bitmask, NVMe Health log)"

func (a Analyzer) SMART(audit storage.SMARTAudit) []Finding {
	source := audit.Source
	if source == "" {
		source = smartSource
	}
	if len(audit.Drives) == 0 {
		status := a.status(audit.Availability, audit.CapturedAt, smartAge)
		if usable(status) {
			status = domain.Unknown
		}
		if source == smartSource {
			source = "/sys/class/block + " + source
		}
		f := a.unavailable(finding("smart.coverage", "ssd", source, audit.CapturedAt, status),
			a.text("Audit kesehatan penyimpanan", "Storage health audit"))
		return []Finding{f}
	}
	var findings []Finding
	for i, drive := range audit.Drives {
		findings = append(findings, a.smartDrive(drive, i+1, source)...)
	}
	return ranked(findings)
}

func (a Analyzer) smartDrive(drive storage.Drive, ordinal int, source string) []Finding {
	if source == storage.WindowsStorageSource {
		return a.windowsDiskHealth(drive, ordinal)
	}
	f := finding(fmt.Sprintf("smart.status.%d", ordinal), "ssd", source, drive.CapturedAt,
		a.status(drive.Availability, drive.CapturedAt, smartAge))
	label := a.textf("Drive #%d", "Drive #%d", ordinal)
	if !usable(f.Availability) || drive.Health == nil {
		if usable(f.Availability) {
			f.Availability = domain.Unknown
		}
		f = a.unavailable(f, label)
		switch f.Availability {
		case domain.DependencyMissing:
			if source == smartSource {
				f.Recommendation = a.text("Pasang smartmontools dari repositori distro agar smartctl tersedia.", "Install smartmontools from your distribution to make smartctl available.")
			}
		case domain.PermissionRequired:
			f.Recommendation = a.text("Minta administrator memeriksa akses baca SMART perangkat ini.", "Ask an administrator to check read access to this device's SMART data.")
		case domain.Unsupported:
			if drive.Transport == "USB" {
				f.Recommendation = a.text("Periksa dukungan SMART pass-through pada USB bridge dan tipe perangkat di smartmontools.", "Check the USB bridge's SMART pass-through support and its smartmontools device type.")
			}
		}
		return []Finding{f}
	}

	h := drive.Health
	fields := []string{label}
	if h.Passed != nil {
		if *h.Passed {
			fields = append(fields, a.text("SMART lulus", "SMART passed"))
		} else {
			fields = append(fields, a.text("SMART gagal", "SMART failed"))
		}
	}
	if h.CriticalWarning != nil {
		fields = append(fields, fmt.Sprintf("NVMe critical warning 0x%02X", *h.CriticalWarning))
	}
	if h.TemperatureC != nil {
		fields = append(fields, fmt.Sprintf("%d °C", *h.TemperatureC))
	}
	if h.Spare != nil {
		fields = append(fields, a.textf("spare %d%%", "%d%% spare", *h.Spare))
	}
	if h.SpareThreshold != nil {
		fields = append(fields, a.textf("ambang spare %d%%", "%d%% spare threshold", *h.SpareThreshold))
	}
	if source == smartSource {
		fields = append(fields, fmt.Sprintf("smartctl exit 0x%02X", h.ExitCode))
	}
	f.Observation = strings.Join(fields, " · ")
	f.Meaning = a.text("SMART merangkum indikator yang dilaporkan perangkat saat diperiksa.", "SMART summarizes indicators the device reported during the scan.")
	f.Limits = a.text("SMART mencakup indikator yang diekspos perangkat. Kerusakan mendadak tetap mungkin terjadi.", "SMART covers the indicators exposed by the device. Sudden failure remains possible.")
	f.Recommendation = a.text("Pertahankan cadangan rutin dan bandingkan temuan dari waktu ke waktu.", "Keep regular backups and compare readings over time.")
	f.Actions = []Action{a.rescan()}

	currentProblem := h.ExitCode&(storage.SMARTDiskFailing|storage.SMARTCurrentThreshold) != 0 ||
		h.Passed != nil && !*h.Passed || h.CriticalWarning != nil && *h.CriticalWarning != 0 ||
		h.Spare != nil && h.SpareThreshold != nil && *h.Spare <= 100 && *h.SpareThreshold <= 100 && *h.Spare < *h.SpareThreshold
	pastProblem := h.ExitCode&(storage.SMARTHistoricalThreshold|storage.SMARTErrorLog|storage.SMARTSelfTestLog) != 0 ||
		positiveCounter(h.MediaErrors)
	switch {
	case currentProblem:
		f.Severity = Critical
		f.Meaning = a.text("Perangkat atau alat baca melaporkan peringatan aktif atau ambang kegagalan terlampaui.", "The device or reader reports a current alert or a crossed failure threshold.")
		f.Recommendation = a.text("Prioritaskan cadangan data penting ke media lain. Periksa peringatan dan kesehatan drive setelah data aman.", "Prioritize backing up important data to another device. Investigate the alert after the data is safe.")
	case pastProblem:
		f.Severity = Warning
		f.Meaning = a.text("Ada rekaman kesalahan media, log, atau ambang SMART historis.", "Media errors, log records, or historical SMART threshold events were reported.")
		f.Recommendation = a.text("Pastikan cadangan mutakhir dan pantau apakah counter kesalahan bertambah.", "Check that backups are current and track whether error counters increase.")
	case h.Passed == nil && h.CriticalWarning == nil:
		f.Availability = domain.Partial
		f.Meaning = a.text("Metrik tersedia sebagian. Status kesehatan utama belum terlihat.", "Some metrics are available. The primary health indicators are missing.")
	}
	if h.ExitCode&(storage.SMARTInvalidArguments|storage.SMARTDeviceOpenFailed|storage.SMARTCommandFailed) != 0 {
		f.Availability = domain.Partial
		f.Limits += " " + a.text("Setidaknya satu tahap pembacaan gagal.", "At least one read step failed.")
	}

	findings := []Finding{f}
	if h.PercentageUsed != nil {
		findings = append(findings, a.endurance(drive, ordinal, *h.PercentageUsed, f.Availability))
	}
	if c, ok := a.smartCounters(drive, ordinal, f.Availability); ok {
		findings = append(findings, c)
	}
	return findings
}

func (a Analyzer) endurance(drive storage.Drive, ordinal int, used uint64, status domain.Availability) Finding {
	f := finding(fmt.Sprintf("smart.endurance.%d", ordinal), "ssd", "NVMe SMART/Health Log 0x02 (Percentage Used)", drive.CapturedAt, status)
	f.Observation = a.textf("Drive #%d · estimasi endurance terpakai %d%%.", "Drive #%d · estimated endurance used %d%%.", ordinal, used)
	f.Meaning = a.text("Nilai ini berasal dari model endurance vendor dan dapat melampaui 100%.", "This value comes from the vendor's endurance model and can exceed 100%.")
	f.Limits = a.text("Gunakan status SMART, peringatan NVMe, dan tren media errors untuk menilai kondisi bersama estimasi ini.", "Evaluate this estimate alongside SMART status, NVMe warnings, and media-error trends.")
	f.Recommendation = a.text("Pantau perubahan dan jaga cadangan tetap mutakhir.", "Monitor changes and keep backups current.")
	if used >= 100 {
		f.Severity = Warning
		f.Recommendation = a.text("Cadangkan data, pantau indikator lain, dan pertimbangkan rencana penggantian sesuai beban drive.", "Back up data, watch the other indicators, and consider replacement planning for this drive's workload.")
	}
	return f
}

func (a Analyzer) smartCounters(drive storage.Drive, ordinal int, status domain.Availability) (Finding, bool) {
	h := drive.Health
	var entries []string
	for _, counter := range [][2]string{
		{a.text("kesalahan media", "media errors"), h.MediaErrors},
		{a.text("shutdown tak aman", "unsafe shutdowns"), h.UnsafeShutdowns},
		{a.text("jam menyala", "power-on hours"), h.PowerOnHours},
		{a.text("unit data baca", "data units read"), h.DataUnitsRead},
		{a.text("unit data tulis", "data units written"), h.DataUnitsWritten},
	} {
		if decimalCounter(counter[1]) {
			entries = append(entries, counter[0]+" "+counter[1])
		}
	}
	if len(h.ATAAttributes) > 0 {
		entries = append(entries, a.textf("%d atribut ATA", "%d ATA attributes", len(h.ATAAttributes)))
	}
	if len(entries) == 0 {
		return Finding{}, false
	}
	f := finding(fmt.Sprintf("smart.counters.%d", ordinal), "ssd", smartSource, drive.CapturedAt, status)
	f.Observation = a.textf("Drive #%d · %s.", "Drive #%d · %s.", ordinal, strings.Join(entries, " · "))
	f.Meaning = a.text("Counter kumulatif berguna untuk melihat perubahan antar-pemeriksaan.", "Cumulative counters are useful for tracking changes between scans.")
	f.Limits = a.text("Unit NVMe baca/tulis setara 512.000 byte yang dibulatkan. Nilai 0 berarti tidak dilaporkan. Atribut ATA mengikuti vendor.", "Each NVMe read/write unit represents 512,000 bytes, rounded up. A zero value means unreported. ATA attributes are vendor-specific.")
	f.Recommendation = a.text("Bandingkan pemeriksaan berikutnya, terutama jumlah kesalahan media.", "Compare subsequent scans, especially the media-error count.")
	return f, true
}

func decimalCounter(value string) bool {
	return len(value) > 0 && len(value) <= 39 && !strings.ContainsFunc(value, func(r rune) bool { return r < '0' || r > '9' })
}

func positiveCounter(value string) bool {
	return decimalCounter(value) && strings.Trim(value, "0") != ""
}

func (a Analyzer) windowsDiskHealth(drive storage.Drive, ordinal int) []Finding {
	f := finding(fmt.Sprintf("smart.status.%d", ordinal), "ssd", storage.WindowsStorageSource, drive.CapturedAt,
		a.status(drive.Availability, drive.CapturedAt, smartAge))
	label := a.textf("Drive #%d", "Drive #%d", ordinal)
	if !usable(f.Availability) {
		return []Finding{a.unavailable(f, label)}
	}
	f.Observation = label
	f.Meaning = a.text("Ini pembacaan provider penyimpanan Windows.", "This is a reading from the Windows storage provider.")
	f.Limits = a.text("Provider dapat menyembunyikan counter. Status ini tidak membuktikan seluruh atribut SMART terbaca.", "The provider may hide counters. This status does not establish coverage of all SMART attributes.")
	f.Recommendation = a.text("Pertahankan backup berkala.", "Keep regular backups.")
	f.Actions = []Action{a.rescan()}
	switch drive.HealthStatus {
	case "healthy":
		f.Observation += a.text(" · provider melaporkan healthy.", " · provider reports healthy.")
	case "warning":
		f.Observation += a.text(" · provider melaporkan peringatan.", " · provider reports a warning.")
		f.Severity = Warning
	case "unhealthy":
		f.Observation += a.text(" · provider melaporkan perangkat tidak sehat.", " · provider reports an unhealthy device.")
		f.Severity = Critical
		f.Recommendation = a.text("Prioritaskan cadangan data penting ke perangkat lain.", "Prioritize backing up important data to another device.")
	default:
		f.Availability = domain.Partial
		f.Observation += a.text(" · status kesehatan belum terbaca.", " · health status unavailable.")
	}
	if h := drive.Health; h != nil {
		for _, entry := range []struct{ label, count string }{
			{a.text("jam menyala", "power-on hours"), h.PowerOnHours},
			{a.text("galat baca tak terkoreksi", "uncorrected reads"), h.ReadErrorsUncorrected},
			{a.text("galat tulis tak terkoreksi", "uncorrected writes"), h.WriteErrorsUncorrected},
		} {
			if entry.count != "" {
				f.Observation += fmt.Sprintf(" · %s %s", entry.label, entry.count)
			}
		}
		if h.TemperatureC != nil {
			f.Observation += fmt.Sprintf(" · %d °C", *h.TemperatureC)
		}
		if h.PercentageUsed != nil {
			f.Observation += a.textf(" · estimasi umur terpakai %d%%", " · estimated wear %d%%", *h.PercentageUsed)
		}
		if f.Severity == Info && (positiveCounter(h.ReadErrorsUncorrected) || positiveCounter(h.WriteErrorsUncorrected) ||
			h.PercentageUsed != nil && *h.PercentageUsed >= 100) {
			f.Severity = Warning
		}
	}
	return []Finding{f}
}

package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/domain"
)

var markdownPunctuation = strings.NewReplacer(
	"\\", "\\\\", "<", "&lt;", ">", "&gt;", "&", "&amp;",
	"[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "#", "\\#", "~", "\\~",
)

func md(value string) string { return markdownPunctuation.Replace(value) }

func markdown(report Report) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# What Happens With My PC\n\nSkema %d · %s\n\n", report.SchemaVersion, stamp(report.GeneratedAt))
	if report.Source == "demo" {
		out.WriteString("**DEMO** · Data contoh deterministik.\n")
	} else {
		out.WriteString("**Data nyata** · Laporan lokal dengan redaksi bawaan.\n")
	}
	if o := report.Overview; o != nil {
		fmt.Fprintf(&out, "\n## Ringkasan\n\nCakupan %s · Sampel %s\n\n", md(string(o.Availability)), stamp(o.CapturedAt))
		bullet(&out, "Sistem", nonempty(o.OS, o.Distro, o.Kernel, o.Arch, o.Environment))
		bullet(&out, "Uptime", optional(o.UptimeSeconds, "%.0f detik"))
		bullet(&out, "CPU", fmt.Sprintf("%d CPU logis · %s", o.LogicalCPUs, o.CPU.Availability))
		if latest, ok := o.CPU.Latest(); ok {
			bullet(&out, "Sampel CPU terakhir", fmt.Sprintf("%.1f%%", latest))
		}
		memory := string(o.Memory.Availability)
		if o.Memory.Availability == domain.Available || o.Memory.Availability == domain.Partial {
			memory += fmt.Sprintf(" · RAM %d/%d byte tersedia · swap %d/%d byte terpakai",
				o.Memory.FreeBytes, o.Memory.TotalBytes, o.Memory.SwapUsed, o.Memory.SwapBytes)
		}
		bullet(&out, "RAM dan swap", memory)
		bullet(&out, "Suhu", nonempty(optional(o.Temperature.Celsius, "%.1f °C"), string(o.Temperature.Availability)))
		bullet(&out, "Daya", nonempty(optional(o.OnAC, "daya eksternal %t"), optional(o.Battery, "baterai %.1f%%"), string(o.PowerStatus)))
		bullet(&out, "Cakupan filesystem", string(o.DiskStatus))
		for i, d := range o.Disks {
			label := fmt.Sprintf("Filesystem %d", i+1)
			if d.Root {
				label += " (root)"
			}
			capacity := ""
			if d.Availability == domain.Available || d.Availability == domain.Partial {
				capacity = fmt.Sprintf("%.1f%% terpakai · %d/%d byte tersedia", d.UsedPercent, d.FreeBytes, d.TotalBytes)
			}
			bullet(&out, label, nonempty(d.FSType, string(d.Availability), capacity))
		}
		bullet(&out, "Cakupan proses", string(o.ProcessStatus))
		for i, p := range o.Processes {
			bullet(&out, fmt.Sprintf("Proses %d", i+1), fmt.Sprintf("%.1f%% CPU host · RSS %d byte", p.CPU, p.RSS))
		}
	}
	if audit := report.SMART; audit != nil {
		fmt.Fprintf(&out, "\n## SSD dan SMART\n\nCakupan %s · Sampel %s\n\n", md(string(audit.Availability)), stamp(audit.CapturedAt))
		for i, drive := range audit.Drives {
			fmt.Fprintf(&out, "### Drive %d\n\n", i+1)
			bullet(&out, "Perangkat", nonempty(drive.DeviceID, drive.Transport, drive.Protocol, string(drive.Availability)))
			if h := drive.Health; h != nil {
				bullet(&out, "SMART", nonempty(optional(h.Passed, "lulus %t"), fmt.Sprintf("exit mask 0x%02X", h.ExitCode),
					optional(h.CriticalWarning, "peringatan NVMe 0x%02X"), optional(h.TemperatureC, "%d °C")))
				bullet(&out, "Spare", nonempty(optional(h.Spare, "%d%% tersedia"), optional(h.SpareThreshold, "ambang %d%%")))
				bullet(&out, "Estimasi endurance terpakai", optional(h.PercentageUsed, "%d%%"))
				bullet(&out, "Kesalahan media", h.MediaErrors)
				bullet(&out, "Shutdown tak aman", h.UnsafeShutdowns)
				bullet(&out, "Jam menyala", h.PowerOnHours)
				bullet(&out, "Unit data baca", h.DataUnitsRead)
				bullet(&out, "Unit data tulis", h.DataUnitsWritten)
			}
			out.WriteByte('\n')
		}
	}
	if audit := report.Trim; audit != nil {
		fmt.Fprintf(&out, "\n## TRIM\n\nCakupan %s · Sampel %s\n\n", md(string(audit.Availability)), stamp(audit.CapturedAt))
		t := audit.Timer
		bullet(&out, "Scheduler", nonempty(t.Scheduler, string(t.Availability)))
		bullet(&out, "Saat boot", t.UnitFileState)
		bullet(&out, "Saat ini", t.ActiveState)
		bullet(&out, "Jadwal berikutnya", nonempty(t.NextElapse, t.NextMonotonic))
		bullet(&out, "Pemicu terakhir", t.LastTrigger)
		bullet(&out, "Hasil service terakhir", nonempty(t.ServiceResult, t.LastServiceExit))
		bullet(&out, "Bukti jurnal", string(t.Journal))
		for i, mount := range audit.Mounts {
			fields := []string{mount.FSType, mount.DeviceID, string(mount.Discard), mount.DiscardMode}
			if mount.LastTrim != nil {
				fields = append(fields, stamp(mount.LastTrim.RecordedAt),
					fmt.Sprintf("%d byte potensial untuk discard", mount.LastTrim.PotentialDiscardBytes))
			}
			bullet(&out, fmt.Sprintf("Filesystem %d", i+1), nonempty(fields...))
		}
	}
	if p := report.Power; p != nil {
		fmt.Fprintf(&out, "\n## CPU dan daya\n\nSampel %s\n\n", stamp(p.At))
		bullet(&out, "Sumber daya", nonempty(p.PowerSource, string(p.PowerStatus)))
		bullet(&out, "CPUFreq", string(p.CPUFreqStatus))
		bullet(&out, "Boost", optional(p.Boost, "%t"))
		bullet(&out, "Profil platform", p.PlatformProfile)
		bullet(&out, "Pengelola profil", nonempty(p.Profiles.Backend, string(p.Profiles.ManagerStatus)))
		bullet(&out, "Profil aktif", p.Profiles.Active)
		bullet(&out, "Profil tersedia", strings.Join(p.Profiles.Available, ", "))
		bullet(&out, "Konflik pengelola", strings.Join(p.Profiles.Conflicts, ", "))
		bullet(&out, "Pembatasan", nonempty(p.Profiles.Degraded, p.Profiles.Inhibited))
		for i, policy := range p.Policies {
			bullet(&out, fmt.Sprintf("Policy CPU %d", i+1), nonempty(policy.Driver, policy.Governor, policy.EPP,
				optional(policy.ObservedKHz, "%d kHz teramati"), optional(policy.AverageKHz, "%d kHz rata-rata hardware"),
				optional(policy.DriverKHz, "%d kHz dari driver"), optional(policy.ConfigMinKHz, "%d kHz minimum konfigurasi"),
				optional(policy.ConfigMaxKHz, "%d kHz maksimum konfigurasi"), optional(policy.HardwareMinKHz, "%d kHz minimum hardware"),
				optional(policy.HardwareMaxKHz, "%d kHz maksimum hardware"), optional(policy.FirmwareCapKHz, "%d kHz batas firmware")))
		}
	}
	out.WriteString("\n## Temuan\n\n")
	if len(report.Findings) == 0 {
		out.WriteString("Belum ada temuan.\n")
	}
	for _, f := range report.Findings {
		fmt.Fprintf(&out, "### %s\n\n", md(f.ID))
		bullet(&out, "Status", nonempty(string(f.Availability), string(f.Severity), stamp(f.At)))
		bullet(&out, "Yang ditemukan", f.Observation)
		bullet(&out, "Artinya", f.Meaning)
		bullet(&out, "Batas bukti", f.Limits)
		bullet(&out, "Sumber", f.Source)
		bullet(&out, "Saran", f.Recommendation)
		var labels []string
		for _, action := range f.Actions {
			labels = append(labels, action.Label)
		}
		bullet(&out, "Pilihan tindakan", strings.Join(labels, ", "))
		out.WriteByte('\n')
	}
	if len(report.Actions) > 0 {
		out.WriteString("## Hasil tindakan\n\n")
		for _, a := range report.Actions {
			fields := []string{string(a.Status), stamp(a.FinishedAt), optional(a.Changed, "perubahan teramati %t"),
				optional(a.PotentialDiscardBytes, "%d byte potensial untuk discard"), optional(a.RemovedBytes, "%d byte teramati dihapus"),
				optional(a.FreeSpaceDelta, "%+d byte perubahan ruang tersedia")}
			if a.CancellationRequested {
				fields = append(fields, "penghentian diminta")
			}
			if a.Status == actions.Unknown || a.Status == actions.Partial {
				fields = append(fields, "bukti hasil belum lengkap")
			}
			bullet(&out, a.Kind, nonempty(fields...))
		}
	}
	return out.String()
}

func bullet(out *strings.Builder, label, value string) {
	if value != "" {
		fmt.Fprintf(out, "- **%s** · %s\n", md(label), md(value))
	}
}

func optional[T any](value *T, format string) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf(format, *value)
}

func nonempty(fields ...string) string {
	var parts []string
	for _, value := range fields {
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " · ")
}

func stamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

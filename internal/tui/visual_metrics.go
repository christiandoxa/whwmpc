package tui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

func hasData(status domain.Availability) bool {
	return status == domain.Available || status == domain.Partial
}

func validPercent(n float64) bool { return n >= 0 && n <= 100 }

func number(n float64, digits int) string {
	s := strconv.FormatFloat(n, 'f', digits, 64)
	if digits > 0 {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if n > 0 && s == "0" {
		return "<" + strconv.FormatFloat(math.Pow10(-digits), 'f', digits, 64)
	}
	return s
}

func metricUsage(m resultMetric, n float64, measured bool) resultMetric {
	if !hasData(m.Availability) {
		return m
	}
	if !measured {
		m.Availability = domain.Unknown
	} else if !validPercent(n) {
		m.Availability = domain.Error
	} else {
		m.Value, m.Unit, m.Usage = number(n, 1), "%", new(n)
	}
	return m
}

func overviewMetrics(o domain.Overview, lang string) []resultMetric {
	l := translator(lang)
	cpu := resultMetric{Category: "cpu", Label: l("CPU terakhir", "CPU latest"), Availability: o.CPU.Availability}
	if o.CPU.Scope == domain.PrimaryProcessorGroup {
		cpu.Label = l("CPU grup primer", "Primary group CPU")
	}
	latest, measured := o.CPU.Latest()
	cpu = metricUsage(cpu, latest, measured)
	if cpu.Usage != nil {
		minLoad, maxLoad, count := latest, latest, 0
		for _, sample := range o.CPU.Samples {
			if validPercent(sample) {
				minLoad, maxLoad = min(minLoad, sample), max(maxLoad, sample)
				count++
			}
		}
		if count != len(o.CPU.Samples) {
			cpu.Availability = domain.Partial
		}
		cpu.Note = fmt.Sprintf(l("%d sampel", "%d samples"), count)
		if !o.CPU.StartedAt.IsZero() && o.CPU.CapturedAt.After(o.CPU.StartedAt) {
			cpu.Note += " / " + o.CPU.CapturedAt.Sub(o.CPU.StartedAt).Round(time.Millisecond).String()
		}
		if count > 1 {
			cpu.Note += " · " + number(minLoad, 1) + "–" + number(maxLoad, 1) + "%"
		}
	}
	mem := o.Memory
	ram := resultMetric{Category: "memory", Label: "RAM", Availability: mem.Use.Availability}
	latest, measured = mem.Use.Latest()
	ram = metricUsage(ram, latest, measured)
	if !hasData(mem.Availability) && ram.Availability == "" {
		ram.Availability = mem.Availability
	} else if hasData(mem.Availability) && mem.TotalBytes > 0 && mem.FreeBytes <= mem.TotalBytes {
		label := l("tersedia", "available")
		if o.OS == "darwin" {
			label = l("bebas, batas bawah", "free, lower bound")
		}
		ram.Note = bytesText(mem.FreeBytes) + " / " + bytesText(mem.TotalBytes) + " " + label
		if mem.Availability == domain.Partial && ram.Availability == domain.Available {
			ram.Availability = domain.Partial
		}
	} else if hasData(mem.Availability) && mem.FreeBytes > mem.TotalBytes {
		ram.Availability = domain.Error
	}
	disk := resultMetric{Category: "disk", Label: l("Disk utama", "Main disk"), Availability: o.DiskStatus}
	idx := slices.IndexFunc(o.Disks, func(d domain.Disk) bool { return d.Root })
	if idx < 0 && len(o.Disks) > 0 {
		idx = 0
		disk.Label = l("Volume", "Volume")
	}
	if idx >= 0 {
		d := o.Disks[idx]
		disk.Availability = d.Availability
		disk = metricUsage(disk, d.UsedPercent, d.TotalBytes > 0 && d.FreeBytes <= d.TotalBytes)
		if hasData(d.Availability) && d.FreeBytes > d.TotalBytes {
			disk.Availability = domain.Error
		}
		if disk.Usage != nil {
			disk.Note = bytesText(d.FreeBytes) + " " + l("tersedia", "available")
		}
	} else if hasData(disk.Availability) {
		disk.Availability = domain.Unknown
	}
	temp := resultMetric{Category: "temperature", Label: l("Suhu CPU", "CPU temperature"), Availability: o.Temperature.Availability, Secondary: true}
	if p := o.Temperature.Celsius; hasData(temp.Availability) {
		switch {
		case p == nil:
			temp.Availability = domain.Unknown
		case math.IsNaN(*p) || math.IsInf(*p, 0):
			temp.Availability = domain.Error
		default:
			temp.Value, temp.Unit, temp.Note = number(*p, 1), "°C", o.Temperature.Source
		}
	}
	metrics := []resultMetric{cpu, ram, disk, temp}
	if hasData(mem.SwapStatus) && mem.SwapUsed > 0 && mem.SwapUsed <= mem.SwapBytes {
		metrics = append(metrics, resultMetric{Label: "Swap", Value: bytesText(mem.SwapUsed), Note: "/ " + bytesText(mem.SwapBytes), Availability: mem.SwapStatus, Secondary: true})
	}
	return metrics
}

func overviewStatuses(smart *storage.SMARTAudit, trim *storage.TrimAudit, p *power.Snapshot, lang string) []resultStatus {
	l := translator(lang)
	var rows []resultStatus
	if smart != nil {
		row := resultStatus{Category: "ssd", Label: "SSD", Availability: smart.Availability}
		passed, failed := 0, 0
		for _, d := range smart.Drives {
			if h := d.Health; h != nil && h.Passed != nil && hasData(d.Availability) {
				if *h.Passed {
					passed++
				} else {
					failed++
				}
			}
		}
		switch {
		case failed > 0:
			row.Value = fmt.Sprintf(l("SMART gagal %d/%d", "SMART failed %d/%d"), failed, len(smart.Drives))
		case passed > 0:
			row.Value = fmt.Sprintf(l("SMART lolos %d/%d", "SMART passed %d/%d"), passed, len(smart.Drives))
		case hasData(row.Availability):
			row.Value = l("Status SMART belum terbaca", "SMART status not read")
		}
		rows = append(rows, row)
	}
	if trim != nil {
		row := resultStatus{Category: "trim", Label: "TRIM", Availability: trim.Availability}
		switch {
		case trim.Timer.Availability == domain.Available && trim.Timer.ActiveState == "active":
			row.Value = l("Jadwal aktif", "Schedule active")
		case trim.Timer.Availability == domain.Available && trim.Timer.ActiveState != "":
			row.Value = l("Jadwal", "Schedule") + " · " + trim.Timer.ActiveState
		case len(trim.Policy) > 0:
			row.Value = l("Kebijakan terbaca", "Policy read")
		}
		rows = append(rows, row)
	}
	if p != nil {
		rows = append(rows, activeProfile(*p, lang))
	}
	return rows
}

func activeProfile(p power.Snapshot, lang string) resultStatus {
	l := translator(lang)
	row := resultStatus{Category: "power", Label: l("Profil daya", "Power profile"), Availability: p.Profiles.Status}
	if hasData(p.Profiles.Status) {
		row.Value = p.Profiles.Active
	}
	if !p.LinuxInterfaces() && row.Availability == "" {
		row.Availability = domain.Unsupported
	}
	if row.Value != "" && hasData(p.PowerStatus) {
		switch p.PowerSource {
		case "ac":
			row.Value += " · AC"
		case "battery":
			row.Value += " · " + l("Baterai", "Battery")
		}
	}
	return row
}

func resultStatusLabel(status domain.Availability, lang string) string {
	if label := statusNames[status].Text(lang); label != "" {
		return label
	}
	return translator(lang)("Belum tersedia", "Unavailable")
}

func emphasis(s resultStatus) bool {
	return s.Severity == diagnostics.Critical || s.Severity == diagnostics.Warning || s.Availability != domain.Available
}

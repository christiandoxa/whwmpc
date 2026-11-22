package diagnostics

import (
	"fmt"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
)

const (
	cpuWarning         = 85
	memoryWarning      = 90
	diskWarning        = 90
	diskCritical       = 97
	warmCPU            = 85
	hotCPU             = 90
	lowBattery         = 20
	dominantProcessCPU = 40
)

func (a Analyzer) Overview(o domain.Overview) []Finding {
	out := []Finding{a.cpu(o), a.memory(o)}
	out = append(out, a.disks(o)...)
	out = append(out, a.cpuTemperature(o), a.process(o), a.overviewPower(o))
	return ranked(out)
}

func (a Analyzer) Slow(o domain.Overview, p power.Snapshot) []Finding {
	findings := a.Overview(o)
	for _, f := range a.Power(p) {
		if f.Severity != Info {
			findings = append(findings, f)
		}
	}
	return ranked(findings)
}

func (a Analyzer) cpu(o domain.Overview) Finding {
	p := o.CPU
	status := a.status(p.Availability, p.CapturedAt, overviewAge)
	f := finding("overview.cpu", "cpu", "/proc/stat", p.CapturedAt, status)
	component := a.text("Penggunaan CPU", "CPU use")
	if !usable(status) {
		return a.unavailable(f, component)
	}
	low, high, valid := sampleRange(p.Samples)
	if !valid {
		f.Availability = domain.Unknown
		return a.unavailable(f, component)
	}
	f.Observation = a.textf("CPU %.0f–%.0f%% pada %d sampel.", "CPU %.0f–%.0f%% over %d samples.", low, high, len(p.Samples))
	f.Meaning = a.text("Persentase ini menggabungkan semua CPU logis.", "These percentages combine all logical CPUs.")
	f.Limits = a.text("Jendela ini singkat. Beban satu inti dapat tersembunyi di angka gabungan.", "This is a short window. An aggregate can hide load on a single core.")
	f.Recommendation = a.text("Ambil sampel saat gejala berlangsung untuk membandingkan beban.", "Repeat the scan while the slowdown is occurring.")
	f.Actions = []Action{a.rescan()}
	if underLoad(p.Samples, cpuWarning) {
		f.Severity = Warning
		f.Meaning = a.text("CPU sibuk sepanjang jendela sampel. Beban ini dapat menunda pekerjaan lain.", "CPU stayed busy throughout the sample window. This can delay other work.")
		f.Recommendation = a.text("Lihat proses pemakai CPU dan bandingkan dengan pekerjaan yang sedang berjalan.", "Review CPU-heavy processes alongside the work you were running.")
	}
	return f
}

func (a Analyzer) memory(o domain.Overview) Finding {
	m := o.Memory
	f := finding("overview.memory", "memory", "/proc/meminfo (MemAvailable, SwapTotal, SwapFree)", o.CapturedAt,
		a.status(m.Availability, o.CapturedAt, overviewAge))
	component := a.text("Memori", "Memory")
	if !usable(f.Availability) {
		return a.unavailable(f, component)
	}
	if m.TotalBytes == 0 || m.FreeBytes > m.TotalBytes {
		f.Availability = domain.Unknown
		return a.unavailable(f, component)
	}
	f.Observation = a.textf("RAM tersedia %.1f / %.1f GiB", "RAM available %.1f / %.1f GiB", gib(m.FreeBytes), gib(m.TotalBytes))
	if m.SwapBytes > 0 && m.SwapUsed <= m.SwapBytes {
		f.Observation += a.textf(" · swap terpakai %.1f / %.1f GiB", " · swap used %.1f / %.1f GiB", gib(m.SwapUsed), gib(m.SwapBytes))
	}
	f.Meaning = a.text("MemAvailable memperkirakan ruang bagi beban baru, termasuk cache yang dapat direklamasi.", "MemAvailable estimates headroom for new work and includes reclaimable cache.")
	f.Limits = a.text("Swap terpakai menunjukkan isi swap. Laju paging dan waktu tunggu perlu pengukuran terpisah.", "Swap occupancy shows what is in swap. Paging rate and stall time require separate measurements.")
	f.Recommendation = a.text("Bandingkan ruang RAM dan swap saat menjalankan pekerjaan yang terasa lambat.", "Compare RAM headroom and swap occupancy during the workload that feels slow.")
	f.Actions = []Action{a.rescan()}
	if usable(a.status(m.Use.Availability, m.Use.CapturedAt, overviewAge)) && underLoad(m.Use.Samples, memoryWarning) {
		f.Severity = Warning
		f.Observation += a.textf(" · RAM terpakai ≥%d%% pada %d sampel", " · RAM use ≥%d%% over %d samples", memoryWarning, len(m.Use.Samples))
		f.Meaning = a.text("Ruang RAM rendah bertahan dalam jendela sampel. Aplikasi baru mungkin menunggu reklamasi memori.", "RAM headroom stayed low during the sample window. New work may wait for memory reclaim.")
		f.Recommendation = a.text("Simpan pekerjaan, lalu kurangi beban aplikasi yang sedang tidak dipakai dan ulangi pemeriksaan.", "Save your work, reduce idle application load, and repeat the scan.")
	}
	return f
}

func (a Analyzer) disks(o domain.Overview) []Finding {
	status := a.status(o.DiskStatus, o.CapturedAt, overviewAge)
	const source = "statfs (Bavail) + /proc/self/mountinfo"
	if !usable(status) || len(o.Disks) == 0 {
		if usable(status) {
			status = domain.Unknown
		}
		return []Finding{a.unavailable(finding("overview.disk", "disk", source, o.CapturedAt, status),
			a.text("Kapasitas filesystem", "Filesystem capacity"))}
	}
	findings := make([]Finding, 0, len(o.Disks))
	for i, disk := range o.Disks {
		f := finding(fmt.Sprintf("overview.disk.%d", i+1), "disk", source, o.CapturedAt, a.status(disk.Availability, o.CapturedAt, overviewAge))
		label := a.textf("Filesystem #%d", "Filesystem #%d", i+1)
		if disk.Root {
			label = a.text("Filesystem root", "Root filesystem")
		}
		if !usable(f.Availability) {
			findings = append(findings, a.unavailable(f, label))
			continue
		}
		if disk.TotalBytes == 0 || disk.FreeBytes > disk.TotalBytes || !validPercent(disk.UsedPercent) {
			f.Availability = domain.Unknown
			findings = append(findings, a.unavailable(f, label))
			continue
		}
		f.Observation = a.textf("%s %.0f%% terisi · %.1f GiB tersedia.", "%s %.0f%% full · %.1f GiB available.", label, disk.UsedPercent, gib(disk.FreeBytes))
		f.Meaning = a.text("Kapasitas tersedia untuk proses tanpa privilege.", "This is capacity available to unprivileged processes.")
		f.Limits = a.text("Sampel ini mencakup kapasitas mount. Latensi I/O perlu pengukuran terpisah.", "This sample covers the mount's capacity. I/O latency requires a separate measurement.")
		f.Recommendation = a.text("Pantau ruang sebelum unduhan, build, atau pembaruan besar.", "Check free space before large downloads, builds, or updates.")
		switch {
		case disk.UsedPercent >= diskCritical:
			f.Severity = Critical
		case disk.UsedPercent >= diskWarning:
			f.Severity = Warning
		}
		if f.Severity != Info {
			f.Meaning = a.text("Ruang sempit dapat membuat unduhan, file sementara, dan pembaruan gagal.", "Low headroom can cause downloads, temporary writes, and updates to fail.")
			f.Recommendation = a.text("Tinjau ukuran data dan cakupan pembersihan yang tersedia.", "Review storage usage and the scope of available cleanup.")
			f.Actions = []Action{a.navigate(assistant.Cleanup, "Tinjau pembersihan", "Review cleanup")}
		}
		findings = append(findings, f)
	}
	return findings
}

func (a Analyzer) cpuTemperature(o domain.Overview) Finding {
	f := finding("overview.temperature", "temperature", "Linux hwmon / thermal zones", o.CapturedAt,
		a.status(o.Temperature.Availability, o.CapturedAt, overviewAge))
	component := a.text("Suhu CPU", "CPU temperature")
	if !usable(f.Availability) {
		return a.unavailable(f, component)
	}
	value, valid := temperature(o)
	if !valid {
		f.Availability = domain.Unknown
		return a.unavailable(f, component)
	}
	f.Observation = a.textf("Sensor CPU tertinggi yang terbaca %.0f °C.", "Highest readable CPU sensor %.0f °C.", value)
	f.Meaning = a.text("Suhu ini berlaku untuk sensor dan waktu yang diamati.", "This temperature describes the available sensors at sample time.")
	f.Limits = a.text("Ambang suhu berbeda menurut perangkat. Thermal throttling perlu bukti dari hardware atau pembacaan pembatas daya.", "Thermal limits vary by device. Thermal throttling requires hardware or power-limit evidence.")
	f.Recommendation = a.text("Bandingkan suhu dan beban pada kondisi kerja yang sama.", "Compare temperature and load under the same workload.")
	if value >= hotCPU {
		f.Severity = Warning
		f.Meaning = a.text("Pembacaan cukup tinggi untuk memeriksa pendinginan sebelum menambah beban.", "This reading is high enough to review cooling before increasing load.")
		f.Recommendation = a.text("Periksa ventilasi dan ulangi sampel sebelum memilih profil lebih agresif.", "Check airflow and repeat the sample before choosing a more aggressive profile.")
		f.Actions = []Action{a.navigate(assistant.Power, "Periksa daya dan batas CPU", "Check power and CPU limits")}
	}
	return f
}

func (a Analyzer) process(o domain.Overview) Finding {
	f := finding("overview.process", "process", "/proc/[pid]/stat delta + /proc/stat", o.CapturedAt,
		a.status(o.ProcessStatus, o.CapturedAt, overviewAge))
	component := a.text("Sampel proses", "Process sample")
	if !usable(f.Availability) {
		return a.unavailable(f, component)
	}
	var top int
	var percent float64
	var found bool
	for i, process := range o.Processes {
		if validPercent(process.CPU) && (!found || process.CPU > percent) {
			top, percent, found = i, process.CPU, true
		}
	}
	if !found {
		f.Availability = domain.Unknown
		return a.unavailable(f, component)
	}
	f.Observation = a.textf("Proses #%d memakai %.0f%% CPU host · RSS %.2f GiB.", "Process #%d used %.0f%% of host CPU · RSS %.2f GiB.",
		top+1, percent, gib(o.Processes[top].RSS))
	f.Meaning = a.text("Ini proses dengan bagian CPU terbesar di antara proses yang teramati.", "This is the largest CPU share among sampled processes.")
	f.Limits = a.text("Sampel mencakup proses yang dapat dibaca dan teramati selama interval.", "The sample covers processes readable and observable during the interval.")
	f.Recommendation = a.text("Tinjau detail proses saat gejala terjadi.", "Check process details while the slowdown occurs.")
	if percent >= dominantProcessCPU && usable(a.status(o.CPU.Availability, o.CPU.CapturedAt, overviewAge)) &&
		underLoad(o.CPU.Samples, cpuWarning) {
		f.Severity = Warning
		f.Meaning = a.text("Proses ini memakai bagian besar CPU saat CPU host sibuk sepanjang sampel.", "This process used a substantial CPU share while host CPU was busy throughout the sample.")
	}
	return f
}

func (a Analyzer) overviewPower(o domain.Overview) Finding {
	f := finding("overview.power", "power", "Linux power_supply class", o.CapturedAt,
		a.status(o.PowerStatus, o.CapturedAt, overviewAge))
	component := a.text("Sumber daya", "Power source")
	if !usable(f.Availability) {
		return a.unavailable(f, component)
	}
	f.Observation = a.text("Status daya AC belum diketahui.", "AC status is unknown.")
	if o.OnAC != nil {
		if *o.OnAC {
			f.Observation = a.text("Sumber daya eksternal terdeteksi.", "External power detected.")
		} else {
			f.Observation = a.text("Sumber daya eksternal tidak terdeteksi.", "External power was not detected.")
		}
	}
	if o.Battery != nil && validPercent(*o.Battery) {
		f.Observation += a.textf(" Baterai %.0f%%.", " Battery %.0f%%.", *o.Battery)
	} else if o.OnAC == nil {
		f.Availability = domain.Unknown
		return a.unavailable(f, component)
	}
	f.Meaning = a.text("Sumber daya dapat memengaruhi profil yang cocok untuk pekerjaan saat ini.", "Power source can affect which profile suits the current workload.")
	f.Limits = a.text("Durasi baterai dan kapasitas adaptor perlu pengukuran terpisah.", "Battery runtime and adapter capacity require separate measurements.")
	f.Recommendation = a.text("Lihat profil yang tersedia sebelum mengubah pengaturan daya.", "Review available profiles before changing power settings.")
	f.Actions = []Action{a.navigate(assistant.Power, "Tinjau profil daya", "Review power profiles")}
	return f
}

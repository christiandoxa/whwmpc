package diagnostics

import (
	"fmt"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
)

const (
	cpufreqSource  = "Linux CPUFreq policy sysfs"
	profilesSource = "D-Bus PowerProfiles API + systemd and /proc manager checks"
)

func (a Analyzer) Power(p power.Snapshot) []Finding {
	if !p.LinuxInterfaces() {
		return []Finding{a.powerSupply(p)}
	}
	out := a.cpuPolicies(p)
	out = append(out, a.powerSupply(p), a.powerProfiles(p))
	return ranked(out)
}

func (a Analyzer) cpuPolicies(p power.Snapshot) []Finding {
	status := a.status(p.CPUFreqStatus, p.At, powerAge)
	if !usable(status) || len(p.Policies) == 0 {
		if usable(status) {
			status = domain.Unknown
		}
		return []Finding{a.unavailable(finding("power.cpufreq", "cpu", cpufreqSource, p.At, status),
			a.text("Policy CPUFreq", "CPUFreq policies"))}
	}
	findings := make([]Finding, 0, len(p.Policies))
	for i, policy := range p.Policies {
		f := finding(fmt.Sprintf("power.cpufreq.%d", i+1), "cpu", cpufreqSource, p.At, status)
		fields := []string{a.textf("Policy CPU #%d", "CPU policy #%d", i+1)}
		for _, entry := range []struct {
			name  string
			value *uint64
		}{
			{a.text("teramati", "observed"), policy.ObservedKHz},
			{a.text("rata-rata hardware", "hardware average"), policy.AverageKHz},
			{a.text("nilai driver", "driver reading"), policy.DriverKHz},
			{a.text("konfigurasi min", "configured min"), policy.ConfigMinKHz},
			{a.text("konfigurasi maks", "configured max"), policy.ConfigMaxKHz},
			{a.text("hardware min", "hardware min"), policy.HardwareMinKHz},
			{a.text("hardware maks", "hardware max"), policy.HardwareMaxKHz},
			{a.text("batas firmware", "firmware limit"), policy.FirmwareCapKHz},
		} {
			if entry.value != nil && *entry.value > 0 {
				fields = append(fields, fmt.Sprintf("%s %.2f GHz", entry.name, float64(*entry.value)/1e6))
			}
		}
		for _, entry := range [][2]string{{"driver", policy.Driver}, {"governor", policy.Governor}, {"EPP", policy.EPP}} {
			if value := safeToken(entry[1]); value != "" {
				fields = append(fields, entry[0]+" "+value)
			}
		}
		boost := policy.Boost
		if boost == nil {
			boost = p.Boost
		}
		if boost != nil {
			if *boost {
				fields = append(fields, a.text("boost diizinkan", "boost allowed"))
			} else {
				fields = append(fields, a.text("boost dibatasi", "boost disabled"))
			}
		}
		if len(fields) == 1 {
			f.Availability = domain.Unknown
			findings = append(findings, a.unavailable(f, fields[0]))
			continue
		}
		f.Observation = strings.Join(fields, " · ")
		f.Meaning = a.text("Frekuensi dapat turun saat idle. Driver dan governor menentukan respons terhadap beban.", "Frequency can fall at idle. The driver and governor shape how it responds to load.")
		f.Limits = a.text("scaling_cur_freq dapat mewakili permintaan driver. Pembacaan hardware berasal dari cpuinfo bila tersedia.", "scaling_cur_freq can represent the driver's request. Hardware readouts come from cpuinfo when available.")
		f.Recommendation = a.text("Bandingkan dengan beban dan suhu yang sama sebelum menilai kinerja.", "Compare readings under the same workload and temperature when assessing performance.")
		if substantiallyCapped(policy.ConfigMaxKHz, policy.HardwareMaxKHz) || substantiallyCapped(policy.FirmwareCapKHz, policy.HardwareMaxKHz) {
			f.Severity = Warning
			f.Meaning = a.text("Batas konfigurasi atau firmware berada di bawah kemampuan maksimum yang dilaporkan.", "A configured or firmware limit is below the reported hardware maximum.")
			f.Recommendation = a.text("Tinjau sumber daya dan profil aktif. Ukur pekerjaan yang sama untuk menilai dampaknya.", "Review the power source and active profile. Measure the same workload to assess the impact.")
			f.Actions = []Action{a.navigate(assistant.Recommend, "Cari profil sesuai kebutuhan", "Find a profile for your workload")}
		}
		findings = append(findings, f)
	}
	return findings
}

func substantiallyCapped(limit, maximum *uint64) bool {
	return limit != nil && maximum != nil && *limit > 0 && *maximum > 0 && float64(*limit)/float64(*maximum) <= 0.9
}

func (a Analyzer) powerSupply(p power.Snapshot) Finding {
	f := finding("power.supply", "power", nativeSource(p.OS, "Linux power_supply + platform_profile + energy_perf_bias",
		"macOS pmset -g batt", "Win32 GetSystemPowerStatus"), p.At,
		a.status(p.PowerStatus, p.At, powerAge))
	fields := []string{a.text("Sumber daya belum diketahui", "Power source unknown")}
	if usable(f.Availability) {
		switch p.PowerSource {
		case "ac":
			fields[0] = a.text("Sumber daya eksternal", "External power")
		case "battery":
			fields[0] = a.text("Daya baterai", "Battery power")
		}
		for i, battery := range p.Batteries {
			label := a.textf("baterai #%d", "battery #%d", i+1)
			var details []string
			if battery.Percent != nil && *battery.Percent <= 100 {
				details = append(details, fmt.Sprintf("%d%%", *battery.Percent))
			}
			if status := strings.ToLower(battery.Status); slices.Contains([]string{"charging", "discharging", "full", "not charging"}, status) {
				details = append(details, status)
			}
			if len(details) > 0 {
				fields = append(fields, label+" "+strings.Join(details, " · "))
			}
		}
		if p.PowerSource != "ac" && p.PowerSource != "battery" {
			f.Availability = domain.Partial
		}
	}
	if usable(a.status(domain.Available, p.At, powerAge)) {
		if profile := safeToken(p.PlatformProfile); profile != "" {
			fields = append(fields, a.textf("profil platform %s", "platform profile %s", profile))
		}
		var choices []string
		for _, value := range p.PlatformChoices {
			if choice := safeToken(value); choice != "" {
				choices = append(choices, choice)
			}
		}
		if len(choices) > 0 {
			fields = append(fields, a.textf("pilihan platform %s", "platform choices %s", strings.Join(choices, ", ")))
		}
		low, high, count := uint64(15), uint64(0), 0
		for _, bias := range p.EnergyBias {
			if bias.Value <= 15 {
				low, high, count = min(low, bias.Value), max(high, bias.Value), count+1
			}
		}
		if count > 0 {
			fields = append(fields, a.textf("EPB %d–%d pada %d CPU", "EPB %d–%d across %d CPUs", low, high, count))
		}
	}
	if !usable(f.Availability) {
		if len(fields) == 1 {
			return a.unavailable(f, fields[0])
		}
		f.Availability = domain.Partial
	}
	f.Observation = strings.Join(fields, " · ")
	if !p.LinuxInterfaces() {
		f.Meaning = a.text("Sumber daya dan status baterai dibaca dari sistem operasi.", "Power source and battery status come from the operating system.")
		f.Limits = a.text("Cakupan baterai mengikuti data yang dilaporkan perangkat.", "Battery coverage depends on the readings exposed by the device.")
		f.Recommendation = a.text("Ambil sampel baru setelah sumber daya berubah.", "Take another reading after the power source changes.")
		return f
	}
	f.Meaning = a.text("Sumber daya, platform profile, dan EPB berasal dari antarmuka yang terpisah.", "Power source, platform profile, and EPB come from separate interfaces.")
	f.Limits = a.text("Driver dan firmware menentukan efek setiap pilihan pada perangkat.", "Driver and firmware determine how each setting affects the hardware.")
	f.Recommendation = a.text("Pilih profil berdasarkan beban kerja, suhu, dan prioritas daya.", "Choose a profile using workload, temperature, and power priorities.")
	return f
}

func (a Analyzer) powerProfiles(p power.Snapshot) Finding {
	profiles := p.Profiles
	f := finding("power.profiles", "power", profilesSource, p.At, a.status(profiles.Status, p.At, powerAge))
	managerStatus := a.status(profiles.ManagerStatus, p.At, powerAge)
	known := knownManagers(profiles.Managers)
	component := a.text("Profil daya", "Power profiles")
	if !usable(f.Availability) {
		f = a.unavailable(f, component)
		if usable(managerStatus) && len(known) > 0 {
			f.Observation += " · " + strings.Join(known, ", ")
			f.Recommendation = a.managerAdvice(known)
		}
	} else {
		available := availableProfiles(profiles.Available)
		if !power.ValidProfile(profiles.Active) || len(available) == 0 || !slices.Contains(available, profiles.Active) {
			f.Availability = domain.Unknown
			f = a.unavailable(f, component)
		} else {
			f.Observation = a.textf("Profil aktif %s · tersedia %s.", "Active profile %s · available %s.", profiles.Active, strings.Join(available, ", "))
			if len(known) > 0 {
				f.Observation += " " + strings.Join(known, ", ") + "."
			}
			f.Meaning = a.text("Profil menunjukkan pilihan yang dilaporkan oleh pengelola daya.", "Profiles describe choices reported by the power manager.")
			f.Limits = a.text("Efek daya, panas, kebisingan, dan kinerja bergantung pada perangkat serta beban.", "Power, heat, noise, and speed effects depend on hardware and workload.")
			f.Recommendation = a.managerAdvice(known)
			if profiles.CanChange() {
				f.Recommendation = a.text("Tinjau dampak sebelum mengajukan perubahan profil.", "Review the impact before proposing a profile change.")
				for _, name := range available {
					if name != profiles.Active && (name != "performance" || profiles.Inhibited == "") {
						f.Actions = append(f.Actions, a.profileAction(name))
					}
				}
			}
			if profiles.Holds != nil && *profiles.Holds > 0 {
				f.Observation += a.textf(" %d hold aktif.", " %d active holds.", *profiles.Holds)
				f.Recommendation = a.text("Periksa lagi setelah aplikasi melepaskan hold profil.", "Scan again after the application releases its profile hold.")
			}
			if profiles.Degraded != "" || profiles.Inhibited != "" {
				f.Severity = Warning
				f.Meaning = a.text("Backend melaporkan pembatasan profil performance.", "The backend reports a restriction on the performance profile.")
				if profiles.Degraded != "" {
					f.Observation += a.text(" Performance terdegradasi. ", " Performance degraded. ") + a.profileRestriction(profiles.Degraded)
				}
				if profiles.Inhibited != "" {
					f.Observation += a.text(" Performance dibatasi. ", " Performance inhibited. ") + a.profileRestriction(profiles.Inhibited)
				}
			}
			for _, driver := range profiles.Drivers[profiles.Active] {
				if driver == "fake" || driver == "placeholder" {
					f.Limits = a.text("Backend melaporkan driver placeholder. Dampak pada hardware perlu diperiksa terpisah.", "The backend reports a placeholder driver. Hardware effects require separate checks.")
					break
				}
			}
		}
	}
	if usable(managerStatus) && len(profiles.Conflicts) > 0 {
		f.ID, f.Severity, f.Actions = "power.manager-conflict", Warning, nil
		f.Observation += a.text(" Pengelola daya terdeteksi konflik.", " Competing power managers detected.")
		f.Meaning = a.text("Lebih dari satu pengelola dapat menerapkan pengaturan daya pada perangkat yang sama.", "More than one manager may apply power settings to the same hardware.")
		f.Limits = a.text("Deteksi memakai pengelola yang terlihat melalui systemd dan daftar proses.", "Detection covers managers visible through systemd and the sampled process list.")
		f.Recommendation = a.text("Tinjau layanan aktif dan gunakan satu pengelola untuk profil daya sebelum mengubah profil.", "Review active services and settle on one power manager before changing profiles.")
	}
	return f
}

func (a Analyzer) profileRestriction(reason string) string {
	switch reason {
	case "lap-detected":
		return a.text("Backend mendeteksi penggunaan di pangkuan.", "The backend detected lap use.")
	case "high-operating-temperature":
		return a.text("Backend melaporkan suhu operasi tinggi.", "The backend reports a high operating temperature.")
	default:
		return a.text("Backend melaporkan batas tambahan untuk performance.", "The backend reports a performance restriction.")
	}
}

func knownManagers(observed []string) []string {
	var managers []string
	for _, name := range []string{"power-profiles-daemon", "TLP", "TuneD", "auto-cpufreq"} {
		if slices.Contains(observed, name) {
			managers = append(managers, name)
		}
	}
	return managers
}

func availableProfiles(observed []string) []string {
	var profiles []string
	for _, name := range []string{"power-saver", "balanced", "performance"} {
		if slices.Contains(observed, name) {
			profiles = append(profiles, name)
		}
	}
	return profiles
}

func (a Analyzer) managerAdvice(known []string) string {
	switch {
	case slices.Contains(known, "TLP"):
		return a.text("Periksa status TLP dengan tlp-stat -s, lalu gunakan kebijakan profil TLP.", "Check TLP with tlp-stat -s and use its profile policy.")
	case slices.Contains(known, "TuneD"):
		return a.text("Periksa profil TuneD melalui tuned-adm active dan tuned-adm list.", "Check TuneD profiles with tuned-adm active and tuned-adm list.")
	case slices.Contains(known, "auto-cpufreq"):
		return a.text("Periksa pengaturan layanan auto-cpufreq untuk melihat kebijakan baterai dan adaptor.", "Review auto-cpufreq's service settings for its battery and charger policies.")
	default:
		return a.text("Periksa pengaturan Daya di desktop dan jalankan whwmpc doctor untuk melihat backend yang terdeteksi.", "Review desktop Power settings and run whwmpc doctor to check the detected backend.")
	}
}

func (a Analyzer) profileAction(name string) Action {
	return Action{Kind: "power-profile", Target: name, Label: a.textf("Tinjau %s", "Preview %s", name)}
}

func (a Analyzer) Recommend(o domain.Overview, p power.Snapshot, workload assistant.Workload) []Finding {
	const source = "Workload preference + overview temperature + power_supply + PowerProfiles API"
	f := finding("recommend.profile", "recommendation", source, p.At, a.status(p.Profiles.Status, p.At, powerAge))
	if !slices.Contains(assistant.Workloads, workload) {
		f.ID, f.Availability, f.At = "recommend.context", domain.Available, a.Now
		f.Observation = a.text("Apa prioritas penggunaanmu saat ini?", "What's your current workload or priority?")
		f.Meaning = a.text("Preferensi membantu memilih kompromi daya dan kinerja.", "Your preference helps choose a balance of power and performance.")
		f.Limits = a.text("Pilihan profil mengikuti backend dan kondisi perangkat.", "Profile choices depend on the backend and hardware conditions.")
		f.Recommendation = a.text("Pilih konteks yang paling sesuai.", "Pick the closest match.")
		for _, choice := range assistant.Workloads {
			f.Actions = append(f.Actions, Action{Kind: "workload", Target: string(choice), Label: a.workloadName(choice)})
		}
		return []Finding{f}
	}
	component := a.textf("Profil untuk %s", "Profile for %s", a.workloadName(workload))
	if len(p.Profiles.Conflicts) > 0 && usable(a.status(p.Profiles.ManagerStatus, p.At, powerAge)) {
		f.ID, f.Severity = "recommend.conflict", Warning
		f.Observation = component + " · " + a.text("konflik pengelola daya", "power-manager conflict")
		f.Meaning = a.text("Dua pengelola atau lebih dapat mengubah kebijakan daya yang sama.", "Two or more managers may change the same power policy.")
		f.Limits = a.text("Rekomendasi profil memerlukan pengelola daya yang dapat ditentukan.", "A profile recommendation requires an identifiable power manager.")
		f.Recommendation = a.text("Tinjau pengelola aktif dan pilih satu backend sebelum mengubah profil.", "Review active managers and settle on one backend before changing profiles.")
		f.Actions = []Action{a.navigate(assistant.Power, "Lihat pengelola daya", "Review power managers")}
		return []Finding{f}
	}
	if !usable(f.Availability) {
		f = a.unavailable(f, component)
		if f.Availability != domain.Stale {
			f.Recommendation = a.managerAdvice(knownManagers(p.Profiles.Managers))
		}
		return []Finding{f}
	}
	profile, why := a.chooseProfile(o, p, workload)
	if profile == "" {
		f.Availability = domain.Partial
		f.Observation = component
		f.Meaning = a.text("Daftar backend belum memuat profil yang cocok dengan batas daya dan suhu yang diketahui.", "The backend list lacks a profile suited to the known power and temperature conditions.")
		f.Limits = a.text("Pilihan mengikuti kemampuan backend yang terbaca.", "Choices depend on the reported backend capabilities.")
		f.Recommendation = a.managerAdvice(knownManagers(p.Profiles.Managers))
		return []Finding{f}
	}
	f.Observation = component + " · " + profile
	f.Meaning = why
	f.Limits = a.text("Ukur pekerjaan yang sama dalam kondisi sebanding untuk menilai perubahan kecepatan.", "Measure the same workload under comparable conditions to assess any speed change.")
	switch profile {
	case "power-saver":
		f.Recommendation = a.text("Gunakan power-saver untuk mengutamakan konsumsi daya, suhu, dan kebisingan. Pekerjaan berat bisa selesai lebih lama.", "Use power-saver to favor energy use, temperature, and noise. Heavy work may take longer.")
	case "performance":
		f.Recommendation = a.text("Pertimbangkan performance selama pekerjaan berat. Pemakaian daya, panas, dan kebisingan bisa meningkat.", "Consider performance for the heavy workload. Power use, heat, and noise may rise.")
	default:
		f.Recommendation = a.text("Gunakan balanced sebagai kompromi antara penggunaan daya, panas, kebisingan, dan respons.", "Use balanced as a compromise among power use, heat, noise, and responsiveness.")
	}
	if p.Profiles.CanChange() {
		if p.Profiles.Active != profile {
			f.Actions = []Action{a.profileAction(profile)}
		}
	} else {
		f.Recommendation += " " + a.managerAdvice(knownManagers(p.Profiles.Managers))
	}
	return []Finding{f}
}

func (a Analyzer) chooseProfile(o domain.Overview, p power.Snapshot, w assistant.Workload) (string, string) {
	source := ""
	if usable(a.status(p.PowerStatus, p.At, powerAge)) {
		source = p.PowerSource
	}
	freshOverview := usable(a.status(o.Availability, o.CapturedAt, overviewAge))
	if (source != "ac" && source != "battery") && freshOverview && usable(o.PowerStatus) && o.OnAC != nil {
		if *o.OnAC {
			source = "ac"
		} else {
			source = "battery"
		}
	}
	temp, tempKnown := temperature(o)
	tempKnown = tempKnown && freshOverview
	batteryNeedsCharge := false
	if source == "battery" {
		batteryNeedsCharge = slices.ContainsFunc(p.Batteries, func(b power.Battery) bool {
			return usable(a.status(p.PowerStatus, p.At, powerAge)) && b.Percent != nil && *b.Percent <= lowBattery
		}) || (freshOverview && usable(o.PowerStatus) && o.Battery != nil && validPercent(*o.Battery) && *o.Battery <= lowBattery)
	}
	prefer := []string{"balanced", "power-saver"}
	reason := a.text("Balanced cocok untuk pekerjaan umum dan kondisi daya yang berubah.", "Balanced suits general work and changing power conditions.")
	switch {
	case batteryNeedsCharge:
		prefer = []string{"power-saver", "balanced"}
		reason = a.text("Kapasitas baterai yang terbaca rendah.", "A readable battery reports low charge.")
	case tempKnown && temp >= warmCPU:
		prefer = []string{"power-saver", "balanced"}
		reason = a.text("Suhu yang teramati mendukung pilihan dengan beban termal lebih rendah.", "The observed temperature favors a profile with lower thermal load.")
	case w == assistant.Battery || w == assistant.Quiet:
		prefer = []string{"power-saver", "balanced"}
		reason = a.text("Prioritasmu adalah daya tahan baterai atau suhu dan kebisingan yang lebih rendah.", "Your priority is battery runtime or lower temperature and noise.")
	case w == assistant.Build || w == assistant.Gaming:
		if source == "ac" && tempKnown && p.Profiles.ManagerStatus == domain.Available &&
			p.Profiles.Holds != nil && *p.Profiles.Holds == 0 && p.Profiles.Degraded == "" && p.Profiles.Inhibited == "" &&
			!slices.Contains(p.Profiles.Drivers["performance"], "placeholder") && !slices.Contains(p.Profiles.Drivers["performance"], "fake") {
			prefer = []string{"performance", "balanced", "power-saver"}
			reason = a.text("Beban berat dipilih, daya eksternal terdeteksi, dan sampel suhu mendukung pilihan performance.", "You chose a heavy workload, external power is present, and the temperature sample supports considering performance.")
		} else {
			reason = a.text("Beban berat dipilih. Bukti untuk performance memerlukan daya eksternal, suhu yang terbaca, dan backend yang siap.", "You chose a heavy workload. Considering performance requires external power, a temperature reading, and a ready backend.")
		}
	}
	for i, profile := range prefer {
		if slices.Contains(p.Profiles.Available, profile) {
			if i > 0 {
				reason += " " + a.textf("Backend menawarkan %s sebagai pilihan yang tersedia.", "The backend offers %s as an available alternative.", profile)
			}
			return profile, reason
		}
	}
	return "", ""
}

func (a Analyzer) workloadName(workload assistant.Workload) string {
	switch workload {
	case assistant.Build:
		return a.text("Build berat", "Heavy builds")
	case assistant.Gaming:
		return a.text("Gaming", "Gaming")
	case assistant.Battery:
		return a.text("Daya tahan baterai", "Battery runtime")
	case assistant.Quiet:
		return a.text("Suhu dan kebisingan", "Temperature and noise")
	default:
		return a.text("Kerja harian", "Daily work")
	}
}

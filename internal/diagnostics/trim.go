package diagnostics

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

const mountSource = "/proc/self/mountinfo + Linux block queue discard_max_bytes + fstrim.service journal"

func (a Analyzer) TRIM(audit storage.TrimAudit) []Finding {
	if audit.Source != "" {
		return a.nativeTRIM(audit)
	}
	status := a.status(audit.Availability, audit.CapturedAt, trimAge)
	if status == domain.Stale {
		return []Finding{a.unavailable(finding("trim.coverage", "trim", mountSource, audit.CapturedAt, status),
			a.text("Audit TRIM", "TRIM audit"))}
	}
	out := []Finding{a.trimTimer(audit)}
	for i, m := range audit.Mounts {
		out = append(out, a.trimMount(audit, m, i+1))
	}
	if len(audit.Mounts) == 0 {
		if usable(status) {
			status = domain.Unknown
		}
		out = append(out, a.unavailable(finding("trim.coverage", "trim", mountSource, audit.CapturedAt, status),
			a.text("Pemetaan mount untuk TRIM", "Mount mapping for TRIM")))
	}
	return ranked(out)
}

func (a Analyzer) trimTimer(audit storage.TrimAudit) Finding {
	timer := audit.Timer
	const source = "systemctl show fstrim.timer/fstrim.service + system journal"
	status := a.status(timer.Availability, audit.CapturedAt, trimAge)
	f := finding("trim.scheduler", "trim", source, audit.CapturedAt, status)
	component := a.text("Jadwal TRIM", "TRIM schedule")
	if !usable(status) {
		f = a.unavailable(f, component)
		if timer.Scheduler == "cron" {
			f.Source = "cron capability check"
			f.Observation = a.text("Cron terdeteksi · jadwal fstrim belum terverifikasi.", "Cron detected · fstrim scheduling is unverified.")
			f.Recommendation = a.text("Periksa jadwal cron dan bukti eksekusi untuk filesystem yang dipilih.", "Check cron scheduling and execution evidence for the selected filesystem.")
		} else if timer.Scheduler != "systemd" && status == domain.Unsupported {
			f.Source = "systemd/cron capability checks"
			f.Observation = a.text("Manager systemd tidak terdeteksi · jadwal alternatif belum diketahui.", "No systemd manager was detected · alternative scheduling is unknown.")
			f.Recommendation = a.text("Periksa scheduler host atau tinjau TRIM manual pada filesystem yang didukung.", "Review the host scheduler or preview manual TRIM on a supported filesystem.")
		}
		return f
	}
	f.Observation = a.textf("fstrim.timer · boot %s · sekarang %s.", "fstrim.timer · boot %s · current %s.",
		a.unitState(timer.UnitFileState), a.unitState(timer.ActiveState))
	if valid := scheduleTime(timer.NextElapse); valid != "" {
		f.Observation += a.textf(" Berikutnya %s.", " Next %s.", valid)
	} else if mono := scheduleTime(timer.NextMonotonic); mono != "" {
		f.Observation += a.textf(" Jadwal monotonic %s.", " Monotonic schedule %s.", mono)
	}
	if valid := scheduleTime(timer.LastTrigger); valid != "" {
		f.Observation += a.textf(" Pemicu timer terakhir %s.", " Last timer trigger %s.", valid)
	}
	f.Meaning = a.text("Status saat ini dan pengaktifan saat boot adalah dua properti systemd yang terpisah.", "Current activity and boot enablement are separate systemd properties.")
	f.Limits = a.text("Periksa bukti per filesystem. Service dapat melewati mount tertentu sesuai konfigurasi.", "Use evidence for each filesystem. The service can skip mounts according to its configuration.")
	f.Recommendation = a.text("Bandingkan jadwal, hasil service, dan bukti per filesystem.", "Compare the schedule, service result, and per-filesystem evidence.")
	result, finishedAt := safeToken(timer.ServiceResult), scheduleTime(timer.LastServiceExit)
	if result != "" && finishedAt != "" {
		f.Observation += a.textf(" Service %s pada %s.", " Service result %s at %s.", result, finishedAt)
		if timer.ServiceExitCode != nil {
			f.Observation += fmt.Sprintf(" status=%d.", *timer.ServiceExitCode)
		}
		if result != "success" || timer.ServiceExitCode != nil && *timer.ServiceExitCode != 0 {
			f.Severity = Warning
			f.Recommendation = a.text("Tinjau kegagalan service dan log tiap mount sebelum menjalankan ulang.", "Review service failures and per-mount logs before retrying.")
		}
	} else {
		f.Observation += a.text(" Hasil service terakhir belum terlihat.", " The last service result is unavailable.")
	}
	if timer.Journal == domain.PermissionRequired {
		f.Limits += " " + a.text("Izin baca jurnal diperlukan.", "Journal read permission is required.")
	}
	if timer.ActiveState == "failed" {
		f.Severity = Warning
	}
	needsSchedule := slices.ContainsFunc(audit.Mounts, func(m storage.Mount) bool {
		return m.Discard == domain.Available && (m.DiscardMode == "" || m.DiscardMode == "nodiscard")
	})
	if needsSchedule && timer.ActiveState == "inactive" && f.Severity != Warning {
		f.Severity = Warning
		f.Recommendation = a.text("Tinjau jadwal TRIM host. Jadwal berkala biasanya sesuai untuk desktop.", "Review the host TRIM schedule. A periodic schedule usually suits desktops.")
		if status == domain.Available && timer.UnitFileState == "disabled" && timer.LoadState == "loaded" &&
			timer.Unit == "fstrim.service" && timer.ServiceLoadState == "loaded" && !timer.NeedDaemonReload &&
			timer.DropInPaths == "" && timer.ServiceDropInPaths == "" && audit.Manual == domain.Available {
			f.Actions = []Action{{Kind: "enable-trim-timer", Target: "fstrim.timer", Label: a.text("Tinjau jadwal TRIM", "Review TRIM schedule")}}
		}
	}
	return f
}

func (a Analyzer) trimMount(audit storage.TrimAudit, m storage.Mount, ordinal int) Finding {
	f := finding(fmt.Sprintf("trim.mount.%d", ordinal), "trim", mountSource, audit.CapturedAt,
		a.status(m.Discard, audit.CapturedAt, trimAge))
	label := fmt.Sprintf("Filesystem #%d", ordinal)
	if fs := safeToken(m.FSType); fs != "" {
		label += " " + fs
	}
	f.Observation = a.textf("%s · %d lapisan block.", "%s · %d block layers.", label, len(m.Layers))
	switch m.DiscardMode {
	case "discard", "discard=async", "discard=sync", "nodiscard":
		f.Observation += a.textf(" Opsi mount %s.", " Mount option %s.", m.DiscardMode)
	}
	f.Limits = a.text("Catatan jurnal hanya mengaitkan nama mount dan perangkat yang cocok pada eksekusi sebelumnya.", "Journal evidence matches the mount and device names from an earlier service run.")
	f.Recommendation = a.text("Periksa hasil TRIM berkala untuk filesystem ini.", "Check periodic TRIM evidence for this filesystem.")
	switch f.Availability {
	case domain.Available:
		f.Meaning = a.text("Lapisan block yang terlihat melaporkan kemampuan discard.", "The visible block layers report discard capability.")
		if audit.Manual == domain.Available && !m.ReadOnly && m.ID != 0 {
			f.Actions = []Action{{Kind: "trim-mount", Target: strconv.FormatUint(m.ID, 10), Label: a.textf("Tinjau TRIM filesystem #%d", "Preview TRIM for filesystem #%d", ordinal)}}
		}
	case domain.Unsupported:
		switch {
		case m.ReadOnly:
			f.Meaning = a.text("Filesystem ini terpasang read-only.", "This filesystem is mounted read-only.")
		case slices.ContainsFunc(m.Layers, func(layer storage.BlockLayer) bool {
			return layer.MaxDiscardBytes != nil && *layer.MaxDiscardBytes == 0
		}):
			f.Meaning = a.text("Lapisan block melaporkan batas discard nol.", "A block layer reports a zero discard limit.")
		default:
			f.Meaning = a.text("Tipe filesystem atau salah satu lapisan block berada di luar cakupan pemeriksaan ini.", "The filesystem type or a block layer falls outside this check's coverage.")
		}
		f.Recommendation = a.text("Tinjau dukungan filesystem dan jalur storage dari sistem host.", "Review filesystem and block-path support on the host.")
	case domain.Partial:
		f.Meaning = a.text("Mount hanya mencakup subtree atau subvolume. FITRIM dapat mencakup filesystem penuh.", "This mount covers a subtree or subvolume. FITRIM can cover the full filesystem.")
		f.Recommendation = a.text("Periksa mount filesystem penuh sebelum mempertimbangkan TRIM manual.", "Check the full filesystem mount before considering manual TRIM.")
	default:
		f.Meaning = a.text("Identitas mount atau kemampuan discard sepanjang jalur block belum dapat dipastikan.", "Mount identity or discard capability along the block path is unverified.")
		f.Recommendation = a.text("Tinjau topologi dan akses sistem sebelum mempertimbangkan TRIM manual.", "Review the topology and host access before considering manual TRIM.")
	}
	if m.LastTrim != nil && !m.LastTrim.RecordedAt.IsZero() && usable(audit.Timer.Journal) {
		f.Observation += a.textf(" Jurnal %s · %d byte berpotensi diajukan untuk discard.", " Journal %s · %d potential discard bytes.",
			m.LastTrim.RecordedAt.UTC().Format("2006-01-02 15:04 MST"), m.LastTrim.PotentialDiscardBytes)
		f.Limits += " " + a.text("Nilai itu menunjukkan cakupan potensial pengajuan discard. Kapasitas ruang tersedia diukur terpisah oleh statfs.", "This value describes potential discard submissions. Available space is measured separately by statfs.")
	} else {
		f.Observation += a.text(" Bukti eksekusi per mount belum tersedia.", " Per-mount execution evidence is unavailable.")
	}
	return f
}

var timeProperty = regexp.MustCompile(`^(?:[A-Z][a-z]{2} )?[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?: [A-Za-z0-9+:-]{1,10})?$|^[0-9]{1,20}$`)

func scheduleTime(value string) string {
	if timeProperty.MatchString(value) {
		return value
	}
	return ""
}

func (a Analyzer) unitState(value string) string {
	if slices.Contains(strings.Fields("enabled enabled-runtime disabled static masked masked-runtime generated indirect linked linked-runtime active reloading activating inactive deactivating failed maintenance"), value) {
		return value
	}
	return a.text("belum diketahui", "unknown")
}

func (a Analyzer) nativeTRIM(audit storage.TrimAudit) []Finding {
	f := finding("trim.coverage", "trim", audit.Source, audit.CapturedAt, a.status(audit.Availability, audit.CapturedAt, trimAge))
	label := a.text("Dukungan TRIM yang dilaporkan", "Reported TRIM support")
	if audit.Source == storage.WindowsTRIMSource {
		label = a.text("Kebijakan notifikasi penghapusan", "Delete-notification policy")
	}
	if !usable(f.Availability) {
		return []Finding{a.unavailable(f, label)}
	}
	f.Observation = label
	for _, policy := range audit.Policy {
		status := a.text("tidak aktif", "disabled")
		if policy.Enabled {
			status = a.text("aktif", "enabled")
		}
		if audit.Source == storage.DarwinTRIMSource {
			status = a.text("tidak didukung", "not supported")
			if policy.Enabled {
				status = a.text("didukung", "supported")
			}
		}
		f.Observation += " · " + safeToken(policy.Scope) + " " + status
	}
	f.Meaning = a.text("Ini kemampuan perangkat atau kebijakan OS yang dapat dibaca.", "This is reported device capability or observable OS policy.")
	f.Limits = a.text("Belum ada bukti eksekusi discard per volume.", "Per-volume discard execution was not verified.")
	f.Recommendation = a.text("Periksa bukti per volume dengan alat penyimpanan sistem.", "Use the system storage tools to investigate an individual volume.")
	return []Finding{f}
}

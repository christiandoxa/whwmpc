package assistant

import (
	"strings"
	"unicode"
)

type Intent string

const (
	Overview  Intent = "overview"
	Slow      Intent = "slow"
	Storage   Intent = "ssd"
	Trim      Intent = "trim"
	Power     Intent = "power"
	Cleanup   Intent = "cleanup"
	Recommend Intent = "recommend"
	History   Intent = "history"
	Settings  Intent = "settings"
)

var Menu = []Intent{Overview, Slow, Storage, Trim, Power, Cleanup, Recommend, History, Settings}

var phrases = map[Intent][]string{
	Overview:  {"overview", "check everything", "system status", "health check", "keseluruhan", "ringkasan", "kondisi komputer"},
	Slow:      {"slow", "lag", "sluggish", "bottleneck", "lambat", "lemot", "macet"},
	Storage:   {"ssd", "nvme", "smart", "drive health", "kesehatan disk"},
	Trim:      {"trim", "fstrim", "discard"},
	Power:     {"power", "clock", "governor", "cpu", "baterai", "battery", "daya", "frequency", "frekuensi"},
	Cleanup:   {"cleanup", "clean", "free up", "cache", "disk space", "bersihkan", "hapus cache", "ruang"},
	Recommend: {"recommend", "recommendation", "suggestions", "rekomendasi", "saran", "profil cocok"},
	History:   {"history", "riwayat", "audit log"},
	Settings:  {"settings", "motion", "pengaturan", "animasi"},
}

func Route(input string) []Intent {
	input = " " + strings.Join(strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " ") + " "
	var results []Intent
	for _, intent := range Menu {
		for _, phrase := range phrases[intent] {
			if strings.Contains(input, " "+phrase+" ") {
				results = append(results, intent)
				break
			}
		}
	}
	return results
}

type Workload string

const (
	Daily   Workload = "daily"
	Build   Workload = "build"
	Gaming  Workload = "gaming"
	Battery Workload = "battery"
	Quiet   Workload = "quiet"
)

var Workloads = []Workload{Daily, Build, Gaming, Battery, Quiet}

func Context(input string) Workload {
	value := strings.ToLower(strings.TrimSpace(input))
	for _, workload := range Workloads {
		if value == string(workload) {
			return workload
		}
	}
	switch value {
	case "kerja harian", "daily work":
		return Daily
	case "build berat", "heavy build":
		return Build
	case "main gim":
		return Gaming
	case "baterai", "hemat daya":
		return Battery
	case "suhu", "kebisingan", "senyap", "cool", "noise":
		return Quiet
	default:
		return ""
	}
}

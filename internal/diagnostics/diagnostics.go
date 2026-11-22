// Package diagnostics interprets observations without reading the host or applying actions.
package diagnostics

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Severity string

const (
	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

type Finding struct {
	ID             string              `json:"id"`
	Category       string              `json:"category"`
	Availability   domain.Availability `json:"availability"`
	Severity       Severity            `json:"severity"`
	Observation    string              `json:"observation"`
	Source         string              `json:"source"`
	At             time.Time           `json:"at"`
	Meaning        string              `json:"meaning"`
	Limits         string              `json:"limits"`
	Recommendation string              `json:"recommendation"`
	Actions        []Action            `json:"actions,omitempty"`
}

type Action struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Label  string `json:"label"`
}

type Analyzer struct {
	Language string
	Now      time.Time
}

const (
	overviewAge = 30 * time.Second
	powerAge    = time.Minute
	trimAge     = 15 * time.Minute
	smartAge    = 30 * time.Minute
)

func (a Analyzer) text(id, en string) string {
	if a.Language == "en" {
		return en
	}
	return id
}

func (a Analyzer) textf(id, en string, values ...any) string {
	return fmt.Sprintf(a.text(id, en), values...)
}

func (a Analyzer) status(av domain.Availability, at time.Time, age time.Duration) domain.Availability {
	switch av {
	case domain.Available, domain.Partial:
		if at.IsZero() {
			return domain.Unknown
		}
		if a.Now.Sub(at) > age {
			return domain.Stale
		}
		return av
	case domain.Unsupported, domain.PermissionRequired, domain.DependencyMissing, domain.Error, domain.Stale:
		return av
	default:
		return domain.Unknown
	}
}

func usable(av domain.Availability) bool {
	return av == domain.Available || av == domain.Partial
}

func (a Analyzer) unavailable(f Finding, component string) Finding {
	status, suggestion := a.text("belum diketahui", "unknown"), a.text("Ulangi pemeriksaan saat data dapat dibaca.", "Scan again when readings are available.")
	switch f.Availability {
	case domain.Stale:
		status, suggestion = a.text("kedaluwarsa", "stale"), a.text("Ambil sampel baru sebelum menilai kondisi saat ini.", "Collect a fresh sample to assess current conditions.")
	case domain.Unsupported:
		status, suggestion = a.text("fitur tidak tersedia di lingkungan ini", "unsupported in this environment"), a.text("Periksa dari sistem host atau gunakan alat yang didukung perangkat.", "Check from the host system or use a tool supported by the device.")
	case domain.PermissionRequired:
		status, suggestion = a.text("izin diperlukan", "permission required"), a.text("Periksa akses baca untuk sumber data ini.", "Check read access for this data source.")
	case domain.DependencyMissing:
		status, suggestion = a.text("tool pendukung belum tersedia", "missing dependency"), a.text("Jalankan whwmpc doctor untuk melihat tool yang diperlukan.", "Run whwmpc doctor to identify the required tool.")
	case domain.Error:
		status, suggestion = a.text("pembacaan gagal", "read failed"), a.text("Periksa pesan diagnostik dan ulangi pemindaian.", "Check the diagnostic details and repeat the scan.")
	}
	f.Observation = component + " · " + status
	f.Meaning = a.text("Data yang dapat dinilai belum tersedia.", "There is no usable evidence for this check.")
	f.Limits = a.text("Kondisi saat ini belum dapat dinilai dari sumber ini.", "This source cannot establish the current condition.")
	f.Recommendation = suggestion
	if f.Availability != domain.Unsupported {
		f.Actions = []Action{a.rescan()}
	}
	return f
}

func finding(id, category, source string, at time.Time, status domain.Availability) Finding {
	return Finding{ID: id, Category: category, Source: source, At: at, Availability: status, Severity: Info}
}

func (a Analyzer) rescan() Action {
	return Action{Kind: "rescan", Label: a.text("Periksa ulang", "Scan again")}
}

func (a Analyzer) navigate(intent assistant.Intent, id, en string) Action {
	return Action{Kind: "navigate", Target: string(intent), Label: a.text(id, en)}
}

func ranked(findings []Finding) []Finding {
	rank := func(s Severity) int {
		switch s {
		case Critical:
			return 2
		case Warning:
			return 1
		}
		return 0
	}
	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Compare(rank(b.Severity), rank(a.Severity))
	})
	return findings
}

func underLoad(samples []float64, threshold float64) bool {
	return len(samples) >= 3 && !slices.ContainsFunc(samples, func(sample float64) bool {
		return !validPercent(sample) || sample < threshold
	})
}

func validPercent(percent float64) bool {
	return percent >= 0 && percent <= 100
}

func sampleRange(samples []float64) (low, high float64, valid bool) {
	if len(samples) == 0 || slices.ContainsFunc(samples, func(sample float64) bool { return !validPercent(sample) }) {
		return 0, 0, false
	}
	return slices.Min(samples), slices.Max(samples), true
}

func safeToken(token string) string {
	if len(token) > 40 || strings.ContainsFunc(token, func(r rune) bool {
		return !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", r)
	}) {
		return ""
	}
	return token
}

func temperature(o domain.Overview) (float64, bool) {
	value := o.Temperature.Celsius
	if !usable(o.Temperature.Availability) || value == nil || math.IsNaN(*value) || *value < -30 || *value > 150 {
		return 0, false
	}
	return *value, true
}

func gib(bytes uint64) float64 { return float64(bytes) / (1 << 30) }

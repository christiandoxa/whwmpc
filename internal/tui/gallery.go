package tui

import (
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
)

var scenarioNames = []Words{
	{"Normal", "Normal"},
	{"Peringatan", "Warning"},
	{"Kritis", "Critical"},
	{"Data tidak tersedia", "Unknown"},
	{"Pembatalan", "Cancellation"},
	{"Hasil parsial", "Partial failure"},
}

func (m *Model) galleryPage() {
	m.page = Page{
		Availability: domain.Available,
		Found: []string{m.tr(Words{"Galeri Piko · DEMO", "Piko gallery · DEMO"}),
			m.tr(Words{"Pilih ekspresi, atau jalankan satu skenario langkah demi langkah.", "Pick an expression or step through a scenario."})},
		Meaning: []string{m.tr(Words{"↑/↓ pilih · Enter tampilkan · m mode · n ukuran ringkas", "↑/↓ select · Enter show · m motion · n narrow"})},
		Advice:  []string{m.tr(Words{"Semua hasil dari fixture. Akses perangkat dan aksi sistem terkunci.", "Results use fixtures. Hardware access and system actions are locked."})},
	}
	for _, label := range scenarioNames {
		m.page.Choices = append(m.page.Choices, Choice{Label: m.tr(label), Kind: "gallery-flow", Value: label.EN})
	}
	for _, pose := range mascot.Gallery {
		m.page.Choices = append(m.page.Choices, Choice{Label: string(pose), Kind: "gallery-pose", Value: string(pose)})
	}
	m.choice, m.scroll, m.stage = 0, 0, 0
	m.show(mascot.Greeting, mascot.Idle, false)
}

type galleryStep struct {
	pose mascot.Pose
	id   string
	en   string
}

var demoFlow = []galleryStep{
	{mascot.Nod, "Pertanyaan · Apa yang aman dibersihkan?", "Question · What is safe to clean?"},
	{mascot.Scanning, "Pemeriksaan · inventaris cache paket contoh", "Scan · fixture package cache inventory"},
	{mascot.Broom, "Temuan · cache APT contoh berukuran 376 MiB", "Finding · sample APT cache contains 376 MiB"},
	{mascot.Preview, "Preview · seluruh cache paket contoh; penghapusan permanen", "Preview · entire fixture package cache; permanent deletion"},
	{mascot.Confirm, "Konfirmasi DEMO · pilih Terapkan demo untuk melanjutkan", "DEMO confirmation · select Apply demo to continue"},
	{mascot.Cleaning, "Menjalankan DEMO · backend fixture sedang menyapu", "Running DEMO · fixture backend is cleaning"},
	{mascot.Applying, "Verifikasi DEMO · cek kembali inventaris fixture", "DEMO verification · recheck the fixture inventory"},
	{mascot.Success, "Terbukti · cache contoh kosong setelah operasi demo", "Verified · demo cache is empty after the fixture operation"},
}

func (m *Model) galleryChoice(c Choice) {
	switch c.Kind {
	case "gallery-pose":
		pose := mascot.Pose(c.Value)
		if slices.Contains(mascot.Gallery, pose) {
			m.page.Found[1] = string(pose)
			m.show(pose, "", false)
			m.accent(false, true)
		}
	case "gallery-flow":
		m.scenario, m.stage = c.Value, -1
		m.advanceDemo()
	case "gallery-next":
		m.advanceDemo()
	case "gallery-home":
		m.galleryPage()
	}
}

func (m *Model) advanceDemo() {
	m.stage++
	step := demoFlow[min(m.stage, len(demoFlow)-1)]
	end := m.stage >= len(demoFlow)-1
	switch {
	case m.scenario == "Warning" && m.stage == 2:
		step = galleryStep{mascot.Warning, "Peringatan · satu direktori cache contoh tidak terbaca", "Warning · one fixture cache directory is unreadable"}
	case m.scenario == "Critical" && m.stage == 2:
		step = galleryStep{mascot.Critical, "SMART contoh melaporkan peringatan kritis. Prioritaskan backup.", "Fixture SMART reports a critical warning. Prioritize backup."}
		end = true
	case m.scenario == "Unknown" && m.stage == 2:
		step = galleryStep{mascot.Unknown, "Status contoh belum diketahui. Tool untuk pemeriksaan tidak tersedia.", "Fixture status is unknown. The scan tool is unavailable."}
		end = true
	case m.scenario == "Cancellation" && m.stage == 5:
		step = galleryStep{mascot.Cancelled, "Dibatalkan · backend fixture belum memulai perubahan", "Cancelled · the fixture backend has not started changing state"}
		end = true
	case m.scenario == "Partial failure" && end:
		step = galleryStep{mascot.Warning, "Sebagian · direktori utama kosong; satu direktori belum dapat diverifikasi", "Partial · the main directory is empty; one directory could not be verified"}
	}
	label := m.tr(copy.demo)
	m.page = Page{
		Availability: domain.Available,
		Found:        []string{label, m.tr(Words{step.id, step.en})},
		Meaning:      []string{m.tr(Words{"Tahap ini ditunjukkan dengan data contoh.", "This stage uses fixture data."})},
	}
	if !end {
		next := m.tr(copy.future)
		if m.stage == 4 {
			next = m.tr(Words{"Terapkan demo", "Apply demo"})
		}
		m.page.Choices = append(m.page.Choices, Choice{Label: next, Kind: "gallery-next"})
	}
	m.page.Choices = append(m.page.Choices, Choice{Label: m.tr(Words{"Kembali ke galeri", "Back to gallery"}), Kind: "gallery-home"})
	m.choice, m.scroll = 0, 0
	m.show(step.pose, "", false)
	m.accent(false, true)
}

func displayStage(p mascot.Pose, lang string) string {
	var words Words
	switch p {
	case mascot.SSD:
		words = Words{"Cek catatan SSD dulu, ya.", "Let me check the SSD logs."}
	case mascot.CPU:
		words = Words{"Membaca frekuensi & pengelola daya", "Reading frequency and power manager"}
	case mascot.Trim:
		words = Words{"Menelusuri mount dan jadwal", "Checking mounts and schedule"}
	case mascot.Broom:
		words = Words{"Sapu sudah siap. Kamu pilih dulu.", "Broom ready. You choose first."}
	case mascot.Preview:
		words = Words{"Menyiapkan pratinjau", "Preparing preview"}
	case mascot.Applying:
		words = Words{"Menerapkan dan memeriksa ulang", "Applying and checking again"}
	case mascot.Cleaning:
		words = Words{"Menjalankan pembersihan", "Running cleanup"}
	case mascot.Restore:
		words = Words{"Memulihkan dan memeriksa ulang", "Restoring and checking again"}
	case mascot.Printing:
		words = Words{"Menulis laporan lokal", "Writing local report"}
	case mascot.Cancelled:
		words = Words{"Penghentian diminta · memeriksa hasil", "Cancellation requested · checking result"}
	default:
		words = Words{"Sebentar, aku cek satu-satu.", "One moment. I'll check each item."}
	}
	return strings.TrimSpace(words.Text(lang))
}

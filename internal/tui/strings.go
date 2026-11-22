package tui

import (
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Words struct{ ID, EN string }

func (w Words) Text(lang string) string {
	if lang == "en" && w.EN != "" {
		return w.EN
	}
	return w.ID
}

var menuNames = map[assistant.Intent]Words{
	assistant.Daemon:     {"Observasi terjadwal", "Scheduled observations"},
	assistant.Firmware:   {"Firmware", "Firmware"},
	assistant.Filesystem: {"Pemeriksaan filesystem", "Filesystem checks"},
	assistant.GPU:        {"Periksa GPU", "Check GPU"},
	assistant.Benchmark:  {"Uji kinerja", "Benchmark"},
	assistant.Overview:   {"Cek kondisi PC", "Check PC condition"},
	assistant.Slow:       {"Cari penyebab PC lambat", "Investigate a slow PC"},
	assistant.Storage:    {"Kesehatan SSD", "SSD health"},
	assistant.Trim:       {"Perawatan SSD / TRIM", "Check TRIM"},
	assistant.Power:      {"Lihat kondisi daya", "Check power settings"},
	assistant.Cleanup:    {"Bersihkan ruang", "Free up space"},
	assistant.Recommend:  {"Pilih profil daya", "Choose a power profile"},
	assistant.History:    {"Riwayat", "History"},
	assistant.Settings:   {"Pengaturan", "Settings"},
	assistant.Report:     {"Laporan", "Report"},
}

var statusNames = map[domain.Availability]Words{
	domain.Available:          {"Tersedia", "Available"},
	domain.Partial:            {"Sebagian", "Partial"},
	domain.Unknown:            {"Belum diketahui", "Unknown"},
	domain.Unsupported:        {"Tidak didukung", "Unsupported"},
	domain.PermissionRequired: {"Butuh izin", "Permission required"},
	domain.DependencyMissing:  {"Tool belum ada", "Missing dependency"},
	domain.Error:              {"Gagal", "Error"},
	domain.Stale:              {"Kedaluwarsa", "Stale"},
}

var copy = struct {
	tagline, menu, evidence, session, greeting, ask, placeholder, palette, ready, scanning, waiting, demo, fallback, initial Words
	found, meaning, advice, choices, clarify, unknown, showHelp, needsData, previous, future, details, report, choicesHint   Words
	memory, disk, temp, battery, connected, powerUnknown, missing, uptime, os, processes                                     Words
}{
	tagline:      Words{"ASISTEN SISTEM LOKAL", "LOCAL SYSTEM ASSISTANT"},
	menu:         Words{"PEMERIKSAAN", "CHECKS"},
	evidence:     Words{"BUKTI & KONTEKS", "EVIDENCE & CONTEXT"},
	session:      Words{"SESI ASISTEN", "ASSISTANT SESSION"},
	greeting:     Words{"Hai! Mau cek apa di komputermu hari ini?", "Hi! What would you like to check on your computer today?"},
	ask:          Words{"TANYA", "ASK"},
	placeholder:  Words{"mis. Kenapa PC terasa lambat?", "e.g. Why is my PC slow?"},
	palette:      Words{"Cari kategori atau tulis pertanyaan", "Search checks or type a question"},
	ready:        Words{"SIAP", "READY"},
	scanning:     Words{"MEMERIKSA", "SCANNING"},
	waiting:      Words{"Menunggu hasil pemeriksaan…", "Waiting for scan results…"},
	demo:         Words{"DEMO · data contoh · aksi terkunci", "DEMO · fixture data · actions locked"},
	fallback:     Words{"Perbesar terminal", "Enlarge the terminal"},
	initial:      Words{"Pilih pemeriksaan di menu atau ketik pertanyaan di bawah.", "Pick a check from the menu or type a question below."},
	found:        Words{"Yang ditemukan", "What I found"},
	meaning:      Words{"Artinya", "What it means"},
	advice:       Words{"Saran", "Recommendation"},
	choices:      Words{"Pilihan tindakan", "Next steps"},
	clarify:      Words{"Ada beberapa kemungkinan. Mana yang ingin diperiksa dulu?", "I matched several checks. Which should I run first?"},
	unknown:      Words{"Saya belum menangkap maksudnya. Pilih pemeriksaan di bawah.", "I couldn't match a check. Pick one below."},
	showHelp:     Words{"Bantuan keyboard", "Keyboard help"},
	needsData:    Words{"Belum ada data untuk pemeriksaan ini.", "No data is available for this check."},
	previous:     Words{"Sebelumnya", "Previous"},
	future:       Words{"Berikutnya", "Next"},
	details:      Words{"Detail bukti", "Evidence details"},
	report:       Words{"Laporan", "Report"},
	choicesHint:  Words{"←/→ pilih tindakan · Enter lanjut", "←/→ select action · Enter continue"},
	memory:       Words{"MEMORI", "MEMORY"},
	disk:         Words{"DISK /", "DISK /"},
	temp:         Words{"SUHU CPU", "CPU TEMP"},
	battery:      Words{"baterai", "battery"},
	connected:    Words{"daya AC", "AC power"},
	powerUnknown: Words{"sumber daya belum diketahui", "power source unknown"},
	missing:      Words{"belum ada data", "no data yet"},
	uptime:       Words{"Waktu aktif", "Uptime"},
	os:           Words{"Lingkungan", "Environment"},
	processes:    Words{"Proses teramati", "Sampled processes"},
}

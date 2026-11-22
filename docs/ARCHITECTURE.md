# Arsitektur

`cmd/whwmpc` menyediakan TUI saat stdin dan stdout interaktif, serta perintah plain/JSON. `internal/assistant` memetakan kata dan frasa Indonesia/Inggris ke intent yang terdaftar. Masukan pertanyaan tidak digunakan sebagai argv.

- `internal/domain` menyimpan jenis metrik, waktu, ketersediaan, dan sanitasi teks. `internal/platform` memiliki provider Linux dan stub build-tag yang tetap memberi identitas runtime. Provider demo memakai data tetap.
- `internal/inspection` menghubungkan overview, `internal/storage`, dan `internal/power`. SMART dibaca ketika diminta. Cache sesi berumur dua menit dengan opsi pembacaan ulang. Bukti detail memakai sysfs, smartctl JSON, mountinfo, journal, dan D-Bus.
- `internal/diagnostics` membaca snapshot tanpa I/O. Aturan menghasilkan temuan ber-ID dengan severity dan availability yang terpisah. [Daftar aturan dan sumber](DIAGNOSTICS.md)
- `internal/actions` menyiapkan operasi dari allowlist. `internal/workflow` mengikatnya ke kunci lokal, konfirmasi, klaim satu kali, verifikasi, dan riwayat. `internal/state` memeriksa lokasi privat, membatasi retensi, dan mempublikasikan file baru secara atomik.
- `internal/report` memakai proyeksi redaksi untuk JSON dan Markdown. Snapshot mentah hanya dipakai lokal untuk bukti yang diminta pengguna. Tidak ada telemetry.
- `internal/tui` menerima snapshot, progress, dan hasil sebagai pesan Bubble Tea. Probe dan subprocess berjalan sebagai command asinkron. Pemeriksaan dimulai dari pilihan pengguna. Pengumpulan snapshot dan timer animasi memakai lifecycle terpisah. Satu foreground job dapat dihentikan dan diselesaikan hingga hasilnya jelas. [Siklus motion](MOTION.md)

Pemeriksaan proses membaca nama pendek dan RSS untuk tampilan lokal, dengan sampling delta CPU. Argumen proses tidak dikumpulkan. Batas sampling singkat, sensor yang tidak ada, dan pertentangan antar sumber tercermin dalam temuan.

## Backlog

Status, matriks baseline, dan bukti per item ada di [Backlog](BACKLOG.md). Integrasi Upkit dibatalkan sesuai instruksi pengguna.

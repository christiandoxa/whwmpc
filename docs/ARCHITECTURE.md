# Arsitektur

`cmd/whwmpc` menyediakan TUI saat stdin dan stdout interaktif, serta perintah plain/JSON. `internal/assistant` memetakan kata dan frasa Indonesia/Inggris ke intent yang terdaftar. Masukan pertanyaan tidak digunakan sebagai argv.

- `internal/domain` menyimpan jenis metrik, waktu, ketersediaan, dan sanitasi teks. `internal/platform` memiliki provider untuk Linux, macOS, Windows, dan fixture demo. [Cakupan native](NATIVE.md)
- `internal/inspection` menghubungkan overview, `internal/storage`, dan `internal/power`. SMART dibaca ketika diminta. Cache sesi berumur dua menit dengan opsi pembacaan ulang. Bukti detail memakai sysfs, smartctl JSON, mountinfo, journal, dan D-Bus.
- `internal/privread` menyediakan sesi baca terbatas. `internal/inspection` memakai respons bertipe dari helper ketika cakupan telah disetujui dan beralih ke collector biasa jika akses gagal. `cmd/whwmpc` menangani entry point helper sebelum memuat konfigurasi, riwayat, atau UI. [Cakupan dan autentikasi](PRIVILEGE.md)
- `internal/diagnostics` membaca snapshot tanpa I/O. Aturan menghasilkan temuan ber-ID dengan severity dan availability yang terpisah. [Daftar aturan dan sumber](DIAGNOSTICS.md)
- `internal/actions` menyiapkan operasi dari allowlist. `internal/workflow` mengikatnya ke kunci lokal, konfirmasi, klaim satu kali, verifikasi, dan riwayat. `internal/state` memeriksa lokasi privat, membatasi retensi, dan mempublikasikan file baru secara atomik.
- `internal/report` memakai proyeksi redaksi untuk JSON dan Markdown. Snapshot mentah hanya dipakai lokal untuk bukti yang diminta pengguna. Tidak ada telemetry.
- `internal/tui` menerima snapshot, progress, dan hasil sebagai pesan Bubble Tea. Probe dan subprocess berjalan sebagai command asinkron. Pemeriksaan dimulai dari pilihan pengguna. Pengumpulan snapshot dan timer animasi memakai lifecycle terpisah. Satu foreground job dapat dihentikan dan diselesaikan hingga hasilnya jelas. [Siklus motion](MOTION.md)
- `internal/hardware`, `internal/firmware`, dan `internal/filesystem` membaca kemampuan backend resmi dan memeriksa identitas target. Mutasi Linux masuk melalui action service yang sama.
- `internal/daemon` mengatur systemd user timer secara eksplisit. Timer menjalankan pengumpulan snapshot read-only dengan lock. `internal/benchmark` mengukur workload terbatas di bawah konteks yang dapat dibatalkan.

Pemeriksaan proses membaca nama pendek dan RSS untuk tampilan lokal, dengan sampling delta CPU. Argumen proses tidak dikumpulkan. Batas sampling singkat, sensor yang tidak ada, dan pertentangan antar sumber tercermin dalam temuan.

## Backlog

Status, matriks baseline, dan bukti per item ada di [Backlog](BACKLOG.md). Integrasi Upkit dibatalkan sesuai instruksi pengguna.

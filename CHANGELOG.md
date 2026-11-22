# Changelog

## Belum dirilis

- Pemeriksaan daya dan TRIM macOS/Windows menampilkan sumber serta status native di CLI, TUI, dan laporan. Bacaan baterai tanpa sumber daya tetap parsial. Laporan Windows mencantumkan kesehatan drive dan counter reliabilitas yang tersedia.

## 0.3.0

- Stress CPU memakai kanal suhu yang sama sepanjang uji. Guard menghentikan beban saat pembacaan hilang, terlambat, atau mencapai 85 °C.
- Cleaner Pacman mendukung Arch Linux ARM aarch64 dengan cakupan, lock, dan verifikasi paket yang sama.
- Preview firmware memerlukan bukti baterai tidak terpasang jika kapasitas tidak tersedia. Inventaris AMD menyembunyikan kontrol hanya-baca.
- Piko menampilkan status pengisian yang dilaporkan macOS atau Windows. Aset standalone menambah macOS amd64 dan Windows arm64.

## 0.2.0

- Beranda tiga tugas, ringkasan berbasis cakupan, detail terpisah, review dengan pilihan awal Kembali, dan ekspor snapshot. Piko ringkas mengikuti pemeriksaan dan hasil.
- Monitoring GPU dan perubahan eksplisit yang didukung NVIDIA/AMDGPU dengan preview, verifikasi, dan restore bersyarat.
- Inventaris fwupd, rencana pembaruan LVFS, dan verifikasi firmware setelah restart. Perbaikan ext2/3/4 memakai partisi offline dan pembacaan ulang.
- Observasi systemd user opt-in dan benchmark CPU, stress dengan sensor, serta I/O file yang dibatasi.
- Inventaris dan pembersihan cache paket Fedora/DNF5 dan Arch/pacman dengan verifikasi cakupan.
- Provider macOS dan Windows, audit storage, dan cek filesystem APFS/NTFS baca-saja.
- Paket standalone Linux amd64/arm64, macOS arm64, dan Windows amd64 dengan SHA-256 dan smoke test. Installer Linux mendukung tag rilis eksplisit.

## 0.1.0

- Asisten Bubble Tea dengan overview Linux, pertanyaan Bahasa Indonesia/Inggris, diagnosis berbasis beberapa sampel, dan preferensi konteks daya.
- Audit SMART/NVMe, TRIM per filesystem, CPUFreq, dan backend daya dengan status capability yang eksplisit.
- Preview dan konfirmasi untuk PPD, TRIM manual dan timer, serta cache APT. Hasil diverifikasi, dan perubahan profil atau timer yang didukung dapat ditinjau untuk pemulihan.
- Riwayat lokal dengan retensi, laporan JSON/Markdown teredaksi, dan executable Linux amd64/arm64 ber-checksum.
- Piko dengan motion full/reduced/off, layout terminal adaptif, dan galeri fixture yang mencakup konfirmasi, verifikasi, pembatalan, dan hasil parsial.

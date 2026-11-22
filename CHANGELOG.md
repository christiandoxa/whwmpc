# Changelog

## 0.3.5

- Pemeriksaan CPU macOS mempertahankan interval yang selesai sebagai data parsial ketika `top` mencapai batas waktu.
- Unduhan CAB mengikuti pengalihan LVFS ke `r2.fwupd.org` untuk nama arsip yang sama. Pemeriksaan digest dan tanda tangan tetap berlaku.
- Layar firmware menjelaskan saat `fwupdmgr` hilang atau platform belum didukung. Jumlah perangkat tampil ketika inventaris berhasil dibaca.

## 0.3.4

- Keluar segera setelah mengganti mode gerak tetap menyimpan pilihan terakhir.

## 0.3.3

- Piko tampil utuh pada margin prompt yang lebar. Input GPU yang tidak valid menampilkan ekspresi error dan memfokuskan pilihan Kembali.
- Rencana daya NVIDIA mengikuti resolusi pembacaan ulang yang dilaporkan driver.
- Verifikasi firmware mengabaikan versi aktif yang berasal dari perangkat emulasi fwupd.
- macOS mempertahankan status pengisian dijeda, pengisian akhir, dan status yang tersedia tanpa persentase baterai.

## 0.3.2

- Riwayat menutup pilihan pemulihan setelah hasilnya terverifikasi. Rencana pemulihan yang sudah ditandatangani diperiksa kembali saat eksekusi.
- Detail GPU membedakan daya papan, daya SoC, serta batas daya NVIDIA yang diminta dan ditegakkan.
- Verifikasi firmware dari Riwayat menampilkan status restart, gagal, atau terverifikasi beserta versi aktif yang teramati.
- Windows melabeli sampel CPU satu grup sebagai parsial pada host dengan lebih dari 64 prosesor logis. Diagnosis menggunakan cakupan yang sesuai.
- Paket standalone memakai mode file dokumen dan lisensi yang konsisten.

## 0.3.1

- Piko menampilkan aksesori pada layout ringkas. Lirikan, highlight fokus, reveal panel, dan anggukan pertanyaan kembali terlihat. Galeri lebar menampilkan pose karakter 16×6.
- Refresh awal LVFS memverifikasi cache JCat pada fwupd 2.0.x ketika checksum belum muncul di respons daemon. Alur bertanda tangan lulus dari cache kosong hingga verifikasi capsule setelah simulasi boot.
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

# Firmware

Dukungan pembacaan adalah Linux amd64 dan arm64 dengan `fwupdmgr`. Inventaris menggunakan `get-devices --json`, sedangkan daftar kandidat memakai `get-updates --json` dan metadata lokal yang sudah tersedia. Daftar kosong berarti backend belum memberi bukti kandidat. Inventaris tidak menyertakan nama akun atau nomor seri perangkat.

Jalur tulis mendukung UEFI capsule perangkat dengan plugin `uefi_capsule` di fwupd 2.0.x. Aksi memakai ID perangkat dan versi yang dipilih, satu komponen, metadata LVFS stabil yang divalidasi fwupd, SHA-256 CAB, dan payload bertanda tangan. Mesin harus memakai daya AC yang terdeteksi. Batas baterai digunakan jika fwupd melaporkan baterai. Daemon harus memiliki `OnlyTrusted=true`. Cabang alternatif, downgrade, perangkat yang terkunci, emulasi, dan paket dengan hasil pencocokan ambigu ditolak.

Metadata dapat diminta melalui aksi refresh LVFS. Rencana memperlihatkan akses jaringan dan perubahan cache lokal. Remote unduhan aktif harus LVFS stabil dan URL metadata serta tanda tangan harus sesuai konfigurasi resmi. JSON mode fwupd melewati alur pelaporan otomatis pada perintah refresh. Pemeriksaan offline tidak mengunduh metadata ataupun firmware.

Setelah konfirmasi pembaruan, aplikasi mengunduh satu CAB melalui HTTPS, membatasi ukurannya, memeriksa SHA-256, dan meminta fwupd mencocokkan kembali GUID, versi, dan tanda tangan payload. Aplikasi memeriksa ulang perangkat, metadata, konfigurasi trust, dan daya setelah unduhan. Instalasi memakai otorisasi sistem melalui fwupd dan tidak melakukan restart otomatis. Simpan cadangan dan kunci pemulihan enkripsi saat meninjau rencana firmware.

Jika fwupd mencatat pending atau needs-reboot, restart sendiri sambil mempertahankan daya AC. Lanjutkan dari riwayat, dengan `whwmpc firmware --resume PLAN_ID`, atau dengan `whwmpc firmware --verify DEVICEID@VERSI` dari hasil aksi. Verifikasi menggunakan riwayat fwupd yang bertahan melewati reboot serta versi aktif yang terbaca ulang. Ketiadaan bukti yang cocok dilaporkan sebagai unknown. Backend ini tidak menyediakan pemulihan firmware.

Fixture menguji pembacaan metadata, target berubah, digest unduhan, kecocokan CAB, izin, output rusak, metadata kedaluwarsa, dan pemisahan pending, gagal, serta hasil terverifikasi setelah reboot. Host pengembangan memiliki fwupd 2.0.20. Validasi host sebatas inventaris dan pembacaan konfigurasi. Flash dan reboot pada perangkat nyata memerlukan izin khusus.

[fwupdmgr](https://fwupd.github.io/libfwupdplugin/fwupdmgr.html) · [Kode fwupd 2.0.20](https://github.com/fwupd/fwupd/tree/2.0.20) · [Arsitektur dan tanda tangan LVFS](https://lvfs.readthedocs.io/en/latest/intro.html) · [Enum status pembaruan](https://fwupd.github.io/libfwupd/enum.UpdateState.html)

Riwayat menyimpan ID rencana yang masih perlu verifikasi dengan target tersimpan privat. Setelah restart, buka **Riwayat → Periksa firmware setelah restart** atau jalankan `whwmpc firmware --resume PLAN_ID`. Kedua jalur hanya membaca status fwupd.

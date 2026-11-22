# Pemeriksaan mendalam

Mulai `whwmpc` dari terminal sebagai pengguna biasa dan pilih tujuan. Sebelum tugas pertama, **Mulai tanpa administrator** menjadi pilihan awal. Pilihan administrator menampilkan cakupan yang dapat dibaca oleh backend untuk tujuan itu. Setelah autentikasi, tugas langsung berjalan dengan akses yang tersedia. Piko menampilkan mode aktif di judul. Buka **Pemeriksaan lain → Alat bantu → Pengaturan → Tingkat pemeriksaan** untuk mengganti mode. Pengaturan juga tersedia dari **Pilihan lain** pada hasil pemeriksaan.

| Platform | Autentikasi | Cakupan baca tambahan |
| --- | --- | --- |
| Linux | `sudo` sistem di terminal asli | SMART dan pemetaan drive, discard dan jurnal `fstrim.service`, CPUFreq, daya dan sensor CPU, telemetri GPU, inventaris firmware lokal, inventaris filesystem |
| macOS | `sudo` bawaan di terminal asli | Kesehatan drive yang dilaporkan `diskutil`, kemampuan TRIM yang dilaporkan perangkat, dan inventaris APFS |
| Windows | UAC untuk proses helper | Storage Management dan reliability counters, kebijakan notifikasi TRIM, dan inventaris volume native |

macOS memakai otorisasi terminal yang didokumentasikan Apple. Windows memakai `ShellExecuteExW` dengan verb `runas`. [Panduan terminal Apple](https://support.apple.com/guide/terminal/enter-administrator-commands-apd5b0b6259-a7d4-4435-947d-0dff528912ba/mac) · [Windows ShellExecuteW](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecutew) · [Windows ShellExecuteExW](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecuteexw)

## Sesi dan batas akses

- Jika SMART atau pemeriksaan lain terhalang izin, pilih **Lengkapi pemeriksaan** pada hasil. Layar persetujuan menampilkan cakupan komponen tersebut. Pemeriksaan lain di ringkasan tetap memakai data dan waktu sampel sebelumnya. Jika autentikasi batal atau gagal, hasil dan pilihan sebelumnya tetap tersedia. Mengganti akses melalui Pengaturan mengembalikan layar asal tanpa memulai pemeriksaan baru.
- UI tetap berjalan sebagai pengguna biasa. Bubble Tea melepas terminal selama autentikasi dan memulihkannya saat kembali. Prompt kredensial berasal dari OS.
- Helper adalah proses terpisah dengan protokol baca bertipe. Pesan hanya memuat ID berurutan dan operasi yang terdaftar. Target perangkat berasal dari inventaris lokal yang divalidasi. Helper memakai executable sistem dari jalur yang diperiksa dan argumen baca yang dibatasi. Respons yang melebihi 4 MiB, tidak cocok dengan permintaan, atau gagal validasi menutup sesi.
- Linux dan macOS memakai dua pipe yang diwariskan dari proses pemanggil. Autentikasi interaktif memakai `sudo -k` untuk perintah helper, sehingga sesi baca tidak memperbarui timestamp autentikasi sudo. Windows memakai named pipe lokal dengan ACL, penolakan koneksi remote, dan pemeriksaan PID serta identitas executable. [Keamanan named pipe Windows](https://learn.microsoft.com/en-us/windows/win32/ipc/named-pipe-security-and-access-rights)
- Persetujuan membuat sesi baca hingga sepuluh menit. Collector dipanggil saat tugas terkait dijalankan. Keluar, mengganti ke Biasa, membatalkan pembacaan yang sedang memakai helper, atau mencapai batas waktu menutup akses sesi. Kredensial dikelola OS dan data sesi disimpan di memori.
- Saat pengguna melengkapi satu komponen, sesi yang sudah mencakup pembacaan itu dapat digunakan kembali. Persetujuan scope baru yang berhasil mengganti sesi baca sebelumnya. Penolakan scope baru mempertahankan sesi lama selama masih berlaku.
- Hasil bergantung pada sensor, perangkat, pengandar, dan dependency. CPU, RAM, disk, serta collector biasa tetap memberi data yang tersedia jika helper gagal. Data parsial dan kebutuhan izin muncul pada hasil serta cakupan. Mode demo memakai fixture dan mensimulasikan persetujuan tanpa membuka prompt OS.
- Fetch firmware, benchmark, stress, cleanup, TRIM manual, perbaikan, dan tuning memiliki alur sendiri. Mutasi tetap memerlukan preview, konfirmasi, hak akses tindakan, revalidasi, eksekusi, dan verifikasi. [Model tindakan](ACTIONS.md)

## CLI tanpa prompt

Opsi `--privileged-read` menerima `check`, `doctor`, `power`, atau `trim` dengan `--json` opsional. Letakkan opsi global sebelum nama perintah.

```sh
whwmpc --privileged-read check --json
```

Linux dan macOS menggunakan kebijakan sudo noninteraktif yang sudah mengizinkan helper atau proses CLI yang telah memiliki izin administrator. Di Windows, CLI mendalam memerlukan proses yang telah mendapat akses administrator. Jika izin gagal atau sesi habis, perintah melaporkan penyebab dan mengembalikan exit code `1`. TUI tetap dapat melakukan pemeriksaan Biasa. Opsi tindakan menghasilkan exit code `2` ketika digabungkan dengan `--privileged-read`.

## Pengujian

Fixture menguji penolakan, pembatalan, masa berlaku, pembacaan parsial, peralihan mode, respons rusak, dan penolakan operasi atau target di luar cakupan. PTY menjalankan alur Biasa dan Mendalam pada 80×24 dan 120×36, serta memeriksa pergantian alternate screen saat autentikasi fixture. Linux juga menjalankan helper dengan operasi baca suhu dalam user namespace dan memulihkan terminal asli saat otorisasi native tidak tersedia. CI native menguji protokol dan named pipe pada Windows serta build dan collector terkait pada Linux dan macOS. Validasi pemberian izin administrator di desktop tetap terbuka. [Alur dan bukti terminal](UX.md)

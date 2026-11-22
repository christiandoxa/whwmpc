# Alur TUI

Saat startup interaktif, Piko menampilkan manfaat aplikasi dan pilihan tujuan. Setelah memilih tujuan pertama, layar akses menampilkan cakupan baca dan menyorot **Mulai tanpa administrator**. Saat tersedia, pilihan administrator membuka autentikasi OS. Persetujuan, penolakan, dan pembatalan autentikasi meneruskan tugas yang sama dengan akses yang tersedia. Judul menunjukkan mode sesi, dan hasil menghitung pemeriksaan yang perlu izin. [Cakupan dan autentikasi](PRIVILEGE.md)

Beranda menyorot **Cek kondisi PC** untuk memeriksa CPU, RAM, ruang simpan, SSD, TRIM, dan daya. **Cari penyebab PC lambat** menanyakan apakah gejalanya sedang terjadi sebelum memilih akses. **Bersihkan ruang** membuka inventaris cache dan menampilkan estimasi ruang. **Pemeriksaan lain** memuat SSD, TRIM, daya, pemeriksaan lanjutan, dan alat bantu.

Gunakan ↑/↓ untuk daftar, Enter untuk membuka pilihan, PgUp/PgDn untuk menggulir bukti, dan Esc untuk kembali atau meminta penghentian. `/` membuka pertanyaan bebas dengan contoh istilah yang bisa diarahkan ke pemeriksaan. **Alat bantu** memuat riwayat, laporan, dan pengaturan. Kolom utama memakai maksimum 76 sel. Pada 120×36 Piko memakai margin kanan. Pada 80×24 wajah dan aksesori Piko berada di dekat judul. Ukuran minimum 48×18. Di bawah batas itu, layar meminta terminal diperbesar dan Esc serta `q` tetap berfungsi.

## Mengamati PC lambat

**Ya, pantau sekarang** mengambil tiga interval CPU sekitar empat detik dan mencatat RAM, proses, dan konteks daya yang tersedia. Selama pengamatan, waktu berjalan berasal dari jam nyata dan Esc meminta penghentian. Linux dan Windows memakai delta proses yang dapat dibaca. macOS memakai rata-rata dari `ps` dengan status parsial dan membandingkan halaman bebas yang terbaca pada awal dan akhir. SMART, TRIM, dan inventaris firmware memiliki tugas sendiri.

**Belum, ambil kondisi awal** membuat sampel pembanding. Penyebab kejadian sebelumnya tetap belum diketahui. **Pantau saat PC lambat** memulai pengamatan ketika pengguna sudah siap. Jawaban dan durasi tetap sama setelah autentikasi. Ringkasan menampilkan rentang beban dan proses teratas jika tersedia, dengan batas sampel di detail.

## Hasil dan cakupan

Ringkasan menampilkan kesimpulan, metrik, saran, lalu waktu sampel dan cakupan. **Selesai** menutup tugas dan menampilkan ringkasan terakhir di beranda. Temuan kritis mengutamakan bukti komponen dan saran seperti mencadangkan SSD. Peringatan dengan tindakan yang relevan bisa langsung membuka review, seperti **Tinjau jadwal TRIM** ketika jadwal diperlukan, teramati nonaktif, dan prasyarat tindakan terpenuhi. **Lengkapi pemeriksaan** hanya tersedia untuk komponen terhalang izin yang didukung helper.

Cakupan menghitung setiap ID pemeriksaan sekali. **Parsial**, **gagal**, dan **belum tersedia** tetap terlihat bersama peringatan. Detail membedakan izin, data kedaluwarsa, dependency, fitur yang tidak didukung, dan kesalahan pembacaan.

- Bar CPU memakai sampel terakhir. Jumlah, durasi, dan rentang memakai sampel yang benar-benar dikumpulkan. Bar RAM mengikuti provider. Tanda `≥` pada RAM macOS menandai batas bawah dari halaman bebas. Bar disk memakai persentase dari provider untuk filesystem utama.
- Suhu memakai Celsius dan sumber sensor. Drive SSD yang perlu perhatian tampil lebih dulu. Endurance menampilkan nilai mentah termasuk nilai di atas 100%. Detail menyimpan counter SMART utuh.
- TRIM menampilkan dukungan, jadwal atau kebijakan, dan bukti terakhir secara terpisah. Daya menunjukkan profil dan sumber aktual serta membedakan clock terukur, rata-rata, driver, dan batas konfigurasi.
- Cleanup menunjukkan estimasi dan cakupan sebelum review. Hasil memisahkan estimasi, data terhapus yang teramati, selisih ruang bebas, potensi discard, dan verifikasi. Halaman lanjutan menampilkan ukuran yang sesuai dengan tugasnya, termasuk throughput benchmark, versi firmware, dan usia observasi terjadwal.
- `--ascii` mengganti karakter diagram dan `NO_COLOR` memberi tema terminal. Mode gerak mengatur Piko dan aksen tampilan. Angka tetap mengikuti snapshot saat Piko bergerak.

**Detail komponen** membuka bukti yang dikelompokkan menurut data, makna, sumber, waktu, batas, dan saran. Tindakan terkait dapat dipilih dari layar detail. **Bukti lengkap** memuat bukti teknis yang berbeda. Esc mengembalikan hasil, pilihan, dan posisi sebelumnya. **Simpan hasil ini** memakai snapshot yang sedang dibuka. **Buat laporan lengkap** mengumpulkan data tambahan sesuai permintaan. **Periksa ulang** menampilkan awal dan akhir pembacaan baru walaupun nilainya sama.

## Review dan hasil aksi

Satu layar review menampilkan target, kondisi sebelum dan sesudah, dampak, risiko, izin, dan kemungkinan pemulihan. **Kembali** menjadi pilihan awal. Rencana panjang perlu dibaca dengan gulir sebelum opsi tindakan bisa digunakan. Resize mempertahankan pilihan dan mengharuskan peninjauan kembali bila pembungkusan berubah. Selama aksi, Esc meminta penghentian dan hasil verifikasi tetap ditunggu. Hasil aksi menawarkan **Selesai**, detail, dan ringkasan sebelum tindakan.

`whwmpc --demo --demo-scenario cleanup-result` menambahkan **Simulasikan pembersihan** setelah review. Skenario itu memakai inventaris dan hasil fixture, dengan bytes terhapus dan selisih ruang bebas yang ditampilkan terpisah. `whwmpc --demo --demo-scenario ssd-permission` memperagakan SSD yang baru terbaca setelah persetujuan fixture.

## Bukti dari PTY

Walkthrough internal dijalankan di Linux amd64 dengan Go 1.27.4, locale `C.UTF-8`, `--demo --lang id --motion=off`, dan direktori state sementara. Baseline ialah `a50c1ee` (`whwmpc v0.4.0-a50c1ee`) dari `/tmp/whwmpc-journey.oNIwD0CG/baseline/whwmpc`. Build sesudah ialah `b29e21a` (`whwmpc journey-b29e21a`) dari `/tmp/whwmpc-journey.oNIwD0CG/whwmpc-journey`. Keduanya dibangun dari arsip commit bersih. Jeda antarinput memberi waktu melihat tujuan, pilihan, hasil, dan langkah utama pada tiap tahap. Autentikasi dan hasil cleanup di rekaman berasal dari fixture.

Hitungan berikut memakai jumlah Enter pada jalur terpendek menuju keluaran utama, di luar prompt OS, tombol arah, dan kunjungan detail opsional.

| Perjalanan | Ukuran | Enter sebelum → sesudah | Yang terlihat setelah perubahan | Rekaman |
| --- | --- | --- | --- | --- |
| Cek kondisi biasa | 80×24 | 2 → 2 | Tujuan lalu pilihan akses. Ringkasan tanpa peringatan menawarkan **Selesai** dan menampilkan hasil terakhir di beranda. | [Sebelum](assets/ux/journey/before-condition.cast) · [Sesudah](assets/ux/journey/after-condition.cast) |
| Cek kondisi mendalam | 120×36 | 3 → 2 | Persetujuan baca menyebut cakupan, terminal dipulihkan setelah autentikasi fixture, dan tugas langsung menampilkan hasil. | [Sebelum](assets/ux/journey/before-deep.cast) · [Sesudah](assets/ux/journey/after-deep.cast) |
| SSD terhalang izin | 80×24 | 3 ke hasil biasa → 5 ke hasil dilengkapi | Label izin muncul pada hasil SSD. Rekaman juga mencoba Kembali dari persetujuan dan mengulangi jalur itu, sehingga memakai tujuh Enter sebelum SMART terbaca. Baseline memakai fixture SSD terbaca. | [Sebelum](assets/ux/journey/before-ssd.cast) · [Sesudah](assets/ux/journey/after-ssd.cast) |
| Lambat sedang terjadi | 80×24 | 2 ke sampel singkat → 3 ke pengamatan | Pertanyaan gejala menentukan pengamatan sekitar 12 detik. Waktu berjalan, rentang beban, dan proses teratas terlihat sebelum **Selesai**. | [Sebelum](assets/ux/journey/before-slow.cast) · [Sesudah](assets/ux/journey/after-slow.cast) |
| Bersihkan ruang | 80×24 | 3 ke review → 4 ke hasil fixture | Estimasi dan cakupan mengantar ke review dengan Kembali terpilih. Rekaman juga kembali dari review dan membukanya ulang, sehingga memakai enam Enter sebelum hasil simulasi. | [Sebelum](assets/ux/journey/before-cleanup.cast) · [Sesudah](assets/ux/journey/after-cleanup.cast) |

Di luar rekaman fixture, provider Linux amd64 pada checkpoint `b1ba780` mengumpulkan tiga sampel CPU, tiga sampel RAM, dan data proses dalam 12,035 detik. CI `b29e21a` menguji pengamatan baca-saja di Windows amd64/arm64 dalam 12,011–12,031 detik dengan tiga sampel CPU dan RAM. Runner macOS amd64/arm64 berjalan 12,44–12,863 detik dengan tiga sampel CPU dan dua sampel RAM. Angka tersebut mengukur durasi pemeriksaan. [Pengujian native](https://github.com/christiandoxa/whwmpc/actions/runs/45051531260)

Validasi terminal mencakup 48×18, fallback di bawah batas, ASCII, NO_COLOR, mode gerak, pemulihan atribut terminal, serta pembatalan. Pengujian perangkat fisik untuk perubahan sistem tetap memerlukan otorisasi tersendiri.

### Arsip revamp sebelumnya

Alur startup Mendalam direkam dari build `e220915` dengan versi `whwmpc dev` di `/tmp/whwmpc-privilege-evidence.hJGYSPKF/whwmpc`. Rekaman 80×24 memakai fixture, termasuk perpindahan terminal saat otorisasi dan ringkasan sesudah memilih **Cek kondisi PC**. [Rekaman pemeriksaan mendalam](assets/ux/deep-check-80x24.cast)

Ringkasan sebelum perubahan dicuplik dari build `5f4a8d7` di `/tmp/whwmpc-visual-before.WtSEAd7M/whwmpc`. Ringkasan sesudah perubahan dicuplik dari build `c0eb036` di `/tmp/whwmpc-visual-after.JJNTZPHv/whwmpc`. Keduanya menampilkan `whwmpc dev` dan berjalan dalam PTY Linux dengan `--demo --motion off --theme dark`. PNG berasal dari frame akhir asciicast sebelum keluar.

| Skenario | Ukuran terminal | Sebelum | Sesudah |
| --- | --- | --- | --- |
| Cek kondisi | 80×24 | [PNG](assets/ux/results/overview-80x24.before.png) | [PNG](assets/ux/results/overview-80x24.after.png) · [Rekaman](assets/ux/results/overview-80x24.cast) |
| Cek kondisi | 120×36 | [PNG](assets/ux/results/overview-120x36.before.png) | [PNG](assets/ux/results/overview-120x36.after.png) |
| SSD | 80×24 | [PNG](assets/ux/results/ssd-80x24.before.png) | [PNG](assets/ux/results/ssd-80x24.after.png) |
| TRIM | 80×24 | [PNG](assets/ux/results/trim-80x24.before.png) | [PNG](assets/ux/results/trim-80x24.after.png) |
| Firmware terkunci dalam demo | 80×24 | [PNG](assets/ux/results/firmware-unavailable-80x24.before.png) | [PNG](assets/ux/results/firmware-unavailable-80x24.after.png) |
| Cleanup | 80×24 | [PNG](assets/ux/results/cleanup-80x24.before.png) | [PNG](assets/ux/results/cleanup-80x24.after.png) |

Rekaman berikut memperlihatkan perubahan beranda dan galeri Piko.

- Beranda [sebelum 80×24](assets/ux/before-80.png) dari build `ac30936`, [sesudah 80×24](assets/ux/after-80.png), dan [sesudah 120×36](assets/ux/after-120.png). [Rekaman pembukaan prompt 120×36](assets/ux/wide-piko.cast)
- Rekaman asciicast dari build revamp dalam mode demo untuk [cek kondisi](assets/ux/condition.cast), [PC terasa lambat](assets/ux/slow.cast), dan [cleanup sampai kembali dari review](assets/ux/cleanup.cast).
- Rekaman galeri terbaru memperagakan pertanyaan, pemeriksaan, preview, konfirmasi, eksekusi, dan verifikasi fixture. [GIF 120×36](assets/piko-demo.gif) · [Asciicast 120×36](assets/piko-demo.cast) · [Asciicast ASCII 80×24](assets/piko-compact.cast)

Review menggunakan skenario tugas dan PTY. Tidak ada peserta studi pengguna dan tidak ada mutasi perangkat dalam skenario.

```sh
go build ./cmd/whwmpc
python3 scripts/tui_pty.py ./whwmpc --width 120 --height 36
python3 scripts/tui_pty.py ./whwmpc --width 80 --height 24 --flow ssd
python3 scripts/tui_pty.py ./whwmpc --width 80 --height 24 --flow slow --observe
python3 scripts/tui_pty.py ./whwmpc --width 80 --height 24 --flow cleanup --cleanup-result
```

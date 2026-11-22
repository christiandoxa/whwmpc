# Alur TUI

Beranda menanyakan **Mau dibantu apa?** dan memilih **Cek kondisi PC** sebagai posisi awal. Cek kondisi memeriksa CPU/memori, ruang simpan, daya, SSD, dan perawatan storage. Pemeriksaan read-only berlangsung tanpa otentikasi. **PC terasa lambat** langsung mengukur sampel karena gejala awal belum mengubah pengukuran yang didukung. **Bersihkan ruang** membuka inventaris sebelum membuat rencana. Kemampuan lanjutan ada di **Lainnya…**.

Gunakan ↑/↓ untuk daftar, Enter untuk membuka pilihan, dan Esc untuk kembali. Pertanyaan bebas ada di halaman tersendiri melalui `/`. Kolom utama memakai maksimum 76 sel. Pada 120×36 Piko memakai margin kanan. Pada 80×24 wajah dan aksesori Piko berada di dekat judul. Ukuran minimum 48×18. Di bawah batas itu, layar meminta terminal diperbesar dan Esc serta `q` tetap berfungsi.

## Hasil dan cakupan

Ringkasan menampilkan kesimpulan, waktu sampel, cakupan berdasarkan ID pemeriksaan, angka yang relevan, hingga tiga temuan prioritas, dan satu pilihan utama. **Selesai** menunjukkan data berhasil dibaca. **Parsial** berarti hanya sebagian data yang tersedia. **Gagal** ialah galat pembacaan. **Belum tersedia** mencakup data tak diketahui, kedaluwarsa, izin, ketergantungan, atau fitur yang tidak didukung. **Belum dijalankan** berarti pemeriksaan diminta tetapi belum mulai. Jumlah temuan kritis selalu dihitung dari semua temuan. Pilihan utama membuka detail lengkap.

- Bar CPU memakai sampel terakhir. Jumlah, durasi, dan rentang memakai sampel yang benar-benar dikumpulkan. Bar RAM mengikuti provider. Tanda `≥` pada RAM macOS menandai batas bawah dari halaman bebas. Bar disk memakai persentase dari provider untuk filesystem utama.
- Suhu memakai Celsius dan sumber sensor. Drive SSD yang perlu perhatian tampil lebih dulu. Endurance menampilkan nilai mentah termasuk nilai di atas 100%. Detail menyimpan counter SMART utuh.
- TRIM menampilkan dukungan, jadwal atau kebijakan, dan bukti terakhir secara terpisah. Daya menunjukkan profil dan sumber aktual serta membedakan clock terukur, rata-rata, driver, dan batas konfigurasi.
- Cleanup menunjukkan estimasi dan cakupan sebelum review. Hasil memisahkan estimasi, data terhapus yang teramati, selisih ruang bebas, potensi discard, dan verifikasi. Halaman lanjutan menampilkan ukuran yang sesuai dengan tugasnya, termasuk throughput benchmark, versi firmware, dan usia observasi terjadwal.
- `--ascii` mengganti karakter diagram dan `NO_COLOR` memberi tema terminal. Mode gerak mengatur Piko dan aksen tampilan. Angka tetap mengikuti snapshot saat Piko bergerak.

Detail mengelompokkan observasi, makna, saran, sumber, waktu, dan keterbatasan per temuan. Esc mengembalikan hasil dan pilihan sebelumnya. Enter membuka pilihan lanjutan. **Simpan hasil ini** memakai snapshot yang sama, sedangkan **Buat laporan lengkap** membaca data tambahan.

## Review dan hasil aksi

Satu layar review menampilkan target, kondisi sebelum dan sesudah, dampak, risiko, izin, dan kemungkinan pemulihan. **Kembali** menjadi pilihan awal. Rencana panjang perlu dibaca dengan gulir sebelum opsi tindakan bisa digunakan. Resize mempertahankan pilihan dan mengharuskan peninjauan kembali bila pembungkusan berubah. Selama aksi, Esc meminta penghentian dan hasil verifikasi tetap ditunggu. Demo cleanup mengunci Apply.

## Bukti dari PTY

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
python3 scripts/tui_pty.py ./whwmpc --width 80 --height 24 --flow slow
python3 scripts/tui_pty.py ./whwmpc --width 80 --height 24 --flow cleanup
```

# Alur TUI

Beranda menanyakan **Mau dibantu apa?** dan memilih **Cek kondisi PC** sebagai posisi awal. Cek kondisi memeriksa CPU/memori, ruang simpan, daya, SSD, dan perawatan storage. Pemeriksaan read-only berlangsung tanpa otentikasi. **PC terasa lambat** langsung mengukur sampel karena gejala awal belum mengubah pengukuran yang didukung. **Bersihkan ruang** membuka inventaris sebelum membuat rencana. Kemampuan lanjutan ada di **Lainnya…**.

Gunakan ↑/↓ untuk daftar, Enter untuk membuka pilihan, dan Esc untuk kembali. Pertanyaan bebas ada di halaman tersendiri melalui `/`. Kolom utama memakai maksimum 76 sel. Pada 120×36 Piko memakai margin kanan. Pada 80×24 wajah dan aksesori Piko berada di dekat judul.

## Hasil dan cakupan

Ringkasan menampilkan kesimpulan, cakupan berdasarkan ID pemeriksaan, hingga tiga temuan, dan satu pilihan utama. Cakupan **selesai** berarti sumber yang diminta memberi data. **Parsial** berarti hanya sebagian data yang tersedia. **Gagal** ialah galat pembacaan. **Belum tersedia** mencakup unknown, stale, izin, ketergantungan, atau fitur yang tidak didukung. **Belum dijalankan** berarti pemeriksaan diminta tetapi belum mulai. Jumlah temuan kritis dihitung sebelum batas tampilan; pilihan utamanya membuka detail lengkap.

Detail mengelompokkan observasi, makna, saran, sumber, waktu, dan keterbatasan per temuan. Esc mengembalikan hasil dan pilihan sebelumnya. Enter membuka pilihan lanjutan. **Simpan hasil ini** memakai snapshot yang sama, sedangkan **Buat laporan lengkap** membaca data tambahan.

## Review dan hasil aksi

Satu layar review menampilkan target, kondisi sebelum dan sesudah, dampak, risiko, izin, dan kemungkinan pemulihan. **Kembali** menjadi pilihan awal. Rencana panjang perlu dibaca dengan gulir sebelum opsi tindakan bisa digunakan. Resize mempertahankan pilihan dan mengharuskan peninjauan kembali bila pembungkusan berubah. Selama aksi, Esc meminta penghentian dan hasil verifikasi tetap ditunggu. Demo cleanup mengunci Apply.

## Bukti dari PTY

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

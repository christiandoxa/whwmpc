# Piko dan gerak

`whwmpc demo animations` membuka galeri fixture. Gunakan kiri/kanan dan Enter untuk memilih pose atau menelusuri skenario langkah demi langkah. `/` mencari nama pose atau skenario, `m` mengganti mode, `n` memperagakan layout ringkas, dan Esc kembali ke daftar. Galeri tidak menjalankan probe maupun menyimpan preferensi.

`--motion full|reduced|off` berlaku untuk sesi. `--no-animations` setara dengan `--motion off`. Ubah mode dari Pengaturan atau dengan `m` untuk menyimpan preferensi lokal. Mode reduced memakai pose sederhana dan tidak mengulang gerakan dekoratif. Mode off meniadakan penjadwalan motion. NO_COLOR mengatur palet secara independen. Gunakan `--ascii` untuk pose dan bingkai karakter dasar.

## Siklus gerak

Satu timer dengan deadline yang dapat diganti memberi pesan Bubble Tea bertanda generasi. Pesan lama dibuang. Frame dihitung dari waktu yang sudah berlalu, sehingga input dan status kerja dapat menggantikan transisi yang tertinggal. Refresh sensor memiliki pesan dan jadwal sendiri.

| Detail | Durasi |
| --- | --- |
| Highlight fokus | 130 ms |
| Reveal panel | 190 ms |
| Animasi sel Piko | 110 ms per frame, sekitar 9 fps |
| Gestur singkat | Umumnya 3–6 frame |
| Kedipan idle | Setelah 9 detik idle, satu kedipan |
| Frame kerja berulang | Selama status kerja masih aktif dan tampilan terlihat |

Layout lebar memakai kanvas 16×6. Layout ringkas memakai wajah mini di header. Pada terminal yang terlalu kecil, label Piko dan petunjuk resize tetap tersedia. Angka metrik dan teks hasil tampil utuh ketika data tiba.

Tambahkan pose di `internal/tui/mascot/piko.go` dengan deskripsi mata, mulut, lengan, lantai, dan aksesori. Daftarkan di galeri agar tes memeriksa lebar sel ASCII dan Unicode. Hubungkan pose dengan aktivitas atau hasil yang benar di `internal/tui/presentation.go`. Gunakan status terverifikasi untuk gestur berhasil.

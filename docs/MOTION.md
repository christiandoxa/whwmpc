# Piko dan gerak

`whwmpc demo animations` membuka galeri fixture. Gunakan atas/bawah dan Enter untuk memilih pose atau menelusuri skenario langkah demi langkah. `m` mengganti mode, `n` memperagakan layout ringkas, dan Esc kembali ke konteks sebelumnya. Galeri tidak menjalankan probe maupun menyimpan preferensi.

`--motion full|reduced|off` berlaku untuk sesi. `--no-animations` setara dengan `--motion off`. Ubah mode dari Pengaturan untuk menyimpan preferensi lokal. Tombol `m` mencoba mode lain di galeri. Mode reduced memakai pose sederhana dan tidak mengulang gerakan dekoratif. Mode off meniadakan penjadwalan motion. NO_COLOR mengatur palet secara independen. Gunakan `--ascii` untuk pose dan bingkai karakter dasar.

## Siklus gerak

Satu timer dengan deadline yang dapat diganti memberi pesan Bubble Tea bertanda generasi. Pesan lama dibuang. Frame dihitung dari waktu yang sudah berlalu, sehingga input dan status kerja dapat menggantikan transisi yang tertinggal. Pemeriksaan pengguna memiliki pesan tersendiri.

| Detail | Durasi |
| --- | --- |
| Highlight fokus | 130 ms |
| Reveal panel | 190 ms |
| Animasi sel Piko | 110 ms per frame, sekitar 9 fps |
| Gestur singkat | Umumnya 3–6 frame |
| Kedipan idle | Setelah 9 detik idle, satu kedipan |
| Frame kerja berulang | Selama status kerja masih aktif dan tampilan terlihat |

Semua layout memakai wajah mini Piko dekat label tugas. Pada terminal yang terlalu kecil, label Piko dan petunjuk resize tetap tersedia. Angka metrik dan teks hasil tampil utuh ketika data tiba.

Tambahkan pose di `internal/tui/mascot/piko.go` dengan deskripsi mata, mulut, lengan, lantai, dan aksesori. Daftarkan di galeri agar tes memeriksa lebar sel ASCII dan Unicode. Hubungkan pose dengan aktivitas atau hasil yang benar di `internal/tui/presentation.go`. Gunakan status terverifikasi untuk gestur berhasil.

## Peta interaksi

| Interaksi | Feedback |
| --- | --- |
| Startup dan keluar | Layar Piko menyala, mata terbuka, lalu melambai. Judul dan menu aktif sejak frame pertama. Keluar segera memulihkan terminal. |
| Menu, fokus, input, dan kirim | Aksen pilihan dan lirikan mengikuti seleksi. Input memakai pose mendengarkan tanpa mengulang animasi per huruf. Pertanyaan masuk ke halaman pemeriksaan atau klarifikasi. |
| Detail bukti, bantuan, dan resize | Aksen panel singkat dengan dimensi yang sama. Resize menghitung ulang layout dan menghentikan frame saat area terlalu kecil. |
| Overview dan pemindaian ulang | Kaca pembesar dan indikator indeterminate. Label menyatakan pemeriksaan atau sumber bacaan. Progres bertahap berasal dari collector. |
| SSD, CPU, baterai, dan TRIM | Stetoskop dan drive, kipas CPU, baterai dengan pose pengisian berdasarkan status AC dan Charging, serta checklist blok untuk audit baca-saja. |
| Cleanup dan preview | Sapu diam saat inventaris ditinjau. Clipboard untuk preview dan pose statis saat konfirmasi. Risiko dan pilihan aksi tetap di posisi yang sama. |
| Revalidasi, apply, cleanup, dan restore | Aktivitas backend memicu kaca pembesar, roda gigi, sapu dengan partikel lokal, atau rewind. Verifikasi memakai label dan pose pemeriksaan tersendiri. |
| Hasil | Jempol dan kilau lokal saat sukses terverifikasi. Profil performance diberi roket singkat, balanced pose tenang, power-saver daun. Label tetap menyebut profil aktual. |
| Data belum tersedia, warning, error, dan hardware kritis | Ekspresi tanya/angkat bahu, perhatian, atau bingung. Temuan kritis menampilkan saran yang diprioritaskan dan penanda header yang bertahan saat membuka hasil lain. |
| Penghentian | Piko menutup clipboard dan label menyatakan penghentian diminta. Hasil backend tetap tampil setelah pemeriksaan lanjutan, termasuk perubahan teramati atau hasil belum diketahui. |
| Riwayat, simpan preferensi, dan laporan | Label pembacaan/pencatatan, notifikasi mode, serta kertas printer. Lokasi laporan tampil setelah file selesai ditulis. |

Galeri memuat skenario normal, peringatan, kritis, unknown, pembatalan, dan hasil parsial. Transisi skenario mengikuti Enter untuk memudahkan review setiap keadaan. Jalur aksi nyata mengikuti pesan backend tanpa durasi tunggu buatan.

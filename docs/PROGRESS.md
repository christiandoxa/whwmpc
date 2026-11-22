# Progres

## 1 · Fondasi

- Repo lokal dan remote terverifikasi kosong pada awal kerja. Target `christiandoxa/whwmpc`, branch `main`.
- Pin Go dan Charm v2, tambah executable, deteksi TTY, runner subprocess terbatasi, serta CI.
- Diverifikasi di Linux amd64 dengan gofmt, vet, unit test, race test, dan build. Layar awal dan versi tersedia.
- Berikutnya asisten, overview Linux, serta demo.

## 2 · Asisten dan overview

- Dashboard Bubble Tea dengan panel adaptif, kartu CPU/RAM/disk/suhu, menu, input dua bahasa, klarifikasi intent, bantuan keyboard, serta demo deterministik.
- Linux membaca procfs/sysfs dan kapasitas filesystem lokal. CPU/memori memakai tiga interval. Ringkasan proses membaca nama dan RSS tanpa argumen, dengan nama dan mountpoint dikecualikan dari JSON.
- Windows/macOS menyediakan identitas runtime dan jumlah CPU logis. Data lain berstatus unsupported.
- Gate lokal mencakup gofmt, vet, test, race, build, dan fixture untuk sampling, pergantian PID, input fokus, serta layout.
- Berikutnya motion Piko dan integrasi collector SMART, TRIM, CPUFreq, dan daya.

## 2a · Motion dan galeri Piko

- Piko memakai pose terminal berukuran tetap, timer satu deadline, ekspresi fokus/input, indikator scan, serta aksen panel tanpa menggeser layout.
- Tiga mode gerak, alias off, preferensi lokal, ASCII, dan galeri dengan enam skenario fixture. Tidak ada aksi host di galeri.
- Gate lokal mencakup gofmt, vet, test, race, build, ukuran sel pose, generasi tick, resize, fokus, dan save preferensi berulang.
- Berikutnya hubungan status collector dan lifecycle aksi dengan pose yang sesuai.

## 3 · Collector Linux

- SMART JSON (termasuk exit mask, status per drive, dan counter NVMe lossless), pemetaan block-device dan mount, bukti TRIM per filesystem, timer dan jadwal, CPUFreq per policy, AC/baterai, serta audit pengelola profil daya.
- Integrasi menu SSD/TRIM/CPU-daya, `doctor`, `trim`, dan `power`. Cache SMART sesi dua menit dan pembacaan ulang manual.
- Probe baca-saja di Linux amd64 dengan smartctl 7.4, systemd 255, dan util-linux 2.39.3 menemukan SMART perlu izin, CPUFreq dan PPD tersedia, serta bukti timer/jurnal TRIM. Driver PPD yang terdeteksi ialah `fake`.
- Gate snapshot mencakup gofmt, vet, test, race, build, dan smoke CLI demo. Fixture mencakup parser dan kondisi yang tidak tersedia.
- Berikutnya temuan berbasis aturan, rekomendasi berdasarkan konteks, dan tindakan terikat preview.

## 4 · Temuan dan rekomendasi

- Aturan deterministik memisahkan ketersediaan, severity, bukti, interpretasi, batas, dan saran. Diagnosis lambat memakai beberapa sampel; SMART kritis mendahulukan backup. Temuan dan detail sumber tampil di asisten.
- Preferensi penggunaan bisa dipilih dengan keyboard atau input bebas, dipakai ulang, dan diubah di Rekomendasi. Demo memakai timestamp fixture dan preferensi sesi.
- Gate lokal mencakup gofmt, vet, test, race, build, serta fixture sehat, kritis, konflik, baterai/AC, unknown dan stale.
- Berikutnya preview tindakan, revalidasi, eksekusi dengan autentikasi OS, dan hasil terverifikasi.

## 5 · Tindakan dengan preview dan verifikasi

- Preview dan konfirmasi terpisah di TUI serta rencana-file/ID di CLI. HMAC, expiry, revalidasi target/argv, gate lintas proses, sudo melalui terminal, dan pemeriksaan hasil setelah interupsi.
- Profil PPD melalui GUID autentikasi terpin, TRIM manual melalui descriptor BusyBox, timer systemd vendor, dan cache APT melalui mekanisme native. Pemulihan profil/timer berangkat dari bukti perubahan terverifikasi.
- Piko memakai aktivitas aktual untuk revalidasi, eksekusi, verifikasi, pembatalan, dan hasil. Konfirmasi diblokir jika terminal terlalu kecil untuk menampilkan rencana.
- Fixture mencakup penggantian target, expiry, replay, sudo/permission, cancel/partial, dan restore. Preview baca-saja pada Linux amd64 untuk APT, profil daya, dan TRIM manual. Mutasi perangkat nyata tidak dijalankan.
- Berikutnya riwayat dan laporan untuk pengguna, distribusi, serta bukti visual final.

## 6 · Riwayat dan laporan

- Riwayat lokal berbatas 100 entri, ringkasan scan opt-in, status tindakan, dan pemilihan pemulihan dari bukti privat. Kunci dan penerbitan catatan diuji dengan penulis paralel.
- Ekspor JSON skema 1 serta Markdown memakai proyeksi redaksi yang sama. Piko menampilkan tahap penulisan dan lokasi hanya setelah file berhasil dipublikasikan. File yang sudah ada dipertahankan.
- TUI dan CLI mendukung laporan fixture yang tidak membaca history host. `doctor` membedakan tool TRIM, eligibility aksi manual, dan dukungan cache APT.
- Gate lokal mencakup gofmt, vet, test, race, build, smoke demo CLI, dan smoke PTY. Pemeriksaan dependency mengembalikan semua modul terverifikasi dan tanpa kerentanan yang terdeteksi oleh govulncheck.
- Berikutnya artefak PTY final, paket snapshot, dan audit dokumentasi.

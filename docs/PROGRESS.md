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

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

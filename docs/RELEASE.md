# Build dan rilis

Jalankan dari checkout lokal menggunakan versi Go yang tercantum di `go.mod`. Workflow dan skrip paket memakai Ubuntu dengan GNU tar, sha256sum, dan Python 3. Arsip berisi executable, README, dan pemberitahuan lisensi untuk Go serta dependensi yang masuk ke executable. Build lintas platform memakai `CGO_ENABLED=0` dan paket memakai urutan serta timestamp stabil.

```sh
bash scripts/release.sh snapshot
cd dist
sha256sum -c SHA256SUMS
tar -xzf whwmpc_snapshot_linux_amd64.tar.gz ./whwmpc
./whwmpc --version
```

| Target | Arsip | Executable |
| --- | --- | --- |
| Linux amd64/arm64 | `whwmpc_<versi>_linux_<arch>.tar.gz` | `whwmpc` |
| macOS arm64 | `whwmpc_<versi>_darwin_arm64.tar.gz` | `whwmpc` |
| Windows amd64 | `whwmpc_<versi>_windows_amd64.zip` | `whwmpc.exe` |

Arsip Linux mempertahankan nama dan struktur yang dipakai `install.sh`. Pada macOS, pakai `shasum -a 256 -c SHA256SUMS` untuk kumpulan arsip lengkap atau cocokkan hash arsip yang dipilih dengan barisnya di manifest. Pada Windows, gunakan `Get-FileHash -Algorithm SHA256` untuk membandingkan arsip dengan manifest, lalu ekstrak ZIP dan jalankan `.\whwmpc.exe` di PowerShell. Paket macOS berbentuk executable command-line hasil cross-build tanpa notarization. Dependency sistem opsional tetap mengikuti platform dan backend.

Untuk rilis stabil, tinjau hasil CI pada commit tujuan, buat tag beranotasi `vX.Y.Z`, lalu push tag tersebut. Workflow Release membangun paket dari commit yang ditag. Publikasi menunggu checksum dan smoke executable hasil paket pada keempat runner yang sesuai arsitektur. [Verifikasi tag di GitHub CLI](https://cli.github.com/manual/gh_release_create)

## Uji terminal dan rekaman

```sh
go build ./cmd/whwmpc
python3 scripts/tui_pty.py ./whwmpc --width 120 --height 36
python3 scripts/tui_pty.py ./whwmpc --record overview.cast --width 120 --height 36
python3 scripts/tui_pty.py ./whwmpc --record piko.cast --gallery --width 120 --height 36
agg --font-family "DejaVu Sans Mono" --font-size 16 overview.cast overview.gif
```

Uji menjalankan binari di PTY Linux. Ia memeriksa bantuan dan navigasi keyboard, fokus input, klarifikasi maksud, resize 120×36 ke 60×18 dan 80×24, serta pemulihan alternate screen dan atribut terminal. Mode rekam membaca keluaran aplikasi di demo, menggunakan direktori config/state sementara, dan menulis format asciicast v2. [Format asciicast v2](https://docs.asciinema.org/manual/asciicast/v2/), [renderer agg](https://github.com/asciinema/agg/releases/tag/v1.9.0)

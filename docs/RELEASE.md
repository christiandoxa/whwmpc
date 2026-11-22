# Build dan rilis

Jalankan dari checkout lokal menggunakan versi Go yang tercantum di `go.mod`. Workflow dan skrip paket memakai Ubuntu dengan GNU tar, GNU coreutils, dan Python 3. Arsip berisi executable, README, dan pemberitahuan lisensi untuk Go serta dependensi yang masuk ke executable. Build lintas platform memakai `CGO_ENABLED=0`. Arsip memakai urutan serta timestamp stabil, dengan mode `0644` untuk dokumen dan lisensi.

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
| macOS amd64/arm64 | `whwmpc_<versi>_darwin_<arch>.tar.gz` | `whwmpc` |
| Windows amd64/arm64 | `whwmpc_<versi>_windows_<arch>.zip` | `whwmpc.exe` |

## Pasang dari arsip

Unduh arsip platform dan `SHA256SUMS` dari rilis yang sama. Installer README mengotomatiskan langkah ini untuk Linux. [Halaman rilis](https://github.com/christiandoxa/whwmpc/releases)

Pada Linux atau macOS, ganti nama arsip sesuai unduhan. Gunakan direktori baru untuk ekstraksi.

```sh
archive=whwmpc_v0.3.6_darwin_arm64.tar.gz
shasum -a 256 "$archive"
# Cocokkan hash dengan SHA256SUMS sebelum lanjut.
tar -xzf "$archive" ./whwmpc
./whwmpc --version
./whwmpc
```

Pada Windows, jalankan di PowerShell.

```powershell
$archive = 'whwmpc_v0.3.6_windows_amd64.zip'
Get-FileHash -Algorithm SHA256 $archive
# Cocokkan hash dengan SHA256SUMS sebelum lanjut.
Expand-Archive $archive -DestinationPath .\whwmpc
.\whwmpc\whwmpc.exe --version
.\whwmpc\whwmpc.exe
```

Paket macOS berisi executable command-line tanpa notarization. Dependency sistem opsional mengikuti platform dan backend.

## Publikasi

Untuk rilis stabil, tinjau hasil CI pada commit tujuan, buat tag beranotasi `vX.Y.Z`, lalu push tag tersebut. Workflow Release membangun paket dari commit yang ditag. Publikasi menunggu checksum dan smoke executable hasil paket pada enam runner yang sesuai OS dan arsitektur. [Verifikasi tag di GitHub CLI](https://cli.github.com/manual/gh_release_create)

## Uji terminal dan rekaman

```sh
go build ./cmd/whwmpc
python3 scripts/tui_pty.py ./whwmpc --width 120 --height 36
python3 scripts/tui_pty.py ./whwmpc --record overview.cast --width 120 --height 36
python3 scripts/tui_pty.py ./whwmpc --record piko.cast --gallery --width 120 --height 36
agg --font-family "DejaVu Sans Mono" --font-size 16 overview.cast overview.gif
```

Uji menjalankan binari di PTY Linux. Ia memeriksa bantuan dan navigasi keyboard, fokus input, klarifikasi maksud, resize 120×36 ke 60×18 dan 80×24, serta pemulihan alternate screen dan atribut terminal. Mode rekam membaca keluaran aplikasi di demo, menggunakan direktori config/state sementara, dan menulis format asciicast v2. [Format asciicast v2](https://docs.asciinema.org/manual/asciicast/v2/), [renderer agg](https://github.com/asciinema/agg/releases/tag/v1.9.0)

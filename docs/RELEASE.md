# Build dan rilis

Jalankan dari checkout lokal menggunakan versi Go yang tercantum di `go.mod`. Workflow dan skrip paket memakai Ubuntu dengan GNU tar dan sha256sum. Arsip berisi `whwmpc`, README, dan pemberitahuan lisensi untuk Go serta dependensi yang masuk ke binari.

```sh
bash scripts/release.sh snapshot
cd dist
sha256sum -c SHA256SUMS
tar -xzf whwmpc_snapshot_linux_amd64.tar.gz ./whwmpc
./whwmpc --version
```

Pilih arsip `linux_arm64` untuk Linux arm64. Dependency sistem opsional tetap dipasang melalui package manager distro. macOS dan Windows diuji kompilasi.

Untuk rilis stabil, tinjau hasil CI pada commit tujuan, buat tag beranotasi `vX.Y.Z`, lalu push tag tersebut. Workflow Release menguji aplikasi, membangun ulang paket Linux amd64 dan arm64 dari tag, dan menerbitkan dua arsip beserta `SHA256SUMS`. [Verifikasi tag di GitHub CLI](https://cli.github.com/manual/gh_release_create)

## Uji terminal dan rekaman

```sh
go build ./cmd/whwmpc
python3 scripts/tui_pty.py ./whwmpc --width 120 --height 36
python3 scripts/tui_pty.py ./whwmpc --record overview.cast --width 120 --height 36
python3 scripts/tui_pty.py ./whwmpc --record piko.cast --gallery --width 120 --height 36
agg --font-family "DejaVu Sans Mono" --font-size 16 overview.cast overview.gif
```

Uji menjalankan binari di PTY Linux. Ia memeriksa bantuan dan navigasi keyboard, fokus input, klarifikasi maksud, resize 120×36 ke 60×18 dan 80×24, serta pemulihan alternate screen dan atribut terminal. Mode rekam membaca keluaran aplikasi di demo, menggunakan direktori config/state sementara, dan menulis format asciicast v2. [Format asciicast v2](https://docs.asciinema.org/manual/asciicast/v2/), [renderer agg](https://github.com/asciinema/agg/releases/tag/v1.9.0)

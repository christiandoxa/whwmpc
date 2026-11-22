# Pengembangan

Stack diverifikasi pada 22 November 2026 terhadap rilis stabil dan metadata modul. Gunakan Go **1.27.4**, versi minimum di `go.mod` dan versi tepat di CI. `GOTOOLCHAIN=local` di CI menjaga workflow memakai binari yang dipasang.

- [Go 1.27.4](https://go.dev/doc/devel/release#go1.27.4) untuk build dan standard library.
- [Bubble Tea v2.1.2](https://github.com/charmbracelet/bubbletea/tree/v2.1.2) untuk event loop, lifecycle terminal, dan layar alternatif. Model memakai `View() tea.View` serta `tea.KeyPressMsg` dari [panduan migrasi v2](https://github.com/charmbracelet/bubbletea/blob/v2.1.2/UPGRADE_GUIDE_V2.md).
- [Bubbles v2.2.2](https://github.com/charmbracelet/bubbles/tree/v2.2.2) untuk kontrol input dan navigasi.
- [Lip Gloss v2.1.1](https://github.com/charmbracelet/lipgloss/tree/v2.1.1) untuk layout dan tema. `x/term` memeriksa TTY sebelum membuka UI.

Desain mengikuti pola dashboard, preview terikat seleksi, history, dan distribusi arsip ber-checksum dari [Upkit](https://github.com/christiandoxa/upkit/tree/65c35e7b005e9d98097c80ae1dfcc0f3efde9ccc). Btop mengilhami ringkasan metrik, gh-dash panel detail, dan Lazygit navigasi keyboard. WHWMPC memakai susunan dan tampilan sendiri. [awesome-tuis](https://github.com/rothgar/awesome-tuis)

Baseline implementasi ialah Ubuntu 24.04 LTS. Deteksi capability dilakukan saat runtime dan ukuran dukungan dicatat berdasarkan bukti pengujian.

```sh
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/whwmpc
```

## Bukti pengujian

CI menjalankan format, vet, race detector, build, dan smoke PTY Bubble Tea v2 pada Ubuntu 24.04. Smoke memakai ukuran 120×36, 60×18, dan 80×24 untuk menguji navigasi tugas, fokus masukan, bantuan, resize, dan pemulihan mode terminal. Workflow terpisah menguji provider pada macOS 15 arm64 dan Windows Server 2025 amd64 serta pembersih cache pada kontainer Arch dan Fedora. Linux arm64 masuk matriks smoke paket saat rilis.

```sh
python3 scripts/tui_pty.py ./whwmpc --width 120 --height 36
go mod verify
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...
bash scripts/release.sh snapshot dist
```

Fixture dan fake runner meliputi parsing SMART, diagnosis, stale data, kondisi aksi, privacy report, serta preferensi/klaim lintas proses. Tes rutin tidak menjadwalkan TRIM, mengubah profil, atau membersihkan komputer. Probe native yang dicoba hanya membaca data dan membuat preview. Uji mutasi perangkat milik sendiri bersifat opt-in melalui preview/confirm normal, dengan backup dan kondisi yang bisa diperiksa kembali.

Aplikasi demo berjalan dalam PTY Linux. Event asciicast v2 dirender dengan agg v1.9.0 dan DejaVu Sans Mono. [Screenshot dan rekaman tiga alur](UX.md) · [Rekaman galeri Piko](assets/piko-demo.cast) · [Cara merekam ulang](RELEASE.md)

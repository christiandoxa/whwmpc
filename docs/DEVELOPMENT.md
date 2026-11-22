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

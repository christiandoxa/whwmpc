# Uji kinerja

Di TUI buka **Lainnya… → Uji kinerja** untuk memilih beban, melihat batasnya, dan memulai secara eksplisit. Demo mengunci workload. CLI mencetak hasil JSON, termasuk apakah pengujian selesai. Ringkasan dicatat ke riwayat lokal.

```sh
whwmpc benchmark cpu --duration 3s
whwmpc benchmark stress --duration 15s
whwmpc benchmark read --file ./sample.bin --mib 16
whwmpc benchmark write --dir ./test-output --mib 16
```

- **CPU** menjalankan SHA-256 serial pada blok 1 MiB dan memasukkan digest kembali ke awal blok berikutnya. Durasi maksimum lima detik dan satu worker.
- **Stress** menggunakan beban CPU yang sama dengan maksimum 30 detik. Sensor CPU harus terbaca sebelum mulai. Pembacaan sekitar satu kali per detik ditambah waktu probe. Batas penghentian 85 °C adalah kebijakan aplikasi yang konservatif. Kehilangan sensor atau pembatalan menghentikan beban.
- **Baca** memakai file reguler yang dipilih dan membaca maksimal 64 MiB melalui cache OS.
- **Tulis** memakai maksimum 64 MiB data acak pada file eksklusif di direktori pilihan. Pengukuran mencakup pembangkitan data, tulis, dan sinkronisasi. File sementara dihapus ketika perintah selesai atau dibatalkan. Tes I/O memakai timeout 30 detik, yang diperiksa antar blok. Gunakan filesystem lokal agar operasi kernel yang tertahan dapat selesai.

Hasil memuat OS, arsitektur, CPU logis, workload, durasi, byte, laju, dan data suhu bila ada. Bandingkan hanya mode, ukuran, dan kondisi yang sebanding. Cache OS, load lain, suhu, dan kecepatan pembangkitan data dapat memengaruhi hasil. Kode benchmark didokumentasikan melalui [Go SHA-256](https://pkg.go.dev/crypto/sha256) dan [API os.Root](https://pkg.go.dev/os#Root).

Tes memakai waktu palsu, file sementara, dan sensor fixture. Validasi stress pada perangkat asli tetap opt-in.

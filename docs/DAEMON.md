# Observasi terjadwal

Linux dengan systemd user session menyediakan timer dan service oneshot khusus pengguna. Pemasangan executable tidak membuat atau mengaktifkan unit. Jalankan lifecycle secara eksplisit sebagai pengguna biasa dari binari dengan path stabil.

```sh
whwmpc daemon install --interval 15m
whwmpc daemon enable
whwmpc daemon start
whwmpc daemon status
whwmpc daemon stop
whwmpc daemon disable
whwmpc daemon uninstall
```

Interval 5 menit hingga 24 jam. Timer mengulang setelah service berhenti. Cakupan observasi adalah CPU/memori, kapasitas disk, dan daya. Pemeriksaan dasar dimasukkan ke riwayat lokal berbatas 100 entri. Lock privat mencegah dua eksekusi observasi berjalan bersamaan. `daemon run` melakukan satu observasi untuk service dan memakai lock yang sama.

Unit membatasi CPU 20%, memori 192 MiB, 32 task, dan 90 detik. Stdout dibuang. Stderr masuk ke journal dengan burst maksimum 8 pesan per menit; retensi journal mengikuti systemd host. Service tidak menyediakan IPC atau maintenance terjadwal dan tidak menaikkan privilege.

Test lifecycle memakai direktori sementara dan manager fixture. Pengujian pada user session yang nyata perlu persetujuan untuk memasang dan mengaktifkan unit. [Timer](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html) · [Service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html) · [Batas resource](https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html)

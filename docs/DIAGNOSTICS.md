# Aturan diagnosis

whwmpc menghubungkan pengamatan lokal dengan interpretasi, tingkat perhatian, dan saran. Temuan memuat ID aturan, kategori, ketersediaan, waktu pengamatan, sumber, penjelasan, dan batas kepastian. Temuan kritis tampil lebih dahulu. Satu pemeriksaan bisa menghasilkan peringatan dari data yang terbaca sebagian.

## Overview dan penyebab lambat

| ID temuan | Aturan | Sumber |
| --- | --- | --- |
| `overview.cpu` | Waktu CPU dihitung dari pertambahan counter `/proc/stat`. Peringatan muncul ketika semua sampel mencapai ≥85% dan ada sedikitnya tiga sampel. Angka gabungan dapat menyembunyikan beban pada satu inti. | [Linux procfs](https://docs.kernel.org/filesystems/proc.html) |
| `overview.memory` | Sedikitnya tiga pembacaan dengan penggunaan RAM ≥90% menunjukkan ruang rendah selama jendela sampel. Estimasi penggunaan memakai `MemAvailable`. Pemakaian swap dicatat sebagai isi swap dan laju paging perlu pengukuran terpisah. | [Linux meminfo](https://docs.kernel.org/filesystems/proc.html#meminfo) |
| `overview.disk.*` | Penggunaan kapasitas ≥90% menghasilkan peringatan dan ≥97% menghasilkan temuan kritis. `Bavail` menunjukkan ruang untuk proses tanpa privilege. Latensi I/O perlu pengukuran tersendiri. | [statfs(2)](https://man7.org/linux/man-pages/man2/statfs.2.html), [mountinfo(5)](https://man7.org/linux/man-pages/man5/proc_pid_mountinfo.5.html) |
| `overview.temperature` | Pembacaan sensor CPU tertinggi ≥90 °C menyarankan pemeriksaan ventilasi dan sampel ulang. Batas suhu dan throttling ditentukan oleh perangkat. | [Linux hwmon](https://docs.kernel.org/hwmon/sysfs-interface.html), [CPUFreq](https://docs.kernel.org/admin-guide/pm/cpufreq.html) |
| `overview.process` | Proses teratas diberi perhatian jika memakai ≥40% kapasitas CPU host dan syarat beban CPU berulang terpenuhi. Temuan memakai nomor urut. Detail lokal mengaitkan nomor dengan proses yang dapat dibaca. | [Linux procfs](https://docs.kernel.org/filesystems/proc.html) |
| `overview.power` | AC dan persentase baterai dicatat jika terlihat. Durasi dan kapasitas adaptor perlu pengukuran terpisah. | [Linux power_supply](https://docs.kernel.org/power/power_supply_class.html) |

Collector Linux mengambil tiga interval sekitar 260 ms untuk CPU dan RAM. Aturan menilai jendela yang terbaca. Ambang aplikasi memandu triase singkat saat gejala terjadi.

## SMART dan TRIM

| ID temuan | Aturan | Sumber |
| --- | --- | --- |
| `smart.coverage`, `smart.status.*` | Status SMART gagal, bit exit 3 atau 4, critical warning NVMe, dan nilai spare valid di bawah ambang menghasilkan temuan kritis dengan prioritas backup. Rekaman error log, self-test, ambang historis, atau kesalahan media menghasilkan peringatan. Bit kegagalan pembacaan memengaruhi ketersediaan. | [smartctl(8)](https://github.com/smartmontools/smartmontools/blob/main/src/smartctl.8.in), [NVMe SMART log](https://github.com/linux-nvme/libnvme/blob/master/src/nvme/types.h) |
| `smart.endurance.*`, `smart.counters.*` | Percentage Used adalah estimasi endurance vendor dan dipertahankan saat melewati 100%. Pada ≥100% whwmpc menyarankan pemantauan dan rencana penggantian sesuai penggunaan. Counter NVMe disimpan dalam desimal eksak. Unit baca/tulis mewakili kelipatan 512.000 byte dan dibulatkan oleh perangkat. Atribut ATA mengikuti vendor. | [smartctl JSON dan atribut ATA](https://github.com/smartmontools/smartmontools/blob/main/src/smartctl.8.in), [NVMe SMART log](https://github.com/linux-nvme/libnvme/blob/master/src/nvme/types.h) |
| `trim.scheduler`, `trim.coverage` | Enablement, aktivitas timer, jadwal berikutnya, pemicu timer terakhir, dan hasil service dicatat terpisah. Eksekusi service bisa berasal dari timer atau pemicu manual. Cron terdeteksi sebagai capability ketika instalasinya terlihat. | [systemd.timer](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html), [fstrim(8)](https://github.com/util-linux/util-linux/blob/master/sys-utils/fstrim.8.adoc) |
| `trim.mount.*` | Bukti discard mengikuti queue dari perangkat, partisi, dan lapisan block yang bisa dipetakan. Program meninjau ext4, XFS, VFAT, dan root filesystem Btrfs dengan seluruh perangkat terpetakan. Catatan jurnal dihubungkan melalui nama mount dan perangkat pada invocation service yang terlihat. Jumlah byte jurnal adalah potensi pengajuan discard. Ruang tersedia diukur oleh `statfs`. | [Linux block queue](https://docs.kernel.org/5.15/block/queue-sysfs.html), [Btrfs sysfs](https://btrfs.readthedocs.io/en/stable/btrfs-man5.html#sysfs-interface), [fstrim(8)](https://github.com/util-linux/util-linux/blob/master/sys-utils/fstrim.8.adoc) |

## CPUFreq dan pilihan profil

| ID temuan | Aturan | Sumber |
| --- | --- | --- |
| `power.cpufreq.*` | Frekuensi hardware, nilai driver, rentang konfigurasi, rentang hardware, dan batas firmware ditampilkan terpisah. Batas konfigurasi atau firmware ≤90% maksimum hardware menjadi alasan untuk memeriksa profil dan membandingkan beban. Frekuensi idle mengikuti driver dan governor. | [CPUFreq Linux](https://docs.kernel.org/admin-guide/pm/cpufreq.html) |
| `power.supply`, `power.profiles`, `power.manager-conflict` | Sumber daya, baterai, platform profile, EPB, profil aktif, hold, dan alasan degraded atau inhibited berasal dari antarmuka masing-masing. Konflik pengelola mengarahkan pengguna untuk meninjau layanan aktif. | [Linux power_supply](https://docs.kernel.org/power/power_supply_class.html), [PowerProfiles D-Bus API](https://upower.pages.freedesktop.org/power-profiles-daemon/gdbus-org.freedesktop.UPower.PowerProfiles.html), [TLP dan konflik PPD](https://linrunner.de/tlp/faq/ppd.html) |
| `recommend.context`, `recommend.profile`, `recommend.conflict` | Balanced menjadi pilihan awal untuk penggunaan umum. Power-saver diprioritaskan untuk baterai ≤20% saat memakai baterai, suhu CPU ≥85 °C, dan preferensi daya, suhu, atau kebisingan. Performance dipertimbangkan untuk build berat dan gaming dengan AC, suhu terbaca di bawah 85 °C, serta profil yang tersedia dan backend tanpa pembatas aktif yang diketahui. Kinerja perlu dibandingkan pada pekerjaan dan kondisi yang sama. | [CPUFreq Linux](https://docs.kernel.org/admin-guide/pm/cpufreq.html), [PowerProfiles D-Bus API](https://upower.pages.freedesktop.org/power-profiles-daemon/gdbus-org.freedesktop.UPower.PowerProfiles.html) |

## Umur dan cakupan sampel

Overview dianggap kedaluwarsa setelah 30 detik dan audit CPU/daya setelah satu menit. Audit TRIM memakai ambang 15 menit. Audit SMART memakai ambang 30 menit. Waktu eksekusi yang berasal dari jurnal tetap muncul sebagai riwayat, sementara ketersediaan audit mengikuti waktu pemindaian. Nilai tak diketahui, keterbatasan platform, kebutuhan izin, dependency hilang, dan kegagalan pembacaan dipertahankan sebagai status.

Temuan yang diekspor memakai nomor urut drive, proses, dan filesystem. Target numerik TRIM mengacu pada ID mount yang perlu dicocokkan dengan pemeriksaan aktif. Opsi tindakan membuka preview melalui planner. Executor memeriksa ulang dan memverifikasi tindakan yang telah dikonfirmasi.

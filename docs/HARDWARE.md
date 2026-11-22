# GPU dan kontrol perangkat keras

Matriks ini membedakan akses metrik, operasi yang diimplementasikan, dan validasi yang sudah dilakukan. Baseline implementasi adalah Linux amd64/arm64 dengan perangkat PCI dan antarmuka vendor yang memenuhi prasyarat di bawah.

| Backend atau keluarga | Bacaan dan operasi | Validasi |
| --- | --- | --- |
| NVIDIA PCI dengan driver proprietary dan `nvidia-smi` | Identitas driver/model, penggunaan GPU, suhu, VRAM, daya board, clock, batas daya yang diminta dan yang ditegakkan. Perubahan batas daya GPU fisik tersedia bila backend melaporkan nilai, default, minimum, dan maksimum yang konsisten. | Fixture CSV dan batas. Probe dan preview baca-saja RTX 3060 dengan driver 615.71.09 pada Linux amd64. Eksperimen perubahan fisik perlu persetujuan. |
| AMD PCI dengan `amdgpu` pada kernel yang menyediakan sysfs GPU | Penggunaan GPU, suhu on-die, VRAM, daya SoC, dan clock. Untuk APU, daya SoC mencakup CPU. Perubahan batas maksimum clock memakai `OD_SCLK` dan rentang `SCLK` pada backend yang memvalidasi slot maksimum dari batas tersebut, termasuk Renoir, Yellow Carp, dan backend SMU 14.0.0. | Fixture sysfs, urutan apply/restore, dan penggantian kondisi. Probe serta preview baca-saja Renoir 0x1636 pada Linux amd64 kernel 7.0.0. |
| AMD SMU 13.0.0 dan 13.0.7 dengan fitur voltage-offset yang diaktifkan firmware | Offset negatif dari kurva voltase default atau kembali ke nol. Driver harus melaporkan `OD_VDDGFX_OFFSET` sekaligus minimum/maksimum `VDDGFX_OFFSET`. | Semantik diverifikasi pada kode kernel v6.8 dan fixture. APU Renoir host pengembangan tidak melaporkan offset atau batasnya. Validasi perangkat memerlukan GPU yang melaporkan bidang tersebut. |
| Intel, Nouveau, GPU lain, dan perangkat dengan metrik terbatas | Identitas PCI dan driver yang ditemukan. Sensor atau tuning yang tidak dapat dibaca tetap memiliki status ketersediaan tersendiri. | Fixture inventaris dan dependency yang hilang. |
| macOS dan Windows | Adapter tuning GPU ini melaporkan unsupported. | Compile-only untuk adapter. |

Metrik yang tidak tersedia dibiarkan kosong. Tuning memerlukan pengguna memilih GPU, kontrol, dan nilai eksplisit. Backend menolak nilai di luar batas driver atau keadaan yang berubah sejak preview. Batas clock dalam MHz merupakan maksimum yang diminta. Pencapaian frekuensi aktual tergantung beban, energi, suhu, dan firmware. Planner NVIDIA menerima bilangan bulat mW dan mengirim watt ke driver. Nilai harus sesuai resolusi bacaan yang dilaporkan `nvidia-smi`, misalnya kelipatan 10 mW ketika CSV menampilkan dua desimal watt. Verifikasi dan restore memakai batas yang diminta pada resolusi tersebut. Batas yang diberlakukan hardware dilaporkan sebagai metrik tersendiri. [Dokumentasi nvidia-smi](https://docs.nvidia.com/deploy/nvidia-smi/index.html)

Detail GPU di TUI menampilkan daya papan NVIDIA dan daya SoC AMD sesuai asal pembacaan. Pada NVIDIA, CLI dan TUI membedakan batas daya yang diminta dari batas yang ditegakkan driver ketika keduanya tersedia.

AMD OverDrive memerlukan kedua atribut driver yang dapat ditulis oleh pemilik. Backend menerima mode awal auto atau manual, beralih ke manual bila diperlukan, menulis parameter tertentu, lalu meminta commit dari driver. Rencana mengikat alamat PCI, identitas atribut, mode, nilai awal, batas driver, dan konten tabel yang teramati. Penulisan pada sysfs memakai descriptor target yang terikat ke inode yang sudah diperiksa. Kegagalan commit atau penghentian sesudah pergantian mode dapat meninggalkan perubahan parsial. Pengaturan berlaku pada sesi driver, dan reset atau driver unload dapat mengubahnya. Gunakan penalaan tanpa proses lain yang mengedit OverDrive secara bersamaan. Sysfs dapat menampilkan tabel staging dan tidak menyediakan transaksi atomik lintas proses. [Linux AMDGPU thermal controls v6.8](https://docs.kernel.org/6.8/gpu/amdgpu/thermal.html) · [Implementasi SMU 13.0.0](https://github.com/torvalds/linux/blob/v6.8/drivers/gpu/drm/amd/pm/swsmu/smu13/smu_v13_0_0_ppt.c) · [Implementasi SMU 13.0.7](https://github.com/torvalds/linux/blob/v6.8/drivers/gpu/drm/amd/pm/swsmu/smu13/smu_v13_0_7_ppt.c)

Pada SMU 13.0.0 dan 13.0.7, `OD_RANGE SCLK` menampilkan minimum slot Fmin. Setter `s 1` memeriksa minimum slot Fmax yang terpisah. whwmpc menggunakan identitas MP1 untuk mengenali backend dengan batas tekstual yang bisa dipakai untuk slot maksimum. Ketika rentang Fmax belum terverifikasi, inventaris menjelaskan alasannya dan preview baru berhenti sebelum otorisasi. Pemulihan terdahulu memakai nilai awal yang tercatat persis pada receipt terverifikasi. Pembacaan `pp_table` sebagai sumber batas Fmax masih memerlukan validasi layout pada perangkat SMU 13 fisik. [Pemetaan MP1](https://github.com/torvalds/linux/blob/v7.0/drivers/gpu/drm/amd/pm/swsmu/amdgpu_smu.c#L720-L789) · [Rentang yang ditampilkan](https://github.com/torvalds/linux/blob/v7.0/drivers/gpu/drm/amd/pm/swsmu/smu13/smu_v13_0_7_ppt.c#L1449-L1459) · [Validasi slot Fmax](https://github.com/torvalds/linux/blob/v7.0/drivers/gpu/drm/amd/pm/swsmu/smu13/smu_v13_0_7_ppt.c#L1613-L1625)

Aksi memakai konfirmasi, klaim satu kali, dan autentikasi OS yang sama dengan tindakan lain. Restore memerlukan hasil apply yang sudah terverifikasi, perangkat yang sama, dan kesamaan state saat ini dengan identitas hasil sebelumnya. Restore mengembalikan nilai awal dan mode DPM yang disimpan. Jika kondisi berubah atau operasi terhenti tanpa kepastian, baca ulang konfigurasi sebelum memilih rencana lain.

Uji end-to-end CLI NVIDIA dari `c79c5c9` menjalankan preview, apply, pembacaan balik, dan restore pada backend fixture dan sudo sintetis dalam user namespace privat. Rencana usang, penolakan autentikasi, replay, dan perubahan daya eksternal ditolak sebelum penulisan. Batas yang diminta kembali ke 170 W setelah apply ke 169 W, dengan batas yang ditegakkan fixture terbaca terpisah. [Hasil uji CLI](assets/nvidia-cli-fixture.txt)

Uji TUI pada 80×24 dan 120×36 melewati pilihan awal Kembali, konfirmasi eksplisit, autentikasi fixture, apply 169 W, dan restore 170 W. Riwayat mempertahankan kedua hasil. Rencana pemulihan tersisa ditolak setelah restore terverifikasi, termasuk ketika fixture kembali ke nilai yang diharapkan rencana lama. [Hasil uji TUI dan replay restore](assets/nvidia-tui-fixture.txt)

```sh
go test -race ./internal/hardware ./internal/actions
```

## Persiapan validasi perangkat

Perintah baca-saja berikut sudah dicoba pada perangkat pengembangan. Pembacaan pertama menampilkan identitas, nilai saat ini, dan batas yang tersedia. Jalankan dua preview hanya pada perangkat dan rentang yang cocok.

```sh
whwmpc gpu --json
whwmpc gpu --device 0000:01:00.0 --plan gpu-power-limit --value 169000 --json
whwmpc gpu --device 0000:08:00.0 --plan gpu-core-max --value 1899 --json
```

Apply dan restore fisik memerlukan persetujuan atas nilai, perangkat, dan rencana tertentu. Cocokkan hasil terverifikasi dengan pembacaan driver, lalu tinjau pemulihan ke 170 W pada RTX 3060 atau 1900 MHz dan mode auto pada Renoir. Jalur restore memeriksa identitas state pasca-apply sebelum menulis. Renoir menggerakkan layar HDMI aktif. Untuk uji undervolt, mulai dari GPU yang melaporkan `gpu-voltage-offset` beserta batasnya. [Rencana, history, dan restore](ACTIONS.md)

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

## 2a · Motion dan galeri Piko

- Piko memakai pose terminal berukuran tetap, timer satu deadline, ekspresi fokus/input, indikator scan, serta aksen panel tanpa menggeser layout.
- Tiga mode gerak, alias off, preferensi lokal, ASCII, dan galeri dengan enam skenario fixture. Tidak ada aksi host di galeri.
- Gate lokal mencakup gofmt, vet, test, race, build, ukuran sel pose, generasi tick, resize, fokus, dan save preferensi berulang.
- Berikutnya hubungan status collector dan lifecycle aksi dengan pose yang sesuai.

## 3 · Collector Linux

- SMART JSON (termasuk exit mask, status per drive, dan counter NVMe lossless), pemetaan block-device dan mount, bukti TRIM per filesystem, timer dan jadwal, CPUFreq per policy, AC/baterai, serta audit pengelola profil daya.
- Integrasi menu SSD/TRIM/CPU-daya, `doctor`, `trim`, dan `power`. Cache SMART sesi dua menit dan pembacaan ulang manual.
- Probe baca-saja di Linux amd64 dengan smartctl 7.4, systemd 255, dan util-linux 2.39.3 menemukan SMART perlu izin, CPUFreq dan PPD tersedia, serta bukti timer/jurnal TRIM. Driver PPD yang terdeteksi ialah `fake`.
- Gate snapshot mencakup gofmt, vet, test, race, build, dan smoke CLI demo. Fixture mencakup parser dan kondisi yang tidak tersedia.
- Berikutnya temuan berbasis aturan, rekomendasi berdasarkan konteks, dan tindakan terikat preview.

## 4 · Temuan dan rekomendasi

- Aturan deterministik memisahkan ketersediaan, severity, bukti, interpretasi, batas, dan saran. Diagnosis lambat memakai beberapa sampel; SMART kritis mendahulukan backup. Temuan dan detail sumber tampil di asisten.
- Preferensi penggunaan bisa dipilih dengan keyboard atau input bebas, dipakai ulang, dan diubah di Rekomendasi. Demo memakai timestamp fixture dan preferensi sesi.
- Gate lokal mencakup gofmt, vet, test, race, build, serta fixture sehat, kritis, konflik, baterai/AC, unknown dan stale.
- Berikutnya preview tindakan, revalidasi, eksekusi dengan autentikasi OS, dan hasil terverifikasi.

## 5 · Tindakan dengan preview dan verifikasi

- Preview dan konfirmasi terpisah di TUI serta rencana-file/ID di CLI. HMAC, expiry, revalidasi target/argv, gate lintas proses, sudo melalui terminal, dan pemeriksaan hasil setelah interupsi.
- Profil PPD melalui GUID autentikasi terpin, TRIM manual melalui descriptor BusyBox, timer systemd vendor, dan cache APT melalui mekanisme native. Pemulihan profil/timer berangkat dari bukti perubahan terverifikasi.
- Piko memakai aktivitas aktual untuk revalidasi, eksekusi, verifikasi, pembatalan, dan hasil. Konfirmasi diblokir jika terminal terlalu kecil untuk menampilkan rencana.
- Fixture mencakup penggantian target, expiry, replay, sudo/permission, cancel/partial, dan restore. Preview baca-saja pada Linux amd64 untuk APT, profil daya, dan TRIM manual. Mutasi perangkat nyata tidak dijalankan.
- Berikutnya riwayat dan laporan untuk pengguna, distribusi, serta bukti visual final.

## 6 · Riwayat dan laporan

- Riwayat lokal berbatas 100 entri, ringkasan scan opt-in, status tindakan, dan pemilihan pemulihan dari bukti privat. Kunci dan penerbitan catatan diuji dengan penulis paralel.
- Ekspor JSON skema 1 serta Markdown memakai proyeksi redaksi yang sama. Piko menampilkan tahap penulisan dan lokasi hanya setelah file berhasil dipublikasikan. File yang sudah ada dipertahankan.
- TUI dan CLI mendukung laporan fixture yang tidak membaca history host. `doctor` membedakan tool TRIM, eligibility aksi manual, dan dukungan cache APT.
- Gate lokal mencakup gofmt, vet, test, race, build, smoke demo CLI, dan smoke PTY. Pemeriksaan dependency mengembalikan semua modul terverifikasi dan tanpa kerentanan yang terdeteksi oleh govulncheck.
- Berikutnya artefak PTY final, paket snapshot, dan audit dokumentasi.

## 7 · Distribusi dan review visual

- PTY aplikasi yang berjalan direview pada 120×36 dan 80×24. Navigasi, input, bantuan, resize, dan pemulihan terminal diuji dengan smoke PTY. Rekaman galeri memuat sesi fixture dari pertanyaan sampai hasil terverifikasi.
- Paket snapshot Linux amd64/arm64 dibangun dan checksum diperiksa. Binari amd64 hasil ekstraksi dipakai untuk smoke. macOS arm64 dan Windows amd64 diperiksa melalui kompilasi.
- Gate lokal menjalankan gofmt, vet, unit/integration test, race, build, PTY, verifikasi modul, dan govulncheck.
- Diagnosis native Linux amd64 baca-saja. SMART pada host pengembangan memerlukan izin, PPD memakai driver `fake`, dan sebagian inventaris APT privat. Preview manual TRIM terikat descriptor terverifikasi. Perubahan daya, TRIM, dan cleanup pada perangkat nyata masih memerlukan pengujian opt-in.
- Workflow tag siap. Paket yang diperiksa adalah snapshot lokal.

Checkpoint awal di `main` mencakup `db4f687` (fondasi), `01f01ca` (asisten), `a5f0342` (motion), `854f9b3` (collector), `675c36b` (diagnostik), `8a478ab` (tindakan), dan `038667f` (laporan).

Audit akhir menguji pembatas subprocess dengan keluaran yang melampaui kuota pada stdout dan stderr. Buffer penyimpanan memakai field bernama agar fast path `io.Copy` tidak melewati pembatas.

Pemeriksaan ulang overview melalui pilihan tindakan dan tombol `r` memakai jalur yang sama. Esc menghentikan scan dan mempertahankan snapshot terakhir. Diagnosis lambat mengambil sampel baru saat diminta ulang.

## Revamp UX 1 · Beranda dan navigasi

Beranda dengan tiga tujuan dan Lainnya menggantikan dashboard. Satu kolom, daftar vertikal, layar pertanyaan tersendiri, dan back stack menggantikan perpindahan fokus dan carousel. Job mulai setelah pengguna memilih pemeriksaan. Gate mencakup format, vet, tes model/race, build, dan smoke PTY.

## Revamp UX 2 · Ringkasan dan cakupan

Pemeriksaan kondisi memakai pengumpulan snapshot yang sama dengan laporan lengkap. Empat tahap pemeriksaan mencakup lima ID cakupan. Ringkasan memprioritaskan tiga temuan dan menghitung semua temuan kritis. Detail tersendiri menyimpan temuan lengkap, teknis, dan konteks saat kembali. Pemeriksaan lambat langsung mengukur bukti yang tersedia tanpa menanyakan konteks yang belum memengaruhi aturan.

## Revamp UX 3 · Review tindakan dan ekspor

Satu layar review memperlihatkan Kembali sebagai pilihan awal dan aksi spesifik sebagai pilihan kedua. Rencana panjang memerlukan pembacaan dengan gulir sebelum Apply tersedia. ID, expiry, revalidasi, klaim, dan verifikasi tetap memakai backend existing. Demo cleanup dapat ditinjau dan selalu terkunci. Ekspor snapshot memakai data yang sudah dikumpulkan, sedangkan laporan lengkap menyatakan pemeriksaan tambahan. Tes mencakup resize review, Enter berulang, pembatalan, dan ekspor dengan scanner nil.

## Revamp UX 4 · Bukti PTY dan inventaris backlog

Review sebelum/sesudah 80×24, beranda 120×36, dan rekaman tiga alur demo memakai binari yang dibangun dari sumber. Smoke PTY menelusuri pemeriksaan, detail, hasil, diagnosis lambat, serta pembatalan pada review cleanup. Daftar cakupan dan status pekerjaan lanjutan dikelola di [Backlog](BACKLOG.md).

## Backlog G · Benchmark dan stress

Jalur CLI/TUI memisahkan CPU, stress dengan sensor, pembacaan file reguler, dan penulisan sementara. Tes clock, cap, pembatalan, sensor hilang, dan file sementara lulus. Uji stress pada hardware development tetap opt-in.

## Backlog F · Observasi terjadwal

CLI menyediakan lifecycle user timer, proses observasi read-only, retensi riwayat, batas log/resource, dan lock lintas proses. Gate unit memeriksa instalasi tidak mengaktifkan timer serta unit asing dipertahankan. Tidak ada unit yang dipasang atau diaktifkan di host development.

## Backlog B dan D · GPU dan OverDrive

Discovery GPU, metrik vendor, batas daya NVIDIA, dan kontrol OverDrive AMD terhubung ke CLI/TUI. Pengguna memasukkan nilai eksplisit sebelum review. Action service yang sama mengikat plan, verifikasi, dan bukti pemulihan bersyarat. Probe dan preview baca-saja berhasil pada NVIDIA RTX 3060 dan AMD Renoir. Host tidak mengekspos offset voltase. Mutasi pada perangkat nyata belum dijalankan.

## Backlog I · Provider macOS dan Windows

Provider native membaca CPU, memori, disk, proses, uptime, dan daya sesuai API host. Disk Utility dan Windows Storage Provider memperluas audit storage. Kebijakan atau dukungan TRIM dibedakan dari bukti pelaksanaan. Fixture dan kompilasi lulus. Workflow native mencakup macOS 15 dan Windows Server 2025; uji perangkat fisik tetap terpisah.

CI native pada `584472c` menemukan fixture macOS memakai nama properti yang berbeda dari Disk Utility. Parser sekarang membaca `WholeDisk` dan tetap menolak partisi dalam inventaris disk fisik. Model CPU/memori dan runtime Windows sudah lulus pada run tersebut.

## Backlog C dan E · Firmware dan perawatan filesystem

Jalur CLI/TUI mengaudit inventaris. Metadata LVFS dan staging firmware memerlukan rencana terpisah. Firmware menunggu bukti fwupd setelah restart, dengan handoff privat dari Riwayat. Filesystem ext offline memakai descriptor terpin, cek read-only, koreksi terbatas e2fsck, dan verifikasi ulang. Fixture aksi dan image ext4 sementara melewati diagnosis baca-saja dan perbaikan metadata. Tidak ada flash atau perbaikan block device pada host.

## Backlog J · Matriks paket standalone

Build arsip untuk Linux amd64/arm64, macOS arm64, dan Windows amd64. Manifest memuat checksum keempat target. Workflow rilis menjalankan smoke terhadap executable yang diekstrak dari arsip pada runner dengan OS dan arsitektur sesuai, lalu menerbitkan aset dari artefak yang sama.

Installer Linux mendukung rilis stabil terbaru dan tag eksplisit. Kedua jalur diuji dengan arsip publik `v0.1.0`, SHA-256 dari rilis, dan eksekusi binari di direktori pengguna sementara.

Verifikasi firmware pascarestart mengikuti format riwayat fwupd 2.0.20. Versi yang dicoba berasal dari entri release dalam riwayat. Hasil sukses juga memerlukan versi aktif yang cocok. Fixture mencakup riwayat pending, sukses, duplikat, dan target yang berbeda.

## Backlog H · Cache Fedora dan Arch

Flow Bersihkan ruang memilih DNF5 atau pacman dari konfigurasi distro dan inventaris backend. Tes pada kedua image kontainer amd64 memeriksa lock native, penolakan rencana stale, kandidat terhapus, sentinel tetap utuh, dan klaim satu kali. CLI dry-run diuji sebagai user biasa di kedua kontainer. [Cakupan backend](PACKAGE-CACHE.md)

## Backlog E dan I · Pemeriksaan APFS dan NTFS

CLI dan TUI menampilkan inventaris APFS atau NTFS serta cek read-only sesuai privilege dan identitas volume. Fixture memeriksa bentuk argumen, klasifikasi output, perubahan mount dan identitas, serta pembatalan. CI native mencakup inventaris aktual. Pada Linux, tes menjaga agar peringatan dari klaim eksklusif milik pemeriksa tidak menutupi peringatan mount yang sebenarnya. [Filesystem](FILESYSTEM.md)

## Review distribusi 0.2.0

Tugas lanjutan membuka status observasi terjadwal secara baca-saja. Ringkasan pemeriksaan campuran menghitung hasil parsial hanya saat ada data. Benchmark tanpa sampel menampilkan status dan penyebabnya. Panduan memasang arsip, cakupan platform, peta Piko, dan changelog mengikuti fitur yang dapat dijalankan. Verifikasi modul dan govulncheck lulus.

Workflow native menyiapkan image APFS dan NTFS sementara dengan identitas terpisah dari volume sistem. Pemeriksaan menguji executable terhadap target itu dan membandingkan checksum image yang dipasang baca-saja.

Uji image NTFS berhasil di Windows Server 2025. Parser APFS mengenali prefiks keluaran pemeriksa dengan tetap meminta bukti selesai pada volume. Tiga varian ext2/3/4 lulus uji diagnosis dan perbaikan image sementara.

Uji image APFS lulus di macOS 15 dengan checksum tetap sama. Di Windows, satu cold start Storage Provider mencapai batas delapan detik. Probe PowerShell Storage memakai batas 20 detik dan menampilkan penyebab jika mencapai batas tersebut.

CI menjalankan lifecycle timer systemd pada akun fixture sementara. Tes menjalankan satu service observasi, mengecek histori, lalu melepas timer dan menghapus akun fixture.

`v0.2.0` terbit dari `386e85e`. Workflow lulus pada Linux amd64/arm64, macOS arm64, dan Windows amd64. Empat arsip publik lolos SHA-256. Binari publik Linux amd64 lulus PTY 80×24 dan 120×36 serta pemasangan eksplisit `v0.1.0` yang dilanjutkan ke versi stabil terbaru. [Aset rilis](https://github.com/christiandoxa/whwmpc/releases/tag/v0.2.0) · [Workflow rilis](https://github.com/christiandoxa/whwmpc/actions/runs/45006921549)

Binari publik juga lulus benchmark CLI baca dan tulis 1 MiB pada file sementara, termasuk riwayat dan pembersihan file hasil tulis. Perbaikan ext4 melalui rencana bertanda tangan lulus pada partisi loop terisolasi di Ubuntu. [Job Linux](https://github.com/christiandoxa/whwmpc/actions/runs/45007448130/job/137162074979)

Lifecycle systemd user lulus pada akun sementara Ubuntu amd64. Fixture juga memeriksa bahwa service menulis satu observasi ke riwayat sebelum unit dilepas. Matriks native mencakup Ubuntu amd64 dan arm64 untuk uji loop ext4 dan daemon pada kedua arsitektur. [Hasil Ubuntu amd64](https://github.com/christiandoxa/whwmpc/actions/runs/45007833793/job/137163307354)

Manifest image Fedora terpin menyediakan amd64 dan arm64. Workflow cache menjalankan skenario DNF5 di kedua runner native.

Ubuntu arm64 lulus alur repair ext4 bertanda tangan dan lifecycle systemd. Fedora DNF5 lulus pada amd64 dan arm64. Arch pacman lulus pada amd64, satu-satunya arsitektur Linux dalam manifest image Arch yang terpin. [Job Ubuntu arm64](https://github.com/christiandoxa/whwmpc/actions/runs/45008131794/job/137164194201) · [CI cache](https://github.com/christiandoxa/whwmpc/actions/runs/45008131828)

Guard stress membaca kanal suhu CPU secara langsung. Beban menunggu sampel segar dengan timeout dan berhenti jika saluran berubah, hilang, panas, atau dibatalkan. Fixture waktu palsu memeriksa jumlah blok saat penghentian. Workflow native menambahkan uji binari dalam kontainer dengan kuota CPU dan memori. [Metodologi](BENCHMARK.md)

Adapter Pacman mengenali Arch Linux ARM aarch64 dengan guard versi dan cakupan yang sama. Kontainer aarch64 dalam emulasi lulus penghapusan cache fixture, lock, rencana berubah, perlindungan database, dan dry-run tanpa privilege. Workflow memakai rootfs bertanggal yang diverifikasi tanda tangan dan SHA-256 sebelum diimpor. [Matriks cleaner](PACKAGE-CACHE.md)

Pemeriksaan firmware mewajibkan konfirmasi baterai absen jika kapasitas tidak diketahui. Sandbox fwupd 2.0.20 menulis capsule fixture dan CLI membedakan pending dari versi aktif yang terverifikasi setelah simulasi boot. Inventaris GPU menyembunyikan tuning AMD saat driver hanya-baca. Fixture voltage membedakan apply, restore, dan commit yang ditolak. Preview baca-saja pada RTX 3060 dan Renoir dikonfirmasi dengan nilai aktual. [Firmware](FIRMWARE.md) · [GPU](HARDWARE.md)

Cache lokal fwupd pada host pengembangan belum memuat kandidat capsule UEFI. CI lulus untuk guard stress pada Linux amd64/arm64 dan Pacman aarch64 pada rootfs resmi bertanggal. [CI stress](https://github.com/christiandoxa/whwmpc/actions/runs/45009274228) · [CI cache](https://github.com/christiandoxa/whwmpc/actions/runs/45009494561)

Provider macOS dan Windows meneruskan status pengisian ke tampilan daya. Fixture membedakan pengisian, daya AC saja, persentase diketahui dengan flag Windows yang tidak diketahui, serta perangkat tanpa baterai. Paket dan CI menambah macOS Intel dan Windows 11 arm64 agar verifikasi executable memakai runner yang searsitektur. [Matriks native](NATIVE.md) · [Build rilis](RELEASE.md)

`v0.3.0` terbit dari `418e39c` dengan enam paket untuk Linux, macOS, dan Windows amd64/arm64. Semua checksum publik diverifikasi ulang. Binari Linux amd64 lulus PTY 80×24 dan 120×36 serta upgrade installer dari tag `v0.1.0`. CI juga lulus APFS pada macOS Intel dan NTFS pada Windows 11 ARM64, dengan checksum kedua image tetap sama. [Rilis](https://github.com/christiandoxa/whwmpc/releases/tag/v0.3.0) · [CI rilis](https://github.com/christiandoxa/whwmpc/actions/runs/45010411657) · [CI native](https://github.com/christiandoxa/whwmpc/actions/runs/45010136965)

Tampilan native memisahkan status daya dan kebijakan TRIM yang teramati dari kapabilitas Linux. Fixture mencakup data daya parsial, status pengisian Windows, dan sumber serta counter pada laporan Markdown. Build CI menjalankan pemeriksaan CLI native pada macOS dan Windows di kedua arsitektur. [Matriks native](NATIVE.md)

CI `c79c5c9` lulus pada enam target. CLI macOS dan Windows lulus `power`, `trim`, dan `check --json`. Uji NVIDIA memakai binari build `c79c5c9` di namespace privat, dengan backend fixture. Apply terverifikasi pada 169 W, restore terverifikasi pada 170 W, dan empat kondisi penolakan berhenti sebelum penulisan. [CI native](https://github.com/christiandoxa/whwmpc/actions/runs/45011977359) · [Uji NVIDIA](assets/nvidia-cli-fixture.txt)

Uji firmware dengan fwupd 2.0.20 mereproduksi refresh dari cache kosong yang belum dapat diverifikasi karena checksum belum muncul pada respons daemon. Setelah perbaikan, refresh bertanda tangan terverifikasi dan CLI dapat melanjutkan dengan daemon yang sama. Konfirmasi dan kondisi daya diverifikasi sebelum satu unduhan CAB. ESP sementara menerima capsule sintetis dan handoff `--resume` cocok dengan hasil native setelah simulasi boot. [Uji CLI dan fwupd](assets/fwupd-uefi-apply.json) · [Firmware](FIRMWARE.md)

Pemeriksaan ulang UX mengembalikan aksesori Piko di header ringkas dan seni pose lengkap di galeri lebar. Tes message deterministik mencakup input cepat, tick usang, lirikan, anggukan, dan reveal tanpa perubahan dimensi. Smoke PTY lulus pada 80×24 dan 120×36, dan alur fixture direkam pada kedua ukuran. [Motion](MOTION.md) · [Demo galeri](assets/piko-demo.gif)

`v0.3.1` terbit dari `6ea1976`. Keenam paket publik lulus smoke di runner searsitektur dan verifikasi SHA-256 setelah diunduh. Isi setiap file dalam arsip cocok dengan build lokal. Izin file lisensi Go berbeda antar lingkungan dan mengubah hash arsip lokal. Binari publik Linux amd64 lulus PTY 80×24 dan 120×36. Installer memasang `v0.1.0` dan memperbaruinya ke `v0.3.1` pada home tmpfs dengan UID biasa. [Rilis](https://github.com/christiandoxa/whwmpc/releases/tag/v0.3.1) · [Workflow rilis](https://github.com/christiandoxa/whwmpc/actions/runs/45013551425) · [CI](https://github.com/christiandoxa/whwmpc/actions/runs/45013524319) · [CI native](https://github.com/christiandoxa/whwmpc/actions/runs/45013524271)

Detail GPU membedakan batas daya yang diminta dan ditegakkan, serta sumber bacaan daya papan dan SoC. Fixture menguji dua batas NVIDIA yang berbeda, dan CLI membaca batas dari perangkat secara baca-saja. TUI firmware memberi gestur sukses ketika verifikasi dari Riwayat sudah berhasil. Tes model mencakup restart tertunda, kegagalan, dan kebutuhan izin. [Dukungan GPU](HARDWARE.md) · [Firmware](FIRMWARE.md)

Provider Windows mempertahankan cakupan `GetSystemTimes` untuk mesin dengan lebih dari 64 prosesor logis. Fixture 128 CPU memverifikasi label grup dan pencegahan diagnosis beban seluruh host dari data parsial tersebut. Build Windows amd64 dan arm64 lulus. [Matriks native](NATIVE.md)

Pada fixture NVIDIA terisolasi, TUI produksi lulus review, autentikasi, apply, dan restore pada 80×24 serta 120×36. Pengujian awal menemukan pilihan restore yang masih muncul sesudah pemulihan. Riwayat kini mengaitkan restore terverifikasi dengan tindakan asal. Tes unit memeriksa riwayat lama dan hasil parsial, sedangkan uji CLI membuktikan rencana lain ditolak setelah restore terverifikasi walau nilai fixture kembali cocok. [Uji TUI dan CLI](assets/nvidia-tui-fixture.txt) · [Model tindakan](ACTIONS.md)

Skrip paket menormalkan izin file README dan lisensi. Dua build snapshot dengan mode README input `0444` dan `0644` menghasilkan SHA-256 yang sama untuk keenam arsip. Seluruh dokumen dan lisensi dalam arsip memakai mode `0644`. [Skrip rilis](../scripts/release.sh)

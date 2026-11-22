# Pembersihan cache paket

Buka **Bersihkan ruang** di Home atau jalankan `whwmpc clean --dry-run`. Keduanya membaca konfigurasi package manager, menampilkan cakupan, dan menjumlahkan ukuran file dari metadata yang bisa diperiksa. `whwmpc clean --dry-run --write-plan cache.json` menyimpan pratinjau untuk `whwmpc apply --file cache.json --confirm ID`. Periksa ID, cakupan, dan dampak rencana sebelum mengonfirmasi. Unduhan dapat diperlukan lagi setelah cache dihapus.

| Distro | Backend yang diuji | Cakupan |
| --- | --- | --- |
| Arch Linux | Pacman 7.1.0 dengan libalpm 16.0 | Arsip paket, tanda tangan, dan unduhan parsial di direktori `CacheDir` tepercaya di bawah `/var/cache/pacman`. Paket terpasang dan database repositori dipertahankan. |
| Fedora 44 | DNF5 dan libdnf5 5.4.6.0 dengan util-linux `flock --fcntl` | Unduhan RPM yang bisa diinventaris di direktori `packages` dari cache sistem aktual di bawah `/var/cache/libdnf5`. Paket terpasang, metadata repositori, dan cache solver dipertahankan. |
| Debian dan Ubuntu | APT 2.6, 2.8, dan 3.0 | Unduhan paket, indeks parsial, dan indeks biner cache sesuai konfigurasi APT yang didukung. |

Pacman menyediakan konfigurasi efektif melalui `pacman-conf`. whwmpc memanggil `pacman --sync --clean --clean` dengan DB sistem dan direktori cache yang sudah diperiksa, lalu memberi jawaban terpisah untuk mempertahankan database repositori. Pacman mengambil lock transaksi sebelum membersihkan cache. [Pacman manual](https://man.archlinux.org/man/pacman.8) · [Pacman-conf manual](https://man.archlinux.org/man/pacman-conf.8).

DNF5 menyediakan `system_cachedir` dan konfigurasi lock dari `--dump-main-config`. Perintah inspeksi menulis log sementara ke direktori privat. Saat dieksekusi, `dnf5 clean packages` memakai konfigurasi cache yang diperiksa dan menulis ke log normalnya. whwmpc memakai lock eksklusif nonblocking pada `system-repo.lock`, memakai `flock --fcntl` agar berkonflik dengan POSIX record lock yang dipakai DNF5 saat mengakses repositori sistem. Tool lain yang melewati lock tetap dapat mengubah cache. [DNF5 Clean](https://dnf5.readthedocs.io/en/stable/commands/clean.8.html) · [DNF5 5.4.6.0 system repo lock](https://github.com/rpm-software-management/dnf5/blob/5.4.6.0/libdnf5/base/base.cpp#L457) · [util-linux flock](https://man7.org/linux/man-pages/man1/flock.1.html).

Tindakan memerlukan akses root atau otorisasi sudo. Pemeriksaan ulang menolak perubahan konfigurasi, executable, atau identitas kandidat yang teramati. Symlink, hard link, nama berkas asing, izin yang tidak memadai, dan layout di luar cakupan tampil sebagai kegagalan pemeriksaan. whwmpc mempertahankan lock file milik package manager.

Hasil memisahkan ukuran logis kandidat, ukuran file kandidat yang teramati hilang, serta selisih ruang bebas filesystem. Selisih ruang bebas dapat terpengaruh alokasi blok, cache filesystem, dan aktivitas lain pada volume. Aksi hanya menampilkan hasil terverifikasi sesudah cakupan dibaca ulang.

Workflow memakai digest terpin dan jaringan dimatikan. Fedora diuji pada amd64 dan arm64. Image Arch terpin menyediakan amd64. Fixture mencakup perubahan kandidat, lock yang sedang dipakai, penghapusan paket cache, dan sentinel di luar cakupan. [Hasil CI kontainer](https://github.com/christiandoxa/whwmpc/actions/runs/45008131828)

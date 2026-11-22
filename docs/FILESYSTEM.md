# Pemeriksaan dan perbaikan filesystem

## Linux ext2, ext3, dan ext4

Linux amd64 dan arm64 mendukung diagnosis offline partisi ext2, ext3, dan ext4 biasa dengan util-linux dan e2fsprogs 1.47.x. Inventaris memakai lsblk dan mengambil jenis, UUID, serta status mount. Aksi target hanya menerima node partisi langsung seperti `/dev/nvme1n1p1`. Volume root perlu diperiksa dari lingkungan pemulihan. Partisi dengan holder aktif, symlink, atau identitas yang berubah ditolak.

Gunakan `whwmpc filesystem --check DEVICE` saat target sudah dilepas dari sistem. Mode `e2fsck -f -n` tidak memperbaiki atau menjalankan replay jurnal. Pemeriksaan memerlukan akses baca perangkat dan klaim eksklusif untuk menolak filesystem yang sedang dipakai lintas namespace. Pemeriksaan CLI dibatasi dua menit. Pemeriksaan tanpa bukti pembacaan yang andal menghasilkan partial, unknown, atau error sesuai tahap.

Perbaikan memakai `e2fsck -p -E fixes_only,nodiscard` setelah review dan otorisasi administrator. Read-only check dan perbaikan menargetkan descriptor partisi yang dipertahankan, mengecek urutan disk di kernel, dan membaca ulang jenis serta UUID secara langsung. Mode `-p` hanya memperbaiki masalah yang dapat diselesaikan otomatis. Keadaan yang memerlukan tindakan manual tetap dilaporkan. Siapkan cadangan dan jangan mount target selama pemeriksaan. Metadata serta file yang rusak bisa berubah atau hilang. Perbaikan ini tidak menyediakan undo.

Setelah perbaikan, volume dibaca ulang dalam mode read-only. Status verified memerlukan hasil native yang cocok dengan pemeriksaan ulang. Jika diminta restart, boot kembali ke lingkungan yang dapat mempertahankan partisi offline, cocokkan UUID pada inventaris, lalu jalankan pemeriksaan ulang. Jika perbaikan terhenti, cek keadaan aktual sebelum mempertimbangkan rencana baru.

Fixture menguji target terpasang, output rusak, jenis dan UUID berubah, jurnal belum dipulihkan, izin, dan klasifikasi hasil. Tes Linux membuat image ext4 reguler di direktori temporer. Tes tersebut memastikan digest image tetap sama saat pemeriksaan baca-saja, memasukkan kerusakan reference count hanya ke image itu, memperbaikinya, lalu mengecek ulang. Penggunaan sudo pada partisi asli belum diuji.

[e2fsck 1.47.0](https://github.com/tytso/e2fsprogs/blob/v1.47.0/e2fsck/e2fsck.8.in) · [Implementasi klaim dan exit code](https://github.com/tytso/e2fsprogs/blob/v1.47.0/e2fsck/unix.c) · [Sumber BLKGETDISKSEQ](https://github.com/torvalds/linux/blob/master/include/uapi/linux/fs.h) · [Util-linux blkid dan lsblk](https://github.com/util-linux/util-linux/tree/v2.39.3/misc-utils)

## macOS APFS dan Windows NTFS

Jalankan `whwmpc filesystem` untuk melihat target yang tersedia. macOS dan Windows menyediakan pemeriksaan baca-saja.

- Di **macOS**, jalankan `whwmpc filesystem --check /dev/disk3s1` sebagai root dan ganti contoh dengan volume APFS dari inventaris. Output `diskutil` dan daftar mount kernel mengidentifikasi volume serta kontainernya. Volume terkunci perlu dibuka oleh pengguna sebelum bisa diperiksa. `fsck_apfs` memakai `-n` pada perangkat raw yang sesuai dan menambahkan `-l` jika kontainer aktif. Verifikasi kontainer aktif dapat membuat sistem berhenti merespons sementara. Hasil tanpa kesalahan pada kontainer aktif tetap `partial`. [Manual macOS fsck_apfs(8)](https://man.freebsd.org/cgi/man.cgi?query=fsck_apfs&sektion=8&manpath=macOS+26.7) · [Manual macOS diskutil(8)](https://man.freebsd.org/cgi/man.cgi?manpath=macOS+26.7&query=diskutil&sektion=8) · [Dokumentasi Apple getfsstat(2)](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/getfsstat.2.html)
- Di **Windows**, jalankan `whwmpc filesystem --check C:` dari terminal Administrator dan ganti huruf drive sesuai inventaris NTFS lokal. Perintah sistem `chkdsk.exe` menerima identitas GUID volume sebagai satu-satunya argumen. Pada volume aktif, laporan bersifat `partial`, termasuk ketika CHKDSK selesai tanpa menemukan masalah. [Dokumentasi Microsoft CHKDSK](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/chkdsk) · [Identitas GUID volume Windows](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-volume)

Alat pemeriksaan dibatasi dua menit. Jika proses dibatalkan, melebihi batas waktu, atau identitas volume berubah, hasil kesehatan tidak diketahui. Fixture mencakup argumen baca-saja, izin, perubahan identitas dan mount, serta klasifikasi hasil. Tes inventaris native dapat dijalankan di host macOS atau Windows dengan `WHWMPC_TEST_NATIVE=1`.

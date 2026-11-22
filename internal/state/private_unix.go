//go:build unix

package state

import (
	"os"
	"syscall"
)

func private(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid() && info.Mode().Perm()&0o077 == 0
}

//go:build unix

package state

import (
	"os"
	"syscall"
)

const openReadFlags = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK

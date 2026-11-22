//go:build !unix

package state

import "os"

const openReadFlags = os.O_RDONLY

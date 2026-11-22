//go:build !unix

package state

import "os"

// The default config path on Windows is inside the current user's profile.
func private(_ os.FileInfo) bool { return true }

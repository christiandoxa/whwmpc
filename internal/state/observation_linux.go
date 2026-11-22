//go:build linux

package state

import (
	"errors"
	"os"
	"syscall"
)

func (s Store) BeginObservation() (release func(), busy bool, err error) {
	root, err := privateRoot(s.StateDir, true)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	f, err := root.OpenFile("observation.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, false, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !private(info) {
		f.Close()
		return nil, false, errors.New("lock observasi tidak privat")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() { _ = f.Close() }, false, nil
}

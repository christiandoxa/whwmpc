//go:build !linux

package state

import (
	"errors"
	"time"
)

func (s Store) BeginAction(string, time.Time) (func(), error) {
	return nil, errors.New("eksekusi aksi tersedia di Linux dengan backend yang didukung")
}

func (s Store) LockActions() (func(), error) {
	return nil, errors.New("verifikasi firmware tersedia di Linux dengan fwupd")
}

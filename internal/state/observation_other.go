//go:build !linux

package state

import "errors"

func (s Store) BeginObservation() (func(), bool, error) {
	return nil, false, errors.New("observasi terjadwal tersedia pada Linux")
}

package state

import (
	"errors"
	"os"
)

func OpenRegular(path string) (*os.File, error) {
	f, err := os.OpenFile(path, openReadFlags, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("expected a regular file")
	}
	return f, nil
}

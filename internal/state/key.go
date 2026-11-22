package state

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const keyName = "action-key"

// ActionKey returns the private local signing key, creating one only when requested.
func (s Store) ActionKey(create bool) ([32]byte, error) {
	if !create {
		return s.actionKey(nil)
	}
	var candidate [32]byte
	_, _ = rand.Read(candidate[:])
	return s.EnsureActionKey(candidate)
}

// EnsureActionKey publishes a preview session's key if no key already exists.
func (s Store) EnsureActionKey(candidate [32]byte) ([32]byte, error) {
	if candidate == ([32]byte{}) {
		return [32]byte{}, errors.New("invalid local action key")
	}
	return s.actionKey(&candidate)
}

func (s Store) actionKey(candidate *[32]byte) ([32]byte, error) {
	var key [32]byte
	root, err := privateRoot(s.StateDir, candidate != nil)
	if err != nil {
		return key, err
	}
	defer root.Close()
	data, err := readPrivate(root, keyName, int64(len(key)))
	if candidate != nil && errors.Is(err, fs.ErrNotExist) {
		if err := atomicWrite(root, keyName, candidate[:], false); err == nil {
			return *candidate, nil
		} else if !errors.Is(err, fs.ErrExist) {
			return key, err
		}
		data, err = readPrivate(root, keyName, int64(len(key)))
	}
	if err != nil {
		return [32]byte{}, err
	}
	if len(data) != len(key) || [32]byte(data) == ([32]byte{}) {
		return [32]byte{}, errors.New("invalid local action key")
	}
	return [32]byte(data), nil
}

func validPlanID(id string) bool {
	return len(id) == 40 && strings.Trim(id, "0123456789abcdef") == ""
}

// WriteNew writes a private file at an explicitly selected path without overwriting.
func WriteNew(path string, data []byte) error {
	if path == "" || filepath.Base(path) == "." || filepath.Base(path) == ".." {
		return errors.New("output must identify a file")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return atomicWrite(root, filepath.Base(path), data, false)
}

package state

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Store struct{ ConfigDir, StateDir string }

func Default() (Store, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return Store{}, err
	}
	store := Store{ConfigDir: filepath.Join(config, "whwmpc"), StateDir: filepath.Join(config, "whwmpc", "state")}
	if runtime.GOOS == "linux" {
		dir := os.Getenv("XDG_STATE_HOME")
		if !filepath.IsAbs(dir) {
			home, err := os.UserHomeDir()
			if err != nil {
				return Store{}, err
			}
			dir = filepath.Join(home, ".local", "state")
		}
		store.StateDir = filepath.Join(dir, "whwmpc")
	}
	return store, nil
}

func privateRoot(dir string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("app data directory must be absolute")
	}
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	before, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || !private(before) {
		return nil, errors.New("app data directory must be private and owned by the user")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, errors.New("app data directory changed")
	}
	return root, nil
}

func readPrivate(root *os.Root, name string, limit int64) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || !private(before) {
		return nil, errors.New("app data file must be private and owned by the user")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, errors.New("app data file changed")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("app data file exceeds size limit")
	}
	return data, nil
}

func atomicWrite(root *os.Root, name string, data []byte, replace bool) error {
	tmp := ".tmp-" + rand.Text()
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if replace {
		return root.Rename(tmp, name)
	}
	return root.Link(tmp, name)
}

func preferenceName(key string) (string, error) {
	switch key {
	case "motion", "workload":
		return key + ".txt", nil
	default:
		return "", errors.New("unknown preference")
	}
}

func (s Store) Preference(key string) (string, error) {
	name, err := preferenceName(key)
	if err != nil {
		return "", err
	}
	root, err := privateRoot(s.ConfigDir, false)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer root.Close()
	data, err := readPrivate(root, name, 64)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(data)), err
}

func (s Store) SetPreference(key, value string) error {
	name, err := preferenceName(key)
	if err != nil {
		return err
	}
	if len(value) == 0 || len(value) > 32 || strings.Trim(value, "abcdefghijklmnopqrstuvwxyz-") != "" {
		return fmt.Errorf("invalid %s value", key)
	}
	root, err := privateRoot(s.ConfigDir, true)
	if err != nil {
		return err
	}
	defer root.Close()
	return atomicWrite(root, name, []byte(value+"\n"), true)
}

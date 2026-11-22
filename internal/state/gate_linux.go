package state

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

const actionLockName = "action.lock"
const claimPrefix = "claim-"

// BeginAction reserves a validated plan once across processes until its expiration.
// Keep the returned release function deferred through post-action verification.
func (s Store) BeginAction(planID string, until time.Time) (func(), error) {
	now := time.Now()
	if !validPlanID(planID) || !until.After(now) || until.After(now.Add(10*time.Minute)) {
		return nil, errors.New("ID atau masa berlaku rencana tidak valid")
	}
	root, err := privateRoot(s.StateDir, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	release, err := lockActions(root)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			release()
		}
	}()
	if err := pruneClaims(root, now); err != nil {
		return nil, err
	}
	if err := atomicWrite(root, claimPrefix+planID, []byte(until.UTC().Format(time.RFC3339Nano)), false); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, errors.New("rencana sudah digunakan, buat preview baru")
		}
		return nil, err
	}
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
		return nil, err
	}
	ok = true
	return release, nil
}

// LockActions serializes verification of a saved firmware handoff with new actions.
func (s Store) LockActions() (func(), error) {
	root, err := privateRoot(s.StateDir, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return lockActions(root)
}

func lockActions(root *os.Root) (func(), error) {
	if _, err := root.Lstat(actionLockName); errors.Is(err, fs.ErrNotExist) {
		if err := atomicWrite(root, actionLockName, nil, false); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	before, err := root.Lstat(actionLockName)
	if err != nil || !before.Mode().IsRegular() || !private(before) {
		return nil, errors.New("file lock aksi lokal tidak aman")
	}
	file, err := root.OpenFile(actionLockName, os.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { _ = file.Close() }) }
	if after, err := file.Stat(); err != nil || !os.SameFile(before, after) {
		release()
		return nil, errors.New("file lock aksi lokal berubah")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		release()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("tindakan lain masih diverifikasi atau sedang berjalan")
		}
		return nil, err
	}
	return release, nil
}

func pruneClaims(root *os.Root, now time.Time) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id, found := strings.CutPrefix(entry.Name(), claimPrefix)
		if !found || !validPlanID(id) {
			continue
		}
		data, err := readPrivate(root, entry.Name(), 64)
		if err != nil {
			return err
		}
		until, err := time.Parse(time.RFC3339Nano, string(data))
		if err != nil {
			return errors.New("penanda eksekusi rencana rusak")
		}
		if !now.Before(until) {
			if err := root.Remove(entry.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

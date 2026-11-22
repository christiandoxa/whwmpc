//go:build linux

package state

import (
	"path/filepath"
	"testing"
)

func TestObservationLockSerializes(t *testing.T) {
	s := Store{StateDir: filepath.Join(t.TempDir(), "state")}
	release, busy, err := s.BeginObservation()
	if err != nil || busy {
		t.Fatalf("first: %v %v", busy, err)
	}
	if _, busy, err = s.BeginObservation(); err != nil || !busy {
		t.Fatalf("overlap: %v %v", busy, err)
	}
	release()
	release, busy, err = s.BeginObservation()
	if err != nil || busy {
		t.Fatalf("later: %v %v", busy, err)
	}
	release()
}

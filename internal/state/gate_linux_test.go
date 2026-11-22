package state

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"
)

func TestActionGateSerializesAndPreventsReplay(t *testing.T) {
	s := testStore(t)
	first, second := strings.Repeat("a", 40), strings.Repeat("b", 40)
	expiry := time.Now().Add(time.Minute)
	release, err := s.BeginAction(first, expiry)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if releaseOther, err := s.BeginAction(second, expiry); err == nil {
		releaseOther()
		t.Fatal("accepted second mutation while action was active")
	}
	release()
	reopened := Store{StateDir: s.StateDir}
	if releaseReplay, err := reopened.BeginAction(first, expiry); err == nil {
		releaseReplay()
		t.Fatal("accepted a replay through another store")
	}
	releaseSecond, err := reopened.BeginAction(second, expiry)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()
}

func TestActionGatePrunesExpiredClaims(t *testing.T) {
	s := testStore(t)
	root, err := privateRoot(s.StateDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	expired := claimPrefix + strings.Repeat("a", 40)
	if err := atomicWrite(root, expired, []byte(time.Now().Add(-time.Minute).Format(time.RFC3339Nano)), false); err != nil {
		t.Fatal(err)
	}
	release, err := s.BeginAction(strings.Repeat("b", 40), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := root.Lstat(expired); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expired claim was retained: %v", err)
	}
}

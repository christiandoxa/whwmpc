package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
)

func testStore(t *testing.T) Store {
	t.Helper()
	return Store{StateDir: filepath.Join(t.TempDir(), "state")}
}

func TestHistorySummarizesAndKeepsRollbackProofPrivate(t *testing.T) {
	s := testStore(t)
	if list, err := s.History(); err != nil || len(list) != 0 {
		t.Fatalf("fresh history = %v, %v", list, err)
	}
	if _, err := os.Stat(s.StateDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("reading history created state")
	}
	at := time.Now().UTC()
	findings := []diagnostics.Finding{
		{Severity: diagnostics.Critical, Availability: domain.Partial, Observation: "user private path"},
		{Severity: diagnostics.Warning, Availability: domain.PermissionRequired},
	}
	if err := s.RecordScan("ssd", at, findings); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 40)
	result := actions.Result{
		Plan: actions.Plan{ID: id, Kind: actions.PowerProfile, Initial: "balanced", Rollback: true,
			Target: "private-profile-target", Operation: []string{"private-command"}},
		Status: actions.Verified, FinishedAt: at.Add(time.Second), Message: "private-message", Receipt: "private-receipt",
	}
	if err := s.RecordAction(result); err != nil {
		t.Fatal(err)
	}
	list, err := s.History()
	if err != nil || len(list) != 2 || list[0].PlanID != id || list[0].Action == nil || !list[0].Action.CanRestore ||
		list[1].Findings == nil || list[1].Findings.Critical != 1 || list[1].Findings.Unavailable != 1 {
		t.Fatalf("history = %+v, %v", list, err)
	}
	data, err := json.Marshal(list)
	if err != nil || strings.Contains(string(data), "private-") || strings.Contains(string(data), "user private path") {
		t.Fatalf("history has private data or invalid JSON: %s, %v", data, err)
	}
	loaded, err := s.RollbackReceipt(id)
	if err != nil || loaded.Receipt != result.Receipt || loaded.Plan.Initial != "balanced" {
		t.Fatalf("private receipt = %+v, %v", loaded, err)
	}
	if _, err := s.RollbackReceipt("../outside"); err == nil {
		t.Fatal("accepted traversal in receipt lookup")
	}
}

func TestHistoryConcurrentRetention(t *testing.T) {
	s := testStore(t)
	at := time.Now().UTC()
	const count = MaxHistory + 12
	var writers sync.WaitGroup
	for worker := range 8 {
		writers.Go(func() {
			for i := worker; i < count; i += 8 {
				if err := s.RecordScan("overview", at.Add(time.Duration(i)*time.Second), nil); err != nil {
					t.Errorf("submit event %d: %v", i, err)
				}
			}
		})
	}
	writers.Wait()
	entries, err := s.History()
	if err != nil || len(entries) != MaxHistory {
		t.Fatalf("retained history %d, err %v", len(entries), err)
	}
	for i, entry := range entries {
		want := at.Add(time.Duration(count-1-i) * time.Second)
		if !entry.At.Equal(want) {
			t.Fatalf("history slot %d is %s, want %s", i, entry.At, want)
		}
	}
	root, err := privateRoot(s.StateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dirs, err := eventDirectories(root)
	if err != nil || len(dirs) != MaxHistory {
		t.Fatalf("disk retention %d, err %v", len(dirs), err)
	}
}

func TestActionKeyIsLazyPrivateAndHasOneWinner(t *testing.T) {
	s := testStore(t)
	if _, err := s.ActionKey(false); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unknown key: %v", err)
	}
	if _, err := os.Stat(s.StateDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("read created state")
	}
	keys := make(chan [32]byte, 8)
	var writers sync.WaitGroup
	for i := 1; i <= cap(keys); i++ {
		writers.Go(func() {
			key, err := s.EnsureActionKey([32]byte{byte(i)})
			if err != nil {
				t.Errorf("publish key: %v", err)
				return
			}
			keys <- key
		})
	}
	writers.Wait()
	close(keys)
	winner, err := s.ActionKey(false)
	if err != nil || winner == ([32]byte{}) {
		t.Fatalf("persisted key unavailable, err %v", err)
	}
	for key := range keys {
		if key != winner {
			t.Fatal("competing writers got different action keys")
		}
	}
	if err := os.WriteFile(filepath.Join(s.StateDir, keyName), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActionKey(true); err == nil {
		t.Fatal("corrupt key was silently replaced")
	}
}

func TestWriteNewNeverReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := WriteNew(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := WriteNew(path, []byte("replacement")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("existing file write = %v", err)
	}
	data, err := os.ReadFile(path)
	if string(data) != "original" || err != nil {
		t.Fatalf("written file = %q, %v", data, err)
	}
	link := filepath.Join(dir, "plan.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	if err := WriteNew(link, []byte("replacement")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("symlink destination write = %v", err)
	}
}

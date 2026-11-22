package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPreferenceOnlyCreatesWhenSelected(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "whwmpc")
	store := Store{ConfigDir: dir}
	if value, err := store.Preference("motion"); value != "" || err != nil {
		t.Fatalf("initial read = %q, %v", value, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("read created the config directory")
	}
	if err := store.SetPreference("motion", "reduced"); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Preference("motion"); value != "reduced" || err != nil {
		t.Fatalf("saved read = %q, %v", value, err)
	}
	if err := store.SetPreference("../outside", "full"); err == nil {
		t.Fatal("accepted an arbitrary preference path")
	}
}

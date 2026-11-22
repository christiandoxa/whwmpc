package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type fakeManager struct {
	calls []string
	fail  string
}

func (*fakeManager) LookPath(string) (string, error) { panic("fixed systemctl path expected") }
func (f *fakeManager) Run(_ context.Context, file string, args ...string) sysrun.Result {
	if file != "/usr/bin/systemctl" {
		panic(file)
	}
	f.calls = append(f.calls, strings.Join(args, " "))
	if args[2] == f.fail {
		return sysrun.Result{Err: errors.New("session unavailable")}
	}
	return sysrun.Result{}
}

func TestLifecycleIsOptInAndPreservesForeignUnit(t *testing.T) {
	ctx := context.Background()
	runner := &fakeManager{}
	m := Manager{Dir: t.TempDir(), Binary: "/home/user/.local/bin/whwmpc", Runner: runner}
	if _, err := m.Manage(ctx, "install", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(runner.calls, []string{"--user --no-pager show --property=Version", "--user --no-pager daemon-reload"}) {
		t.Fatal(runner.calls)
	}
	raw, _ := os.ReadFile(filepath.Join(m.Dir, service))
	if !strings.Contains(string(raw), "CPUQuota=20%") || !strings.Contains(string(raw), "daemon run") {
		t.Fatal(string(raw))
	}
	if _, err := m.Manage(ctx, "install", 5*time.Minute); err == nil {
		t.Fatal("overwrote unit")
	}
	if _, err := m.Manage(ctx, "uninstall", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, service)); !os.IsNotExist(err) {
		t.Fatal("service left installed")
	}
	if err := os.WriteFile(filepath.Join(m.Dir, service), []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Manage(ctx, "install", 5*time.Minute); err == nil {
		t.Fatal("foreign unit replaced")
	}
	if _, err := m.Manage(ctx, "uninstall", 0); err == nil {
		t.Fatal("foreign unit removed")
	}
}

func TestUnitRejectsInjectedPathAndShortInterval(t *testing.T) {
	if _, _, err := units("/tmp/app\nExecStart=/bin/sh", time.Hour); err == nil {
		t.Fatal("accepted injected unit")
	}
	if _, _, err := units("/tmp/app", time.Minute); err == nil {
		t.Fatal("accepted too short interval")
	}
}

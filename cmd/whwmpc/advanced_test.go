package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func TestFirmwareResumeRecordsTerminalHistory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fwupd resume is available on Linux")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	fixture := `#!/bin/sh
case "$6" in
    get-devices) printf '%s\n' "$WHWMPC_TEST_FWUPD_DEVICES" ;;
    get-history) printf '%s\n' "$WHWMPC_TEST_FWUPD_HISTORY" ;;
    *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "fwupdmgr"), []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	planID, deviceID := strings.Repeat("1", 40), strings.Repeat("a", 40)
	target := deviceID + "@2.0"
	created := time.Unix(1720000000, 0)
	for _, terminal := range []struct {
		status   actions.Status
		active   string
		native   int
		wantExit int
	}{
		{actions.Verified, "2.0", 2, 0},
		{actions.Failed, "1.0", 3, 1},
	} {
		t.Run(string(terminal.status), func(t *testing.T) {
			store := state.Store{StateDir: filepath.Join(t.TempDir(), "state")}
			maintenance := workflow.NewMaintenance(store, false)
			if err := store.RecordAction(actions.Result{
				Plan:   actions.Plan{ID: planID, Kind: actions.FirmwareUpdate, Target: target, CreatedAt: created},
				Status: actions.Partial, Started: true, CancellationRequested: true, FinishedAt: created.Add(time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			setFirmware := func(active string, native int, modified int64) {
				t.Helper()
				t.Setenv("WHWMPC_TEST_FWUPD_DEVICES", fmt.Sprintf(`{"Devices":[{"DeviceId":%q,"Version":%q}]}`, deviceID, active))
				t.Setenv("WHWMPC_TEST_FWUPD_HISTORY", fmt.Sprintf(`{"Devices":[{"DeviceId":%q,"Version":"1.0","Modified":%d,"UpdateState":%d,"Releases":[{"Version":"2.0"}]}]}`,
					deviceID, modified, native))
			}
			verify := func(flag, value, wantState string, wantExit int) {
				t.Helper()
				var out, stderr bytes.Buffer
				code, handled := storageActionCommand(t.Context(), []string{"firmware", flag, value, "--json"}, maintenance, &out, &stderr)
				var result firmware.Verification
				if err := json.Unmarshal(out.Bytes(), &result); err != nil || !handled || code != wantExit || result.State != wantState || stderr.Len() != 0 {
					t.Fatalf("exit=%d result=%s stderr=%s decode=%v", code, &out, &stderr, err)
				}
			}
			setFirmware("1.0", 4, created.Unix()+1)
			verify("--resume", planID, "reboot_required", 1)
			setFirmware(terminal.active, terminal.native, created.Unix()-1)
			verify("--resume", planID, "unknown", 1)
			setFirmware(terminal.active, terminal.native, created.Unix()+2)
			verify("--verify", target, string(terminal.status), terminal.wantExit)
			entries, err := store.History()
			if err != nil || len(entries) != 1 || !entries[0].Action.VerifyAfterRestart {
				t.Fatalf("unfinished or unbound verification altered history: %+v, %v", entries, err)
			}
			for range 2 {
				verify("--resume", planID, string(terminal.status), terminal.wantExit)
			}
			entries, err = store.History()
			if err != nil || len(entries) != 2 || entries[0].PlanID != planID || entries[0].Action.Status != terminal.status ||
				!entries[0].Action.CancellationRequested || entries[0].Action.CanRestore || entries[1].Action.VerifyAfterRestart {
				t.Fatalf("terminal history = %+v, %v", entries, err)
			}
			if changed := entries[0].Action.Changed; (changed != nil && *changed) != (terminal.status == actions.Verified) {
				t.Fatalf("unexpected changed value %v for %s", changed, terminal.status)
			}
		})
	}
}

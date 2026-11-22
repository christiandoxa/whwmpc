package inspection

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/platform"
	"github.com/christiandoxa/whwmpc/internal/privread"
)

type noTemperature struct{ platform.Demo }

func (p noTemperature) Overview(ctx context.Context) domain.Overview {
	o := p.Demo.Overview(ctx)
	o.Temperature = domain.Temperature{Availability: domain.PermissionRequired}
	return o
}

func TestInspectionReadsApprovedSources(t *testing.T) {
	audit, trim, power := demoSMART(), demoTrim(), demoPower()
	audit.Availability = domain.Partial
	trimReply := privread.TrimReading{Audit: trim}
	for _, mount := range trim.Mounts {
		trimReply.Paths = append(trimReply.Paths, privread.TrimPath{ID: mount.ID, Point: mount.Point})
	}
	replies := map[privread.Operation]any{
		privread.SMART: audit, privread.TRIM: trimReply, privread.Power: privread.PowerReading{Snapshot: power},
		privread.Temperature: domain.Temperature{Availability: domain.Available, Celsius: new(43.0), Source: "fixture"},
		privread.GPU: privread.GPUReading{Snapshot: hardware.Snapshot{At: platform.DemoTime, Availability: domain.Available,
			Devices: []hardware.Device{{Model: "Fixture GPU"}}}, IDs: []string{"fixture-id"}},
		privread.Firmware:   firmware.Snapshot{Availability: domain.Available, Devices: []firmware.Device{{Version: "test-version"}}},
		privread.Filesystem: filesystem.Snapshot{Availability: domain.Available, Volumes: []filesystem.Volume{{Filesystem: "ext4"}}},
	}
	var operations []privread.Operation
	for op := range replies {
		operations = append(operations, op)
	}
	session := privread.New(privread.Backend{Operations: operations, Start: func(_ context.Context, scope privread.Scope, _ *os.File, _ io.Writer) (io.ReadWriteCloser, error) {
		ui, peer := net.Pipe()
		go func() {
			defer peer.Close()
			enc, dec := json.NewEncoder(peer), json.NewDecoder(peer)
			_ = enc.Encode(map[string]any{"scope": scope, "until": time.Now().Add(time.Minute)})
			for {
				var req struct {
					ID uint64             `json:"id"`
					Op privread.Operation `json:"op"`
				}
				if dec.Decode(&req) != nil || enc.Encode(map[string]any{"id": req.ID, "op": req.Op, "data": replies[req.Op]}) != nil {
					return
				}
			}
		}()
		return ui, nil
	}}, false)
	defer session.Disable()
	ctx := t.Context()
	if err := session.Enable(ctx, privread.All, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := New(false)
	s.Provider, s.Privilege = noTemperature{}, session
	if o := s.Overview(ctx); o.Temperature.Source != "fixture" || o.Temperature.Celsius == nil || *o.Temperature.Celsius != 43 {
		t.Fatalf("temperature not merged: %+v", o.Temperature)
	}
	if s.SMART(ctx, true).Availability != domain.Partial || s.TRIM(ctx).Mounts[0].Point != "/" || s.Power(ctx).Profiles.Active != "balanced" ||
		s.GPU(ctx).Devices[0].ID != "fixture-id" || s.Firmware(ctx, false).Devices[0].Version != "test-version" || s.Filesystem(ctx).Volumes[0].Filesystem != "ext4" {
		t.Fatal("approved collector did not return the typed reply")
	}
	if session.State().Reads != 7 {
		t.Fatalf("unexpected number of authorized reads: %+v", session.State())
	}
	session.Disable()
	if temp := s.Overview(ctx).Temperature; temp.Availability != domain.PermissionRequired || temp.Celsius != nil {
		t.Fatalf("ordinary mode reused the privileged temperature: %+v", temp)
	}
}

func TestDemoDoesNotUseNativeRunner(t *testing.T) {
	ctx := context.Background()
	s := New(true)
	s.runner = nil
	if !reflect.DeepEqual(s.SMART(ctx, false), s.SMART(ctx, true)) || s.TRIM(ctx).Mounts[0].LastTrim == nil {
		t.Fatal("fixture data changed")
	}
	if s.Power(ctx).Profiles.Active != "balanced" || !s.Doctor(ctx).Demo {
		t.Fatal("fixture capability data missing")
	}
}

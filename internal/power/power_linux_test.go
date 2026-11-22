//go:build linux

package power

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

type fixtureRunner struct {
	tools []string
	run   func(file string, args ...string) sysrun.Result
}

func (r fixtureRunner) LookPath(name string) (string, error) {
	if slices.Contains(r.tools, name) {
		return "/usr/bin/" + name, nil
	}
	return "", fs.ErrNotExist
}

func (r fixtureRunner) Run(_ context.Context, file string, args ...string) sysrun.Result {
	return r.run(file, args...)
}

func files(data map[string]string) fstest.MapFS {
	out := make(fstest.MapFS, len(data))
	for k, v := range data {
		out[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return out
}

func TestCPUFreqAndSupplies(t *testing.T) {
	const p = freqRoot + "/policy2/"
	sys := files(map[string]string{
		p + "affected_cpus":                            "2 6\n",
		p + "scaling_driver":                           "intel_pstate\n",
		p + "scaling_governor":                         "powersave\n",
		p + "scaling_available_governors":              "performance powersave\n",
		p + "cpuinfo_cur_freq":                         "1500000\n",
		p + "cpuinfo_avg_freq":                         "1350000\n",
		p + "scaling_cur_freq":                         "2600000\n",
		p + "scaling_min_freq":                         "800000\n",
		p + "scaling_max_freq":                         "3000000\n",
		p + "cpuinfo_min_freq":                         "500000\n",
		p + "cpuinfo_max_freq":                         "4200000\n",
		p + "bios_limit":                               "3400000\n",
		p + "boost":                                    "1\n",
		p + "energy_performance_preference":            "balance_power\n",
		p + "energy_performance_available_preferences": "performance balance_power\n",
		freqRoot + "/policy10/scaling_driver":          "intel_pstate\n",
		freqRoot + "/policy10/scaling_cur_freq":        "invalid\n",
		freqRoot + "/boost":                            "1\n",
		cpuRoot + "/intel_pstate/no_turbo":             "1\n",
		cpuRoot + "/cpu2/power/energy_perf_bias":       "6\n",
		acpiRoot + "/platform_profile":                 "balanced\n",
		acpiRoot + "/platform_profile_choices":         "low-power balanced performance\n",
		powerRoot + "/BAT0/type":                       "Battery\n",
		powerRoot + "/BAT0/status":                     "Discharging\n",
		powerRoot + "/BAT0/capacity":                   "64\n",
		powerRoot + "/USB-C/type":                      "USB_PD\n",
		powerRoot + "/USB-C/online":                    "0\n",
		powerRoot + "/accessory/type":                  "Battery\n",
		powerRoot + "/accessory/scope":                 "Device\n",
		powerRoot + "/accessory/status":                "Charging\n",
	})
	ctx := context.Background()
	got := Scan(ctx, sys, fixtureRunner{})
	if got.CPUFreqStatus != domain.Partial || len(got.Policies) != 2 || got.Policies[0].Name != "policy2" ||
		got.Policies[1].DriverKHz != nil || got.Boost == nil || *got.Boost ||
		got.BoostSource != cpuRoot+"/intel_pstate/no_turbo" || len(got.EnergyBias) != 1 || got.EnergyBias[0] != (EnergyBias{"cpu2", 6}) {
		t.Fatalf("unexpected frequency availability: %+v", got)
	}
	policy := got.Policies[0]
	if policy.Driver != "intel_pstate" || policy.Governor != "powersave" || !slices.Equal(policy.CPUs, []string{"2", "6"}) ||
		!slices.Equal(policy.Governors, []string{"performance", "powersave"}) || policy.EPP != "balance_power" ||
		!slices.Equal(policy.EPPChoices, []string{"performance", "balance_power"}) || policy.Boost == nil || !*policy.Boost {
		t.Fatalf("unexpected CPUFreq policy: %+v", policy)
	}
	for name, pair := range map[string]struct {
		got  *uint64
		want uint64
	}{
		"observed": {policy.ObservedKHz, 1500000}, "average": {policy.AverageKHz, 1350000},
		"driver": {policy.DriverKHz, 2600000}, "config min": {policy.ConfigMinKHz, 800000},
		"config max": {policy.ConfigMaxKHz, 3000000}, "hardware min": {policy.HardwareMinKHz, 500000},
		"hardware max": {policy.HardwareMaxKHz, 4200000}, "firmware": {policy.FirmwareCapKHz, 3400000},
	} {
		if pair.got == nil || *pair.got != pair.want {
			t.Errorf("%s frequency = %v, expected %d kHz", name, pair.got, pair.want)
		}
	}
	if got.PowerStatus != domain.Available || got.PowerSource != "battery" || len(got.Batteries) != 1 ||
		got.Batteries[0].Percent == nil || *got.Batteries[0].Percent != 64 || got.PlatformProfile != "balanced" ||
		!slices.Equal(got.PlatformChoices, []string{"low-power", "balanced", "performance"}) {
		t.Fatalf("unexpected power information: %+v", got)
	}
	sys[powerRoot+"/USB-C/online"].Data = []byte("1\n")
	sys[powerRoot+"/BAT0/capacity"].Data = []byte("104\n")
	source, batteries, status := supplies(ctx, sys)
	if source != "ac" || status != domain.Partial || len(batteries) != 1 || batteries[0].Percent != nil {
		t.Fatalf("AC with incomplete battery fields = %s %s %+v", source, status, batteries)
	}
}

const testBusID = "00112233445566778899aabbccddeeff"
const testOwner = ":1.12"

const profileResponse = `{"type":"a{sv}","data":[{
	"ActiveProfile":{"type":"s","data":"balanced"},
	"PerformanceDegraded":{"type":"s","data":"lap-detected\u001b"},
	"PerformanceInhibited":{"type":"s","data":""},
	"Profiles":{"type":"aa{sv}","data":[
		{"Profile":{"type":"s","data":"power-saver"},"Driver":{"type":"s","data":"amd_pstate"}},
		{"Profile":{"type":"s","data":"balanced"},"Driver":{"type":"s","data":"amd_pstate"}},
		{"Profile":{"type":"s","data":"performance"},"CpuDriver":{"type":"s","data":"amd_pstate"},
		 "PlatformDriver":{"type":"s","data":"platform_profile"}}
	]},
	"ActiveProfileHolds":{"type":"aa{sv}","data":[]}
}]}`

func knownManagerUnits(ppdPID int, active ...string) string {
	var out strings.Builder
	for _, pair := range managerUnits {
		load, state, pid := "not-found", "inactive", 0
		if pair.manager == profileDaemon && ppdPID != 0 {
			load, state, pid = "loaded", "active", ppdPID
		}
		if slices.Contains(active, pair.unit) {
			load, state = "loaded", "active"
		}
		fmt.Fprintf(&out, "MainPID=%d\nId=%s\nLoadState=%s\nActiveState=%s\n\n", pid, pair.unit, load, state)
	}
	return out.String()
}

func testProfileRunner(t *testing.T, systemd, daemonData, api string, ownerPID int) fixtureRunner {
	t.Helper()
	return fixtureRunner{tools: []string{"busctl", "systemctl", "powerprofilesctl"}, run: func(file string, args ...string) sysrun.Result {
		if file == "/usr/bin/systemctl" {
			if len(args) != len(managerUnits)+4 || args[0] != "show" {
				t.Fatalf("unapproved manager probe: %v", args)
			}
			return sysrun.Result{Output: []byte(systemd)}
		}
		if file != "/usr/bin/busctl" || len(args) < 9 ||
			!slices.Equal(args[:5], []string{busAddress, "--auto-start=no", "--allow-interactive-authorization=no", "--json=short", "call"}) {
			t.Fatalf("unapproved probe: %s %v", file, args)
		}
		var reply string
		switch args[8] {
		case "GetId":
			reply = fmt.Sprintf(`{"type":"s","data":[%q]}`, testBusID)
		case "GetNameOwner":
			if args[10] != api {
				return sysrun.Result{ExitCode: 1, Stderr: []byte("no such name")}
			}
			reply = fmt.Sprintf(`{"type":"s","data":[%q]}`, testOwner)
		case "GetAll":
			if args[5] != testOwner || args[6] != profileObject(api) || args[7] != propertiesAPI || args[10] != api {
				t.Fatalf("unexpected profile call: %v", args)
			}
			reply = daemonData
		case "GetConnectionUnixProcessID":
			reply = fmt.Sprintf(`{"type":"u","data":[%d]}`, ownerPID)
		default:
			t.Fatalf("unapproved bus method: %v", args)
		}
		return sysrun.Result{Output: []byte(reply)}
	}}
}

func TestInspectAndBuildProfileOperation(t *testing.T) {
	for _, api := range []string{currentAPI, legacyAPI} {
		t.Run(api, func(t *testing.T) {
			sys := files(map[string]string{"proc/1/comm": "systemd\n", "proc/25/comm": "power-profiles-\n"})
			p := InspectProfiles(context.Background(), sys, testProfileRunner(t, knownManagerUnits(25), profileResponse, api, 25))
			if !p.CanChange() || p.Active != "balanced" || p.Target != testBusID+"/"+testOwner ||
				p.Degraded != "lap-detected" || !slices.Equal(p.Drivers["performance"], []string{"amd_pstate", "platform_profile"}) {
				t.Fatalf("unexpected backend: %+v", p)
			}
			args, err := ProfileCommand(p, "power-saver")
			want := []string{busAddress, "--auto-start=no", "--allow-interactive-authorization=no", "call", testOwner,
				profileObject(api), propertiesAPI, "Set", "ssv", api, "ActiveProfile", "s", "power-saver"}
			if err != nil || !slices.Equal(args, want) {
				t.Fatalf("unexpected mutation arguments: %v, %v", args, err)
			}
			if _, err := ProfileCommand(p, "--help"); err == nil {
				t.Fatal("accepted command option as profile")
			}
			p.Inhibited = "thermal"
			if _, err := ProfileCommand(p, "performance"); err == nil {
				t.Fatal("accepted the inhibited performance profile")
			}
			p.Target = testBusID + "/--address=unix:path=/tmp/bus"
			if _, err := ProfileCommand(p, "balanced"); err == nil {
				t.Fatal("accepted an injected bus destination")
			}
		})
	}
}

func TestProfileGuards(t *testing.T) {
	held := strings.Replace(profileResponse,
		`"ActiveProfileHolds":{"type":"aa{sv}","data":[]}`,
		`"ActiveProfileHolds":{"type":"aa{sv}","data":[{"ApplicationId":{"type":"s","data":"private.hold"}}]}`, 1)
	for _, tt := range []struct {
		name       string
		opts       map[string]string
		systemd    string
		data       string
		pid        int
		wantStatus domain.Availability
		conflict   string
	}{
		{"active TuneD", nil, knownManagerUnits(25, "tuned.service"), profileResponse, 25, domain.Available, "TuneD"},
		{"TLP ran this boot", map[string]string{"run/tlp/last_pwr": "1"}, knownManagerUnits(25), profileResponse, 25, domain.Available, "TLP"},
		{"auto-cpufreq process", map[string]string{"proc/90/comm": "auto-cpufreq"}, knownManagerUnits(25), profileResponse, 25, domain.Available, "auto-cpufreq"},
		{"another provider owns API", nil, knownManagerUnits(25), profileResponse, 99, domain.Available, ""},
		{"incomplete manager inspection", nil, "Id=power-profiles-daemon.service\n", profileResponse, 25, domain.Available, ""},
		{"held profile", nil, knownManagerUnits(25), held, 25, domain.Available, ""},
		{"bad JSON", nil, knownManagerUnits(25), "malformed", 25, domain.Error, ""},
		{"unknown active profile", nil, knownManagerUnits(25), strings.Replace(profileResponse, `"data":"balanced"`, `"data":"turbo-custom"`, 1), 25, domain.Error, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sys := files(tt.opts)
			sys["proc/1/comm"] = &fstest.MapFile{Data: []byte("systemd\n")}
			p := InspectProfiles(context.Background(), sys, testProfileRunner(t, tt.systemd, tt.data, currentAPI, tt.pid))
			if p.Status != tt.wantStatus || p.CanChange() || (tt.conflict != "" && !slices.Contains(p.Conflicts, tt.conflict)) {
				t.Fatalf("profiles = %+v", p)
			}
			if _, err := ProfileCommand(p, "balanced"); err == nil {
				t.Fatal("change accepted with unverified profile state")
			}
			report, err := json.Marshal(p)
			if err != nil || strings.Contains(string(report), "private.hold") || strings.Contains(string(report), testBusID) {
				t.Fatalf("private values leaked: %s, %v", report, err)
			}
		})
	}
}

func TestMissingOrRestrictedBackend(t *testing.T) {
	ctx := context.Background()
	sys := files(map[string]string{"proc/1/comm": "init\n", "proc/24/comm": "tuned\n"})
	p := InspectProfiles(ctx, sys, testProfileRunner(t, "", "", "", 0))
	if p.Status != domain.Unsupported || p.ManagerStatus != domain.Partial || !slices.Equal(p.Managers, []string{"TuneD"}) {
		t.Fatalf("non-systemd manager not recognized: %+v", p)
	}
	p = InspectProfiles(ctx, sys, fixtureRunner{})
	if p.Status != domain.DependencyMissing {
		t.Fatalf("missing busctl = %+v", p)
	}
	p = InspectProfiles(ctx, sys, fixtureRunner{tools: []string{"busctl"}, run: func(string, ...string) sysrun.Result {
		return sysrun.Result{Err: errors.Join(fs.ErrPermission, errors.New("bus unavailable"))}
	}})
	if p.Status != domain.PermissionRequired {
		t.Fatalf("restricted bus = %+v", p)
	}
}

package report

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

func TestFormatsShareRedactedMetricsAndOutcome(t *testing.T) {
	const (
		counter   = "340282366920938463463374607431768211455"
		home      = "/home/private-user/work-item"
		proc      = "owner-private-process"
		serial    = "secret-serial-1234"
		bus       = "private-bus-host/:1.12"
		mount     = "/media/sensitive-volume"
		driver    = "private-host-driver"
		host      = "private-host"
		privilege = "sudo-protected-secret"
		receipt   = "private-action-receipt"
	)
	at := time.Date(2026, 11, 22, 9, 41, 0, 0, time.UTC)
	o := domain.Overview{
		Availability: domain.Available, CapturedAt: at, OS: "linux", Distro: "Ubuntu 24.04.4 LTS " + host,
		DistroID: "ubuntu", Kernel: "6.17.0-23-generic-" + host, CPUModel: serial, Arch: "amd64", LogicalCPUs: 4,
		CPU:         domain.Percent{Availability: domain.Available, CapturedAt: at, Samples: []float64{5, 6, 7}},
		Temperature: domain.Temperature{Availability: domain.Unknown, Celsius: new(math.NaN())},
		Disks:       []domain.Disk{{Mount: home, Root: true, FSType: "ext4", Availability: domain.Available, TotalBytes: 1000}},
		Processes:   []domain.Process{{Name: proc, CPU: 21, RSS: 2048}},
	}
	s := storage.SMARTAudit{Availability: domain.Partial, CapturedAt: at, Drives: []storage.Drive{{
		Availability: domain.PermissionRequired, CapturedAt: at, Name: home, Model: serial, DeviceID: "8:0", Transport: "NVMe",
		Health: &storage.SMART{MediaErrors: counter, PercentageUsed: new(uint64(130)),
			ATAAttributes: []storage.ATAAttribute{{ID: 9, Name: "Alice-serial", Raw: "17"}}},
	}}}
	tr := storage.TrimAudit{Availability: domain.Available, CapturedAt: at,
		Timer: storage.Timer{Availability: domain.Available, FragmentPath: home, Scheduler: "systemd", Detail: host, UnitFileState: "disabled"},
		Mounts: []storage.Mount{{ID: 45, DeviceID: "8:1", Point: mount, Source: "/dev/mapper/private-user-home", FSType: "ext4",
			Discard: domain.Available, Layers: []storage.BlockLayer{{Name: "private-mapper", MaxDiscardBytes: new(uint64(8192))}},
			LastTrim: &storage.TrimRecord{RecordedAt: at, PotentialDiscardBytes: 300}}}}
	p := power.Snapshot{At: at, CPUFreqStatus: domain.Available, Policies: []power.Policy{{Name: "policy0", Driver: driver,
		ObservedKHz: new(uint64(2400000)), DriverKHz: new(uint64(2600000))}},
		Profiles: power.Profiles{Status: domain.Available, Target: bus, Active: "balanced", Available: []string{"balanced"}}}
	a := diagnostics.Analyzer{Now: at}
	findings := append(a.Overview(o), a.Power(p)...)
	findings = append(findings, diagnostics.Finding{
		ID: "trim.mount.1", Category: "trim", Availability: domain.Available, Observation: "hostname=" + host + " " + home,
		Source: home, At: at, Actions: []diagnostics.Action{{Kind: actions.TrimMount, Target: mount, Label: mount}},
	})
	action := actions.Result{
		Plan: actions.Plan{Kind: actions.CleanAPT, Target: mount, Operation: []string{privilege}}, Status: actions.Partial,
		Started: true, FinishedAt: at, Receipt: receipt, RemovedBytes: new(uint64(300)), FreeSpaceDelta: new(int64(-100)),
	}
	report := Build(Input{
		At: at, Overview: &o, SMART: &s, Trim: &tr, Power: &p, Findings: findings, Actions: []actions.Result{action},
	})
	for _, format := range []string{"json", "markdown"} {
		raw, err := Encode(report, format)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{"private-user", home, proc, serial, bus, mount, host, privilege, receipt, "Alice-serial", "private-mapper"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s exposed %q", format, forbidden)
			}
		}
		for _, want := range []string{counter, "Ubuntu 24.04.4", "2400000", "2600000", "clean-apt", "300", "-100", "130"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s omits %q", format, want)
			}
		}
		if format == "json" {
			var parsed Report
			if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Source != "live" || parsed.SchemaVersion != 1 ||
				parsed.SMART.Drives[0].Health.MediaErrors != counter || parsed.SMART.Drives[0].Availability != domain.PermissionRequired {
				t.Fatalf("schema did not round trip: %+v, %v", parsed, err)
			}
		}
	}
	if o.CPUModel != serial || p.Policies[0].Driver != driver || tr.Mounts[0].Point != mount {
		t.Fatal("reporting changed live telemetry")
	}
}

func TestDemoIsLabeledAndOmitsLiveActions(t *testing.T) {
	r := Build(Input{Demo: true, Actions: []actions.Result{{Plan: actions.Plan{Kind: actions.CleanAPT}, Status: actions.Verified}}})
	raw, err := Encode(r, "json")
	if err != nil || !strings.Contains(string(raw), `"source": "demo"`) || len(r.Actions) != 0 {
		t.Fatalf("demo report %s, err %v", raw, err)
	}
	markdown, err := Encode(r, "markdown")
	if err != nil || !strings.Contains(string(markdown), "**DEMO**") || strings.Contains(string(markdown), "Hasil tindakan") {
		t.Fatalf("demo markdown %s, err %v", markdown, err)
	}
	if _, err := Encode(r, "html"); err == nil {
		t.Fatal("accepted unknown export format")
	}
}

func TestNativeMarkdownUsesReportedPowerAndStorageEvidence(t *testing.T) {
	p := power.Snapshot{OS: "windows", PowerSource: "ac", PowerStatus: domain.Available, CPUFreqStatus: domain.Unsupported,
		Batteries: []power.Battery{{Name: "private-battery-label", Status: "Not charging", Percent: new(uint64(0))}}}
	trim := storage.TrimAudit{Source: storage.WindowsTRIMSource, Availability: domain.Partial,
		Policy: []storage.TrimPolicy{{Scope: "ntfs", Enabled: true}}}
	smart := storage.SMARTAudit{Source: storage.WindowsStorageSource, Availability: domain.Partial,
		Drives: []storage.Drive{{HealthStatus: "warning", Availability: domain.Partial, Health: &storage.SMART{ReadErrorsUncorrected: "2"}}}}
	raw, err := Encode(Build(Input{Power: &p, Trim: &trim, SMART: &smart}), "markdown")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Baterai 1** · 0% · not charging", "Notifikasi penghapusan NTFS", "Kesehatan yang dilaporkan** · warning", "Galat baca tak terkoreksi** · 2"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %q in native report\n%s", want, raw)
		}
	}
	for _, unwanted := range []string{"CPUFreq", "Scheduler", "exit mask", "private-battery-label"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("native report emitted %q\n%s", unwanted, raw)
		}
	}
}

func TestProcessorGroupScopeInExports(t *testing.T) {
	o := domain.Overview{OS: "windows", LogicalCPUs: 128,
		CPU: domain.Percent{Availability: domain.Partial, Scope: domain.PrimaryProcessorGroup, Samples: []float64{98}}}
	projected := Build(Input{Overview: &o})
	for format, want := range map[string]string{
		"json":     `"scope": "primary_processor_group"`,
		"markdown": "Sampel CPU grup prosesor primer** · 98.0%",
	} {
		data, err := Encode(projected, format)
		if err != nil || !strings.Contains(string(data), want) {
			t.Fatalf("%s lost the scope of the CPU sample: %s (%v)", format, data, err)
		}
	}
	o.CPU.Scope = ""
	data, err := Encode(Build(Input{Overview: &o}), "json")
	if err != nil || strings.Contains(string(data), `"scope":`) {
		t.Fatalf("whole-host CPU sample must preserve the previous format: %s (%v)", data, err)
	}
}

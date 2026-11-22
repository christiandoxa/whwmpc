package diagnostics

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
	"github.com/christiandoxa/whwmpc/internal/storage"
)

var fixtureTime = time.Date(2026, 4, 15, 11, 30, 0, 0, time.UTC)

func overviewFixture() domain.Overview {
	return domain.Overview{
		Availability: domain.Available, CapturedAt: fixtureTime,
		CPU: domain.Percent{Availability: domain.Available, CapturedAt: fixtureTime, Samples: []float64{20, 31, 25}},
		Memory: domain.Memory{
			Availability: domain.Available, TotalBytes: 16 << 30, FreeBytes: 10 << 30,
			SwapBytes: 2 << 30, SwapUsed: 172 << 20,
			Use: domain.Percent{Availability: domain.Available, CapturedAt: fixtureTime, Samples: []float64{36, 38, 37}},
		},
		DiskStatus: domain.Available,
		Disks: []domain.Disk{{
			Availability: domain.Available, Mount: "/home/private-person/private-space", Root: true,
			FSType: "ext4", TotalBytes: 100 << 30, FreeBytes: 30 << 30, UsedPercent: 70,
		}},
		ProcessStatus: domain.Available,
		Processes:     []domain.Process{{Name: "private-worker", CPU: 16, RSS: 430 << 20}},
		Temperature:   domain.Temperature{Availability: domain.Available, Celsius: new(48.0)},
		PowerStatus:   domain.Available, OnAC: new(true), Battery: new(71.0),
	}
}

func powerFixture() power.Snapshot {
	return power.Snapshot{
		At: fixtureTime, CPUFreqStatus: domain.Available,
		Policies: []power.Policy{{
			ObservedKHz: new(uint64(1200000)), DriverKHz: new(uint64(1800000)),
			ConfigMaxKHz: new(uint64(4100000)), HardwareMaxKHz: new(uint64(4100000)),
			Driver: "intel_pstate", Governor: "powersave",
		}},
		PowerStatus: domain.Available, PowerSource: "ac",
		Profiles: power.Profiles{
			Status: domain.Available, ManagerStatus: domain.Available, Holds: new(0),
			Backend: "power-profiles-daemon", Active: "balanced",
			Target: "0123456789abcdef0123456789abcdef/abcdef0123456789abcdef0123456789/:1.42", API: "org.freedesktop.UPower.PowerProfiles",
			Available: []string{"power-saver", "balanced", "performance"}, Managers: []string{"power-profiles-daemon"},
		},
	}
}

func byID(t *testing.T, findings []Finding, id string) Finding {
	t.Helper()
	for _, f := range findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("finding %s missing in %+v", id, findings)
	return Finding{}
}

func hasAction(f Finding, kind, target string) bool {
	return slices.ContainsFunc(f.Actions, func(a Action) bool { return a.Kind == kind && a.Target == target })
}

func TestOverviewCorrelatesPressureAcrossSamples(t *testing.T) {
	o := overviewFixture()
	a := Analyzer{Now: fixtureTime, Language: "en"}
	for _, f := range a.Overview(o) {
		if f.Severity != Info || f.Availability != domain.Available {
			t.Fatalf("healthy fixture: %+v", f)
		}
	}
	o.CPU.Samples = []float64{95, 96, 92}
	o.Memory.Use.Samples = []float64{96, 95, 94}
	o.Memory.FreeBytes = 1 << 30
	o.Processes[0].CPU = 56
	o.Disks[0].UsedPercent, o.Disks[0].FreeBytes = 99, 1<<30
	findings := a.Slow(o, powerFixture())
	if findings[0].ID != "overview.disk.1" || findings[0].Severity != Critical ||
		!hasAction(findings[0], "navigate", string(assistant.Cleanup)) {
		t.Fatalf("low space priority: %+v", findings)
	}
	for _, id := range []string{"overview.cpu", "overview.memory", "overview.process"} {
		if f := byID(t, findings, id); f.Severity != Warning {
			t.Fatalf("missing repeated pressure: %+v", f)
		}
	}
	data, err := json.Marshal(findings)
	if err != nil || strings.Contains(string(data), "private-") {
		t.Fatalf("finding exposes raw path or process name: %s (%v)", data, err)
	}
	o.CPU.Samples, o.Memory.Use.Samples = []float64{99}, []float64{99}
	findings = a.Overview(o)
	for _, id := range []string{"overview.cpu", "overview.memory", "overview.process"} {
		if f := byID(t, findings, id); f.Severity != Info {
			t.Fatalf("one sample was treated as pressure: %+v", f)
		}
	}
}

func TestOverviewRetainsUnknownPartialAndStale(t *testing.T) {
	a := Analyzer{Now: fixtureTime.Add(time.Second), Language: "id"}
	o := overviewFixture()
	o.CPU.CapturedAt = fixtureTime.Add(-time.Minute)
	o.Memory.Use.Samples = []float64{99, math.NaN(), 99}
	o.Temperature.Availability = domain.PermissionRequired
	o.DiskStatus = domain.Unsupported
	o.Processes = nil
	findings := a.Overview(o)
	for id, status := range map[string]domain.Availability{
		"overview.cpu": domain.Stale, "overview.temperature": domain.PermissionRequired,
		"overview.disk": domain.Unsupported, "overview.process": domain.Unknown, "overview.memory": domain.Available,
	} {
		f := byID(t, findings, id)
		if f.Availability != status || f.Severity != Info || f.Limits == "" {
			t.Fatalf("unexpected status: %+v", f)
		}
	}
	if f := byID(t, findings, "overview.cpu"); !strings.Contains(f.Observation, "kedaluwarsa") ||
		!hasAction(f, "rescan", "") || strings.Contains(f.Observation, "%") {
		t.Fatalf("stale sample rendered as current: %+v", f)
	}
}

func TestSMARTPrioritizesBackupAndPreservesEndurance(t *testing.T) {
	a := Analyzer{Now: fixtureTime, Language: "en"}
	audit := storage.SMARTAudit{Availability: domain.Partial, CapturedAt: fixtureTime, Drives: []storage.Drive{
		{Availability: domain.Available, CapturedAt: fixtureTime, Name: "private-device", Health: &storage.SMART{
			Passed: new(true), CriticalWarning: new(uint64(0)), PercentageUsed: new(uint64(125)),
			Spare: new(uint64(99)), SpareThreshold: new(uint64(255)),
		}},
		{Availability: domain.Partial, CapturedAt: fixtureTime, Model: "private-model", Health: &storage.SMART{
			ExitCode: storage.SMARTDiskFailing | storage.SMARTCommandFailed, CriticalWarning: new(uint64(4)),
			MediaErrors: "18446744073709551617",
		}},
		{Availability: domain.PermissionRequired, CapturedAt: fixtureTime, Health: &storage.SMART{Passed: new(true)}},
		{Availability: domain.Available, CapturedAt: fixtureTime, Health: &storage.SMART{ExitCode: 0}},
		{Availability: domain.Available, CapturedAt: fixtureTime.Add(-time.Hour), Health: &storage.SMART{Passed: new(true)}},
	}}
	findings := a.SMART(audit)
	if f := findings[0]; f.ID != "smart.status.2" || f.Severity != Critical ||
		f.Availability != domain.Partial || !strings.Contains(f.Recommendation, "backing up") {
		t.Fatalf("critical SMART not prioritized: %+v", f)
	}
	if f := byID(t, findings, "smart.endurance.1"); f.Severity != Warning || !strings.Contains(f.Observation, "125%") {
		t.Fatalf("vendor endurance lost or treated as failure: %+v", f)
	}
	if f := byID(t, findings, "smart.status.1"); f.Severity != Info {
		t.Fatalf("reserved spare threshold interpreted as critical: %+v", f)
	}
	if f := byID(t, findings, "smart.counters.2"); !strings.Contains(f.Observation, "18446744073709551617") {
		t.Fatalf("counter precision lost: %+v", f)
	}
	for id, status := range map[string]domain.Availability{
		"smart.status.3": domain.PermissionRequired, "smart.status.4": domain.Partial, "smart.status.5": domain.Stale,
	} {
		f := byID(t, findings, id)
		if f.Availability != status || strings.Contains(f.Observation, "SMART passed") {
			t.Fatalf("unobserved SMART treated as passed: %+v", f)
		}
	}
	data, err := json.Marshal(findings)
	if err != nil || strings.Contains(string(data), "private-") {
		t.Fatalf("raw device identity exposed: %s (%v)", data, err)
	}
}

func TestTRIMUsesPerFilesystemEvidence(t *testing.T) {
	a := Analyzer{Now: fixtureTime, Language: "en"}
	audit := storage.TrimAudit{
		Availability: domain.Available, CapturedAt: fixtureTime, Manual: domain.Available,
		Timer: storage.Timer{Availability: domain.Available, Journal: domain.Available, Scheduler: "systemd",
			ActiveState: "active", UnitFileState: "disabled", ServiceResult: "success", NextElapse: "Thu 2026-04-16 11:30:00 UTC"},
		Mounts: []storage.Mount{
			{ID: 25, Discard: domain.Available, Point: "/home/private-person", FSType: "ext4"},
			{ID: 93, Discard: domain.Available, Point: "/private-mount", FSType: "xfs",
				LastTrim: &storage.TrimRecord{RecordedAt: fixtureTime.Add(-time.Hour), PotentialDiscardBytes: 8388608}},
			{ID: 99, Discard: domain.Unsupported, ReadOnly: true, Point: "/private-mount-readonly", FSType: "vfat"},
		},
	}
	findings := a.TRIM(audit)
	timer := byID(t, findings, "trim.scheduler")
	if !strings.Contains(timer.Observation, "boot disabled · current active") ||
		!strings.Contains(timer.Observation, "Next Thu") || strings.Contains(timer.Observation, "Service result success") {
		t.Fatalf("mistaken timer and result semantics: %+v", timer)
	}
	first := byID(t, findings, "trim.mount.1")
	if !strings.Contains(first.Observation, "evidence is unavailable") || !hasAction(first, "trim-mount", "25") {
		t.Fatalf("missing separate capability and proof: %+v", first)
	}
	second := byID(t, findings, "trim.mount.2")
	if !strings.Contains(second.Observation, "8388608 potential discard bytes") ||
		!strings.Contains(second.Limits, "separately by statfs") || !hasAction(second, "trim-mount", "93") {
		t.Fatalf("per mount trim proof: %+v", second)
	}
	if f := byID(t, findings, "trim.mount.3"); len(f.Actions) > 0 || f.Availability != domain.Unsupported {
		t.Fatalf("read-only mount advertised as actionable: %+v", f)
	}
	data, err := json.Marshal(findings)
	if err != nil || strings.Contains(string(data), "private-") {
		t.Fatalf("raw mount identity leaked: %s (%v)", data, err)
	}
}

func TestPowerReportsCapsAndConflictsSeparately(t *testing.T) {
	a := Analyzer{Now: fixtureTime, Language: "en"}
	p := powerFixture()
	p.Policies[0].ConfigMaxKHz = new(uint64(2000000))
	p.Profiles.Managers = append(p.Profiles.Managers, "TLP")
	p.Profiles.Conflicts = []string{"TLP"}
	p.Profiles.Degraded = "high-operating-temperature"
	p.Profiles.Inhibited = "lap-detected"
	findings := a.Power(p)
	f := byID(t, findings, "power.cpufreq.1")
	if f.Severity != Warning || !strings.Contains(f.Observation, "driver reading 1.80 GHz") ||
		!strings.Contains(f.Observation, "observed 1.20 GHz") || !strings.Contains(f.Observation, "configured max 2.00 GHz") {
		t.Fatalf("frequency categories mixed: %+v", f)
	}
	conflict := byID(t, findings, "power.manager-conflict")
	if conflict.Severity != Warning || len(conflict.Actions) != 0 || !strings.Contains(conflict.Observation, "TLP") ||
		!strings.Contains(conflict.Observation, "lap use") || !strings.Contains(conflict.Observation, "high operating temperature") {
		t.Fatalf("conflict or backend restrictions lost: %+v", conflict)
	}
	advice := a.Recommend(overviewFixture(), p, assistant.Build)[0]
	if advice.ID != "recommend.conflict" || hasAction(advice, "power-profile", "performance") {
		t.Fatalf("profile action allowed despite conflict: %+v", advice)
	}
}

func TestRecommendationMatchesWorkloadAndCapabilities(t *testing.T) {
	t.Run("asks context once", func(t *testing.T) {
		a := Analyzer{Now: fixtureTime, Language: "id"}
		f := a.Recommend(overviewFixture(), powerFixture(), "")[0]
		if f.ID != "recommend.context" || len(f.Actions) != 5 || !hasAction(f, "workload", "daily") {
			t.Fatalf("context prompt missing choices: %+v", f)
		}
	})
	tests := []struct {
		name    string
		work    assistant.Workload
		change  func(*domain.Overview, *power.Snapshot)
		profile string
		preview bool
	}{
		{"AC heavy workload", assistant.Build, nil, "performance", true},
		{"AC daily", assistant.Daily, nil, "balanced", false},
		{"battery goal", assistant.Battery, nil, "power-saver", true},
		{"AC quiet", assistant.Quiet, nil, "power-saver", true},
		{"low battery", assistant.Gaming, func(_ *domain.Overview, p *power.Snapshot) {
			p.PowerSource = "battery"
			p.Batteries = []power.Battery{{Percent: new(uint64(14))}}
		}, "power-saver", true},
		{"warm device", assistant.Build, func(o *domain.Overview, _ *power.Snapshot) { o.Temperature.Celsius = new(89.0) }, "power-saver", true},
		{"missing temperature", assistant.Build, func(o *domain.Overview, _ *power.Snapshot) { o.Temperature.Celsius = nil }, "balanced", false},
		{"stale temperature", assistant.Gaming, func(o *domain.Overview, _ *power.Snapshot) { o.CapturedAt = fixtureTime.Add(-time.Minute) }, "balanced", false},
		{"no performance profile", assistant.Build, func(_ *domain.Overview, p *power.Snapshot) {
			p.Profiles.Available = []string{"balanced", "power-saver"}
		}, "balanced", false},
		{"degraded performance", assistant.Build, func(_ *domain.Overview, p *power.Snapshot) {
			p.Profiles.Degraded = "high-operating-temperature"
		}, "balanced", false},
		{"application hold", assistant.Build, func(_ *domain.Overview, p *power.Snapshot) {
			p.Profiles.Holds = new(1)
		}, "balanced", false},
		{"unconfirmed manager state", assistant.Build, func(_ *domain.Overview, p *power.Snapshot) {
			p.Profiles.ManagerStatus = domain.Partial
		}, "balanced", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, p := overviewFixture(), powerFixture()
			if tt.change != nil {
				tt.change(&o, &p)
			}
			f := (Analyzer{Now: fixtureTime, Language: "en"}).Recommend(o, p, tt.work)[0]
			if f.ID != "recommend.profile" || !strings.HasSuffix(f.Observation, " · "+tt.profile) ||
				hasAction(f, "power-profile", tt.profile) != tt.preview {
				t.Fatalf("unexpected recommendation: %+v", f)
			}
		})
	}
}

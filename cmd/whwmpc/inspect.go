package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func readCommand(ctx context.Context, args []string, source *inspection.Scanner, maintenance *workflow.Maintenance, out, stderr io.Writer) (int, bool) {
	if len(args) == 0 || len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
		return 0, false
	}
	asJSON := len(args) == 2
	demo := maintenance.Demo
	if demo && !asJSON && slices.Contains([]string{"doctor", "power", "trim"}, args[0]) {
		fmt.Fprintln(out, "DEMO")
	}
	switch args[0] {
	case "doctor":
		d := source.Doctor(ctx)
		cleanup, _ := maintenance.Cleanup(ctx)
		d.Capabilities["package_cache_cleanup"] = cleanup.Availability
		d.Capabilities["apt_cache_cleanup"] = domain.Unsupported
		if cleanup.Kind == "" || cleanup.Kind == actions.CleanAPT {
			d.Capabilities["apt_cache_cleanup"] = cleanup.Availability
		}
		switch {
		case demo || d.Overview.OS != "linux":
			d.Capabilities["trim_manual_action"] = domain.Unsupported
		default:
			_, err := actions.New().TrimTargets(ctx)
			switch {
			case err == nil:
				d.Capabilities["trim_manual_action"] = domain.Available
			case errors.Is(err, fs.ErrNotExist):
				d.Capabilities["trim_manual_action"] = domain.DependencyMissing
			case errors.Is(err, fs.ErrPermission):
				d.Capabilities["trim_manual_action"] = domain.PermissionRequired
			default:
				d.Capabilities["trim_manual_action"] = domain.Unknown
			}
		}
		if asJSON {
			return printJSON(out, stderr, d), true
		}
		o := d.Overview
		fmt.Fprintf(out, "%s %s · %s · %s · systemd=%t\n", o.OS, o.Arch, domain.SafeText(o.Distro), o.Environment, o.Systemd)
		for _, tool := range slices.Sorted(maps.Keys(d.Tools)) {
			fmt.Fprintf(out, "%-18s %s\n", tool, d.Tools[tool])
		}
		for _, key := range slices.Sorted(maps.Keys(d.Capabilities)) {
			fmt.Fprintf(out, "%-18s %s\n", key, d.Capabilities[key])
		}
	case "power":
		p := source.Power(ctx)
		if asJSON {
			return printJSON(out, stderr, struct {
				Demo bool `json:"demo"`
				Data any  `json:"power"`
			}{demo, p}), true
		}
		if !p.LinuxInterfaces() {
			fmt.Fprintln(out, "Status daya", p.PowerStatus)
			for _, f := range (diagnostics.Analyzer{Now: source.Now()}).Power(p) {
				fmt.Fprintln(out, f.Observation)
			}
			return 0, true
		}
		fmt.Fprintf(out, "CPUFreq %s · %d policy · daya %s\n", p.CPUFreqStatus, len(p.Policies), p.PowerSource)
		for _, policy := range p.Policies {
			fmt.Fprintf(out, "%s: %s, %s, EPP %s\n", policy.Name, domain.SafeText(policy.Driver), domain.SafeText(policy.Governor), domain.SafeText(policy.EPP))
			fmt.Fprintf(out, "  observasi %s · driver %s · batas %s–%s kHz · hardware %s–%s kHz\n",
				knownKHz(policy.ObservedKHz), knownKHz(policy.DriverKHz), knownKHz(policy.ConfigMinKHz), knownKHz(policy.ConfigMaxKHz),
				knownKHz(policy.HardwareMinKHz), knownKHz(policy.HardwareMaxKHz))
		}
		fmt.Fprintf(out, "Profil %s · aktif %s · tersedia %s\n", p.Profiles.Status, p.Profiles.Active, strings.Join(p.Profiles.Available, ", "))
		if p.Profiles.Detail != "" {
			fmt.Fprintln(out, domain.SafeText(p.Profiles.Detail))
		}
	case "trim":
		a := source.TRIM(ctx)
		if asJSON {
			return printJSON(out, stderr, struct {
				Demo bool `json:"demo"`
				Data any  `json:"trim"`
			}{demo, a}), true
		}
		if a.Source != "" {
			fmt.Fprintf(out, "Audit TRIM %s · %s\n", a.Availability, a.Source)
			for _, f := range (diagnostics.Analyzer{Now: source.Now()}).TRIM(a) {
				fmt.Fprintln(out, f.Observation)
				fmt.Fprintln(out, f.Limits)
			}
			return 0, true
		}
		fmt.Fprintf(out, "TRIM %s · fstrim %s · jadwal %s (%s/%s)\n",
			a.Availability, a.Manual, a.Timer.Availability, a.Timer.UnitFileState, a.Timer.ActiveState)
		if a.Timer.NextElapse != "" {
			fmt.Fprintln(out, "Berikutnya", domain.SafeText(a.Timer.NextElapse))
		}
		if a.Timer.LastTrigger != "" || a.Timer.LastServiceExit != "" {
			fmt.Fprintf(out, "Trigger terakhir %s · service terakhir %s (%s)\n",
				domain.SafeText(a.Timer.LastTrigger), domain.SafeText(a.Timer.LastServiceExit), domain.SafeText(a.Timer.ServiceResult))
		}
		for _, mount := range a.Mounts {
			fmt.Fprintf(out, "%s (%s) · %s · %s", domain.SafeText(mount.Point), domain.SafeText(mount.FSType), mount.Discard, domain.SafeText(mount.Evidence))
			if mount.LastTrim != nil {
				fmt.Fprintf(out, " · %d B potensi discard pada %s", mount.LastTrim.PotentialDiscardBytes, mount.LastTrim.RecordedAt.Format("2006-01-02"))
			}
			fmt.Fprintln(out)
		}
	default:
		return 0, false
	}
	return 0, true
}

func knownKHz(v *uint64) string {
	if v == nil {
		return "?"
	}
	return fmt.Sprint(*v)
}

func printJSON(out, stderr io.Writer, value any) int {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

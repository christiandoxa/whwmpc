//go:build linux

package power

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const busAPI = "org.freedesktop.DBus"

type busArgument struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (a busArgument) decode(kind string, out any) bool {
	return a.Type == kind && len(a.Data) > 0 && string(a.Data) != "null" && json.Unmarshal(a.Data, out) == nil
}

func decodeCall(raw []byte, kind string, out any) bool {
	var arg busArgument
	var values []json.RawMessage
	return json.Unmarshal(raw, &arg) == nil && arg.decode(kind, &values) &&
		len(values) == 1 && string(values[0]) != "null" && json.Unmarshal(values[0], out) == nil
}

func InspectProfiles(ctx context.Context, fsys fs.FS, runner sysrun.Runner) Profiles {
	managers := inspectManagers(ctx, fsys, runner)
	p := Profiles{Status: domain.Unknown, ManagerStatus: managers.status, Managers: managers.names}
	if len(p.Managers) > 1 {
		for _, manager := range p.Managers {
			if manager != profileDaemon {
				p.Conflicts = append(p.Conflicts, manager)
			}
		}
	}
	busctl, err := runner.LookPath("busctl")
	if err != nil {
		p.Status = domain.DependencyMissing
		p.Detail = "busctl is required to inspect the system power-profile API."
		return p
	}
	call := func(destination, object, iface, method string, args ...string) sysrun.Result {
		cmd := []string{busAddress, "--auto-start=no", "--allow-interactive-authorization=no", "--json=short",
			"call", destination, object, iface, method}
		return runner.Run(ctx, busctl, append(cmd, args...)...)
	}
	busCall := func(method string, args ...string) sysrun.Result {
		return call(busAPI, "/org/freedesktop/DBus", busAPI, method, args...)
	}
	id := busCall("GetId")
	var busID string
	if !okResult(id) || !decodeCall(id.Output, "s", &busID) || profileOwner(busID+"/:1.1") == "" {
		p.Status = callStatus(id)
		p.Detail = "System D-Bus identity could not be read."
		return p
	}
	for _, api := range []string{currentAPI, legacyAPI} {
		res := busCall("GetNameOwner", "s", api)
		if res.Err == nil && res.ExitCode != 0 && nameMissing(res.Stderr) {
			continue
		}
		var owner string
		if !okResult(res) || !decodeCall(res.Output, "s", &owner) || profileOwner(busID+"/"+owner) == "" {
			p.Status = callStatus(res)
			p.Detail = "The owner of the system power-profile API could not be verified."
			return p
		}
		p.Target, p.API = busID+"/"+owner, api
		break
	}
	if p.API == "" {
		p.Status = domain.Unsupported
		p.Detail = "No running service owns the system power-profile API."
		if _, err := runner.LookPath("powerprofilesctl"); err != nil && len(p.Managers) == 0 {
			p.Status = domain.DependencyMissing
			p.Detail = "No running power-profile backend was detected and powerprofilesctl is unavailable."
		}
		return p
	}
	owner := profileOwner(p.Target)
	data := call(owner, profileObject(p.API), propertiesAPI, "GetAll", "s", p.API)
	if !okResult(data) {
		p.Status = callStatus(data)
		p.Detail = "The running profile service did not expose its profile properties."
		return p
	}
	if !readProfiles(data.Output, &p) {
		p.Status = domain.Error
		p.Detail = "The profile service returned malformed or inconsistent properties."
		return p
	}
	p.Status, p.Backend = domain.Available, "profile-api"
	var pid uint64
	process := busCall("GetConnectionUnixProcessID", "s", owner)
	if okResult(process) && decodeCall(process.Output, "u", &pid) && pid > 0 && pid == managers.daemonPID {
		p.Backend = profileDaemon
	}
	switch {
	case len(p.Conflicts) > 0:
		p.Detail = "Review active power managers and choose a single backend before changing profiles."
	case p.Backend != profileDaemon:
		p.Detail = "Use the active manager's controls. Changes require a verified power-profiles-daemon service."
	case p.ManagerStatus != domain.Available:
		p.Detail = "Competing power managers could not be ruled out."
	case p.Holds == nil:
		p.Detail = "The service did not report its active profile holds."
	case *p.Holds > 0:
		p.Detail = "An application holds a power profile. Review again after the hold ends."
	case slices.Contains(p.Drivers[p.Active], "fake"):
		p.Detail = "The daemon reports its test driver. The active profile describes daemon state."
	case slices.Contains(p.Drivers[p.Active], "placeholder"):
		p.Detail = "The daemon reports a placeholder driver. Hardware effects are unverified."
	}
	return p
}

func readProfiles(raw []byte, p *Profiles) bool {
	var props map[string]busArgument
	if !decodeCall(raw, "a{sv}", &props) || !props["ActiveProfile"].decode("s", &p.Active) || !ValidProfile(p.Active) {
		return false
	}
	var profiles []map[string]busArgument
	if !props["Profiles"].decode("aa{sv}", &profiles) {
		return false
	}
	p.Drivers = make(map[string][]string)
	for _, profile := range profiles {
		var name string
		if !profile["Profile"].decode("s", &name) || !ValidProfile(name) || slices.Contains(p.Available, name) {
			return false
		}
		p.Available = append(p.Available, name)
		for _, field := range []string{"CpuDriver", "PlatformDriver", "Driver"} {
			var value string
			if profile[field].decode("s", &value) {
				value = safeText(value)
				if value != "" && !slices.Contains(p.Drivers[name], value) {
					p.Drivers[name] = append(p.Drivers[name], value)
				}
			}
		}
	}
	props["PerformanceDegraded"].decode("s", &p.Degraded)
	props["PerformanceInhibited"].decode("s", &p.Inhibited)
	p.Degraded, p.Inhibited = safeText(p.Degraded), safeText(p.Inhibited)
	var holds []json.RawMessage
	// Hold reasons and application IDs are deliberately omitted from observations.
	if props["ActiveProfileHolds"].decode("aa{sv}", &holds) {
		p.Holds = new(len(holds))
	}
	return slices.Contains(p.Available, p.Active)
}

func okResult(result sysrun.Result) bool {
	return result.Err == nil && result.ExitCode == 0
}

func callStatus(r sysrun.Result) domain.Availability {
	if errors.Is(r.Err, fs.ErrPermission) {
		return domain.PermissionRequired
	}
	message := strings.ToLower(string(r.Stderr))
	if strings.Contains(message, "permission denied") || strings.Contains(message, "access denied") ||
		strings.Contains(message, "not authorized") {
		return domain.PermissionRequired
	}
	if strings.Contains(message, "failed to connect to bus") || strings.Contains(message, "no such file or directory") {
		return domain.Unsupported
	}
	return domain.Error
}

func nameMissing(stderr []byte) bool {
	msg := strings.ToLower(string(stderr))
	return strings.Contains(msg, "no such name") || strings.Contains(msg, "has no owner")
}

type managerObservation struct {
	status    domain.Availability
	names     []string
	daemonPID uint64
}

func (m *managerObservation) add(name string) {
	if !slices.Contains(m.names, name) {
		m.names = append(m.names, name)
	}
}

var managerUnits = []struct{ unit, manager string }{
	{"power-profiles-daemon.service", profileDaemon},
	{"tlp.service", "TLP"},
	{"tlp-pd.service", "TLP"},
	{"tuned.service", "TuneD"},
	{"tuned-ppd.service", "TuneD"},
	{"auto-cpufreq.service", "auto-cpufreq"},
	{"snap.auto-cpufreq.service.service", "auto-cpufreq"},
}

func inspectManagers(ctx context.Context, fsys fs.FS, runner sysrun.Runner) managerObservation {
	m := managerObservation{status: domain.Partial}
	if init, _ := readValue(fsys, "proc/1/comm"); init == "systemd" {
		if systemctl, err := runner.LookPath("systemctl"); err == nil {
			args := []string{"show", "--no-pager", "--property=Id,LoadState,ActiveState,MainPID", "--"}
			for _, known := range managerUnits {
				args = append(args, known.unit)
			}
			res := runner.Run(ctx, systemctl, args...)
			if okResult(res) {
				m = readUnits(string(res.Output))
			}
		}
	}
	// TLP's startup service can exit after applying settings.
	if _, err := fs.Stat(fsys, "run/tlp/last_pwr"); err == nil {
		m.add("TLP")
	} else if !errors.Is(err, fs.ErrNotExist) {
		m.status = domain.Partial
	}
	processes, err := fs.ReadDir(fsys, "proc")
	if err != nil {
		m.status = domain.Partial
	}
	for _, process := range processes {
		if ctx.Err() != nil {
			m.status = domain.Partial
			break
		}
		if !indexed(process.Name(), "") {
			continue
		}
		comm, err := readValue(fsys, path.Join("proc", process.Name(), "comm"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			m.status = domain.Partial
		}
		switch comm {
		case "power-profiles-", profileDaemon:
			m.add(profileDaemon)
		case "tlp-pd", "tlp":
			m.add("TLP")
		case "tuned", "tuned-ppd":
			m.add("TuneD")
		case "auto-cpufreq":
			m.add("auto-cpufreq")
		}
	}
	slices.Sort(m.names)
	return m
}

func readUnits(output string) managerObservation {
	m := managerObservation{status: domain.Available}
	units := make(map[string]map[string]string)
	for block := range strings.SplitSeq(strings.TrimSpace(output), "\n\n") {
		props := make(map[string]string)
		for line := range strings.SplitSeq(block, "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				props[key] = value
			}
		}
		if id := props["Id"]; id != "" {
			if _, duplicate := units[id]; duplicate {
				m.status = domain.Partial
			}
			units[id] = props
		}
	}
	for _, known := range managerUnits {
		unit := units[known.unit]
		switch unit["ActiveState"] {
		case "active":
			if unit["LoadState"] != "loaded" {
				m.status = domain.Partial
				continue
			}
			m.add(known.manager)
			if known.manager == profileDaemon {
				m.daemonPID, _ = strconv.ParseUint(unit["MainPID"], 10, 32)
				if m.daemonPID == 0 {
					m.status = domain.Partial
				}
			}
		case "inactive", "failed":
			switch unit["LoadState"] {
			case "loaded", "not-found", "masked":
			default:
				m.status = domain.Partial
			}
		default:
			m.status = domain.Partial
		}
	}
	return m
}

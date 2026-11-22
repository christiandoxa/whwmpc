//go:build linux

package platform

import (
	"context"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type Native struct{}

func (Native) Overview(ctx context.Context) domain.Overview {
	return (linuxProvider{fs: os.DirFS("/"), now: time.Now, wait: wait, statfs: syscall.Statfs}).collect(ctx)
}

type linuxProvider struct {
	fs     fs.FS
	now    func() time.Time
	wait   func(context.Context, time.Duration) error
	statfs func(string, *syscall.Statfs_t) error
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p linuxProvider) collect(ctx context.Context) domain.Overview {
	o := baseOverview(p.now(), domain.Unknown)
	info := keyValues(p.read("etc/os-release"), "=")
	if len(info) == 0 {
		info = keyValues(p.read("usr/lib/os-release"), "=")
	}
	o.Distro, o.DistroID = osValue(info["PRETTY_NAME"]), osValue(info["ID"])
	o.Kernel = domain.SafeText(p.read("proc/sys/kernel/osrelease"))
	o.Environment = p.environment(o.Kernel)
	_, systemdErr := fs.Stat(p.fs, "run/systemd/system")
	o.Systemd = systemdErr == nil && strings.TrimSpace(p.read("proc/1/comm")) == "systemd"
	cpuInfo := keyValues(p.read("proc/cpuinfo"), ":")
	for _, key := range []string{"model name", "Hardware", "Processor"} {
		if o.CPUModel = domain.SafeText(cpuInfo[key]); o.CPUModel != "" {
			break
		}
	}
	if fields := strings.Fields(p.read("proc/uptime")); len(fields) > 0 {
		if value, err := strconv.ParseFloat(fields[0], 64); err == nil && value >= 0 && !math.IsInf(value, 0) {
			o.UptimeSeconds = &value
		}
	}

	o.Disks, o.DiskStatus = p.disks(ctx)
	o.Temperature = p.temperature()
	o.OnAC, o.Battery = p.power()
	if o.OnAC != nil || o.Battery != nil {
		o.PowerStatus = domain.Available
	}

	start := p.now().UTC()
	o.CPU.StartedAt = start
	var totalDelta uint64
	o.Memory = p.memory()
	mem := domain.Percent{Availability: domain.Unknown, StartedAt: start}
	o.ProcessStatus = domain.Unknown
	procs := p.processes(ctx)
	previous, previousOK := cpuCounters(p.read("proc/stat"))
	for range 3 {
		if p.wait(ctx, 260*time.Millisecond) != nil {
			break
		}
		next, ok := cpuCounters(p.read("proc/stat"))
		if previousOK && ok && next.total > previous.total && next.idle >= previous.idle {
			total, idle := next.total-previous.total, next.idle-previous.idle
			if idle <= total {
				o.CPU.Samples = append(o.CPU.Samples, 100*float64(total-idle)/float64(total))
				totalDelta += total
				o.CPU.CapturedAt = p.now().UTC()
			}
		}
		previous, previousOK = next, ok
		if data := p.memory(); data.Availability == domain.Available {
			o.Memory = data
			mem.Samples = append(mem.Samples, 100*float64(data.TotalBytes-data.FreeBytes)/float64(data.TotalBytes))
			mem.CapturedAt = p.now().UTC()
		}
	}
	o.CPU.Availability = sampleStatus(len(o.CPU.Samples))
	mem.Availability = sampleStatus(len(mem.Samples))
	o.Memory.Use = mem
	if totalDelta > 0 {
		if records := p.processes(ctx); len(records) > 0 && len(procs) > 0 {
			o.Processes = topProcesses(procs, records, totalDelta)
			if len(o.Processes) != 0 {
				o.ProcessStatus = sampleStatus(len(o.CPU.Samples))
			}
		}
	}
	o.CapturedAt = p.now().UTC()
	o.Availability = domain.Partial
	if o.CPU.Availability == domain.Available && o.Memory.Availability == domain.Available && o.DiskStatus == domain.Available {
		o.Availability = domain.Available
	}
	return o
}

func sampleStatus(n int) domain.Availability {
	switch n {
	case 0:
		return domain.Unknown
	case 3:
		return domain.Available
	default:
		return domain.Partial
	}
}

func (p linuxProvider) read(path string) string {
	b, _ := fs.ReadFile(p.fs, path)
	return string(b)
}

func keyValues(text, separator string) map[string]string {
	values := make(map[string]string)
	for line := range strings.SplitSeq(text, "\n") {
		key, value, ok := strings.Cut(line, separator)
		if key = strings.TrimSpace(key); ok && values[key] == "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

func osValue(s string) string {
	if unquoted, err := strconv.Unquote(s); err == nil {
		s = unquoted
	}
	return domain.SafeText(s)
}

func (p linuxProvider) exists(path string) bool {
	_, err := fs.Stat(p.fs, path)
	return err == nil
}

func (p linuxProvider) environment(kernel string) string {
	container := strings.ToLower(p.read("proc/1/cgroup"))
	if p.exists(".dockerenv") || p.exists("run/.containerenv") ||
		strings.Contains(container, "docker") || strings.Contains(container, "libpod") ||
		strings.Contains(container, "kubepods") || strings.Contains(container, "lxc") ||
		p.read("run/systemd/container") != "" {
		return "container"
	}
	if strings.Contains(strings.ToLower(kernel+" "+p.read("proc/version")), "microsoft") {
		return "wsl"
	}
	dmi := strings.ToLower(p.read("sys/class/dmi/id/product_name") + " " + p.read("sys/class/dmi/id/sys_vendor"))
	for _, marker := range []string{"kvm", "qemu", "virtual", "vmware", "xen", "hyper-v", "amazon ec2", "google compute engine"} {
		if strings.Contains(dmi, marker) {
			return "virtual_machine"
		}
	}
	if strings.Contains(p.read("proc/cpuinfo"), " hypervisor ") || p.exists("sys/hypervisor/type") {
		return "virtual_machine"
	}
	return "undetermined"
}

type cpuTicks struct{ total, idle uint64 }

func cpuCounters(text string) (cpuTicks, bool) {
	line, _, _ := strings.Cut(text, "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTicks{}, false
	}
	var t cpuTicks
	// Guest values are already included in user and nice.
	for i, field := range fields[1:min(len(fields), 9)] {
		n, err := strconv.ParseUint(field, 10, 64)
		if err != nil || n > math.MaxUint64-t.total {
			return cpuTicks{}, false
		}
		t.total += n
		if i == 3 || i == 4 {
			t.idle += n
		}
	}
	return t, t.total > 0
}

func (p linuxProvider) memory() domain.Memory {
	values := keyValues(p.read("proc/meminfo"), ":")
	kib := func(key string) (uint64, bool) {
		f := strings.Fields(values[key])
		if len(f) != 2 || f[1] != "kB" {
			return 0, false
		}
		n, err := strconv.ParseUint(f[0], 10, 54)
		return n << 10, err == nil
	}
	total, totalOK := kib("MemTotal")
	free, freeOK := kib("MemAvailable")
	swap, swapOK := kib("SwapTotal")
	swapFree, swapFreeOK := kib("SwapFree")
	if !totalOK || !freeOK || total == 0 || free > total {
		return domain.Memory{Availability: domain.Unknown, SwapStatus: domain.Unknown}
	}
	m := domain.Memory{Availability: domain.Available, SwapStatus: domain.Unknown, TotalBytes: total, FreeBytes: free}
	if swapOK && swapFreeOK && swapFree <= swap {
		m.SwapStatus, m.SwapBytes, m.SwapUsed = domain.Available, swap, swap-swapFree
	}
	return m
}

var mountEscapes = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

func (p linuxProvider) disks(ctx context.Context) ([]domain.Disk, domain.Availability) {
	var disks []domain.Disk
	seen := make(map[string]bool)
	status := domain.Available
	for line := range strings.SplitSeq(p.read("proc/self/mountinfo"), "\n") {
		if ctx.Err() != nil || len(disks) >= 24 {
			status = domain.Partial
			break
		}
		left, right, ok := strings.Cut(line, " - ")
		f, r := strings.Fields(left), strings.Fields(right)
		if !ok || len(f) < 6 || len(r) < 3 {
			continue
		}
		point, kind := mountEscapes.Replace(f[4]), r[0]
		switch kind {
		case "ext2", "ext3", "ext4", "btrfs", "xfs", "f2fs", "zfs", "ntfs3", "vfat", "exfat":
		case "overlay":
			if point != "/" {
				continue
			}
		default:
			continue
		}
		key := f[2]
		if seen[key] || !filepath.IsAbs(point) {
			continue
		}
		seen[key] = true
		d := domain.Disk{Mount: point, FSType: kind, Root: point == "/", Availability: domain.Error}
		var s syscall.Statfs_t
		if p.statfs(point, &s) == nil && s.Blocks > 0 && s.Bfree <= s.Blocks && s.Bavail <= s.Bfree &&
			s.Bsize > 0 && s.Blocks <= math.MaxUint64/uint64(s.Bsize) {
			d.TotalBytes, d.FreeBytes = s.Blocks*uint64(s.Bsize), s.Bavail*uint64(s.Bsize)
			used := s.Blocks - s.Bfree
			if used+s.Bavail > 0 {
				d.UsedPercent = 100 * float64(used) / float64(used+s.Bavail)
				d.Availability = domain.Available
			}
		}
		if d.Availability != domain.Available {
			status = domain.Partial
		}
		disks = append(disks, d)
	}
	if len(disks) == 0 {
		status = domain.Unknown
	}
	slices.SortFunc(disks, func(a, b domain.Disk) int {
		if a.Root != b.Root {
			if a.Root {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Mount, b.Mount)
	})
	return disks, status
}

func (p linuxProvider) temperature() domain.Temperature {
	got := domain.Temperature{Availability: domain.Unknown}
	read := func(file, source string) {
		n, err := strconv.ParseInt(strings.TrimSpace(p.read(file)), 10, 32)
		if err != nil || n < -30000 || n > 150000 {
			return
		}
		reading := float64(n) / 1000
		if got.Celsius == nil || reading > *got.Celsius {
			got = domain.Temperature{Availability: domain.Available, Celsius: &reading, Source: source}
		}
	}
	entries, _ := fs.Glob(p.fs, "sys/class/hwmon/hwmon*")
	for _, dir := range entries {
		name := strings.TrimSpace(p.read(dir + "/name"))
		switch name {
		case "coretemp", "k10temp", "zenpower", "k8temp", "cpu_thermal":
			inputs, _ := fs.Glob(p.fs, dir+"/temp*_input")
			for _, input := range inputs {
				read(input, name)
			}
		}
	}
	if got.Celsius == nil {
		zones, _ := fs.Glob(p.fs, "sys/class/thermal/thermal_zone*")
		for _, dir := range zones {
			switch kind := strings.TrimSpace(p.read(dir + "/type")); kind {
			case "x86_pkg_temp", "cpu-thermal", "cpu_thermal", "soc_thermal":
				read(dir+"/temp", kind)
			}
		}
	}
	return got
}

func (p linuxProvider) power() (ac *bool, battery *float64) {
	dirs, _ := fs.Glob(p.fs, "sys/class/power_supply/*")
	for _, dir := range dirs {
		switch strings.TrimSpace(p.read(dir + "/type")) {
		case "Mains", "USB", "USB_C", "USB_PD":
			if value := strings.TrimSpace(p.read(dir + "/online")); value == "0" || value == "1" {
				online := value == "1"
				if ac == nil || online {
					ac = &online
				}
			}
		case "Battery":
			if strings.TrimSpace(p.read(dir+"/present")) == "0" || strings.TrimSpace(p.read(dir+"/scope")) == "Device" {
				continue
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(p.read(dir+"/capacity")), 64)
			if err == nil && n >= 0 && n <= 100 && (battery == nil || n < *battery) {
				battery = &n
			}
		}
	}
	return ac, battery
}

type processStat struct {
	start, ticks, rss uint64
	name              string
}

func (p linuxProvider) processes(ctx context.Context) map[string]processStat {
	found := make(map[string]processStat)
	proc, err := p.fs.Open("proc")
	if err != nil {
		return found
	}
	defer proc.Close()
	dir, ok := proc.(fs.ReadDirFile)
	if !ok {
		return found
	}
	entries, _ := dir.ReadDir(4096)
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		pid := entry.Name()
		if _, err := strconv.ParseUint(pid, 10, 32); err != nil {
			continue
		}
		if stat, ok := parseProcess(p.read("proc/" + pid + "/stat")); ok {
			found[pid] = stat
		}
	}
	return found
}

func parseProcess(line string) (processStat, bool) {
	start, end := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
	if start < 0 || end <= start || end+1 >= len(line) {
		return processStat{}, false
	}
	fields := strings.Fields(line[end+1:])
	if len(fields) <= 21 {
		return processStat{}, false
	}
	user, e1 := strconv.ParseUint(fields[11], 10, 64)
	kernel, e2 := strconv.ParseUint(fields[12], 10, 64)
	born, e3 := strconv.ParseUint(fields[19], 10, 64)
	rss, e4 := strconv.ParseUint(fields[21], 10, 64)
	page := uint64(os.Getpagesize())
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || rss > math.MaxUint64/page || user > math.MaxUint64-kernel {
		return processStat{}, false
	}
	return processStat{born, user + kernel, rss * page, domain.SafeText(line[start+1 : end])}, true
}

func topProcesses(before, after map[string]processStat, total uint64) []domain.Process {
	if total == 0 {
		return nil
	}
	out := make([]domain.Process, 0, len(after))
	for pid, next := range after {
		if prev, ok := before[pid]; ok && prev.start == next.start && next.ticks >= prev.ticks {
			delta := next.ticks - prev.ticks
			if delta <= total && (delta > 0 || next.rss > 0) {
				out = append(out, domain.Process{Name: next.name, CPU: 100 * float64(delta) / float64(total), RSS: next.rss})
			}
		}
	}
	slices.SortFunc(out, func(a, b domain.Process) int {
		switch {
		case a.CPU > b.CPU || (a.CPU == b.CPU && a.RSS > b.RSS):
			return -1
		case a.CPU < b.CPU || (a.CPU == b.CPU && a.RSS < b.RSS):
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})
	return out[:min(len(out), 5)]
}

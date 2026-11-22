package storage

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

const mountInfoLimit = 4 << 20

var (
	deviceName  = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	deviceID    = regexp.MustCompile(`^[0-9]{1,10}:[0-9]{1,10}$`)
	nvmeName    = regexp.MustCompile(`^nvme[0-9]+(c[0-9]+)?n[0-9]+$`)
	usbPath     = regexp.MustCompile(`/usb[0-9]+/`)
	btrfsID     = regexp.MustCompile(`^[0-9a-f-]{36}$`)
	mountEscape = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
)

type blockDevice struct {
	BlockLayer
	readOnly   bool
	complete   bool
	physical   bool
	transport  string
	rotational *bool
}

type topology struct {
	byName map[string]blockDevice
	byID   map[string]string
}

func (c Collector) path(path string) string {
	return filepath.Join(c.Root, path)
}

func readText(path string) string {
	value, _ := os.ReadFile(path)
	return strings.TrimSpace(string(value))
}

func readUint(path string) *uint64 {
	value, err := strconv.ParseUint(readText(path), 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

func (c Collector) topology(ctx context.Context) (topology, error) {
	class := c.path("/sys/class/block")
	entries, err := os.ReadDir(class)
	if err != nil {
		return topology{}, err
	}
	store := topology{byName: make(map[string]blockDevice), byID: make(map[string]string)}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return store, err
		}
		name := entry.Name()
		if !deviceName.MatchString(name) {
			continue
		}
		dir := filepath.Join(class, name)
		id := readText(filepath.Join(dir, "dev"))
		resolved, err := filepath.EvalSymlinks(dir)
		if !deviceID.MatchString(id) || err != nil {
			continue
		}
		ro := readUint(filepath.Join(dir, "ro"))
		layer := blockDevice{
			BlockLayer: BlockLayer{Name: name, DeviceID: id},
			readOnly:   ro != nil && *ro != 0,
			complete:   ro != nil,
		}
		if _, err := os.Stat(filepath.Join(dir, "partition")); err == nil {
			parent := filepath.Base(filepath.Dir(resolved))
			layer.BackingDevices = []string{parent}
			layer.DiskSequence = readText(filepath.Join(filepath.Dir(resolved), "diskseq"))
			// Partitions share the queue of the whole device.
			layer.MaxDiscardBytes = readUint(filepath.Join(filepath.Dir(resolved), "queue/discard_max_bytes"))
		} else {
			layer.MaxDiscardBytes = readUint(filepath.Join(dir, "queue/discard_max_bytes"))
			layer.DiskSequence = readText(filepath.Join(dir, "diskseq"))
			slaves, err := os.ReadDir(filepath.Join(dir, "slaves"))
			layer.complete = layer.complete && err == nil
			for _, slave := range slaves {
				layer.BackingDevices = append(layer.BackingDevices, slave.Name())
			}
			if degraded := readUint(filepath.Join(dir, "md/degraded")); degraded != nil && *degraded > 0 {
				layer.complete = false
			}
			_, err = os.Stat(filepath.Join(dir, "device"))
			layer.physical = err == nil && len(layer.BackingDevices) == 0 && !strings.HasPrefix(name, "sr")
			if value := readUint(filepath.Join(dir, "queue/rotational")); value != nil {
				rotational := *value != 0
				layer.rotational = &rotational
			}
			switch {
			case nvmeName.MatchString(name):
				layer.transport = "NVMe"
			case usbPath.MatchString(resolved):
				layer.transport = "USB"
			case strings.HasPrefix(name, "vd") || strings.HasPrefix(name, "xvd"):
				layer.transport = "virtual"
			}
		}
		store.byName[name] = layer
		store.byID[id] = name
	}
	return store, nil
}

func (t topology) layers(roots []string) ([]BlockLayer, bool, bool) {
	var layers []BlockLayer
	seen := make(map[string]uint8)
	complete := len(roots) != 0
	blocked := false
	var visit func(string)
	visit = func(name string) {
		if seen[name] == 1 {
			complete = false
			return
		}
		if seen[name] == 2 {
			return
		}
		seen[name] = 1
		defer func() { seen[name] = 2 }()
		layer, ok := t.byName[name]
		if !ok {
			complete = false
			return
		}
		complete = complete && layer.complete && layer.MaxDiscardBytes != nil &&
			(layer.physical || len(layer.BackingDevices) != 0)
		blocked = blocked || layer.readOnly || (layer.MaxDiscardBytes != nil && *layer.MaxDiscardBytes == 0)
		layers = append(layers, layer.BlockLayer)
		for _, dependency := range layer.BackingDevices {
			visit(dependency)
		}
	}
	for _, name := range roots {
		visit(name)
	}
	return layers, complete, blocked
}

func parseMountInfo(data []byte) ([]Mount, error) {
	var mounts []Mount
	var parseErr error
	reader := bufio.NewScanner(bytes.NewReader(data))
	reader.Buffer(make([]byte, 4096), 128<<10)
	for reader.Scan() {
		before, after, ok := strings.Cut(reader.Text(), " - ")
		fields := strings.Split(before, " ")
		fs := strings.Split(after, " ")
		if !ok || len(fields) < 6 || len(fs) != 3 || !deviceID.MatchString(fields[2]) {
			parseErr = errors.New("incomplete mountinfo")
			continue
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		parent, parentErr := strconv.ParseUint(fields[1], 10, 64)
		root, point := mountEscape.Replace(fields[3]), mountEscape.Replace(fields[4])
		if err != nil || parentErr != nil || id == 0 || !filepath.IsAbs(root) || !filepath.IsAbs(point) {
			parseErr = errors.New("invalid mountinfo entry")
			continue
		}
		opts := append(strings.Split(fields[5], ","), strings.Split(fs[2], ",")...)
		mount := Mount{
			ID: id, parentID: parent, DeviceID: fields[2], Root: root, Point: point,
			Source: mountEscape.Replace(fs[1]), FSType: cleanLabel(fs[0]),
			ReadOnly: slices.Contains(opts, "ro"), MountOptions: opts,
		}
		for _, option := range opts {
			switch option {
			case "discard", "discard=sync", "discard=async", "nodiscard":
				mount.DiscardMode = option
			}
		}
		mounts = append(mounts, mount)
	}
	return mounts, errors.Join(parseErr, reader.Err())
}

func (c Collector) sourceDevice(source string, t topology) string {
	if !strings.HasPrefix(source, "/dev/") || filepath.Clean(source) != source {
		return ""
	}
	path, err := filepath.EvalSymlinks(c.path(source))
	if err != nil {
		if filepath.Dir(source) != "/dev" {
			return ""
		}
		path = c.path(source)
	}
	if filepath.Dir(path) != c.path("/dev") {
		return ""
	}
	name := filepath.Base(path)
	if _, ok := t.byName[name]; ok {
		return name
	}
	return ""
}

func (c Collector) btrfsDevices(source string) ([]string, bool) {
	if source == "" {
		return nil, false
	}
	base := c.path("/sys/fs/btrfs")
	groups, _ := os.ReadDir(base)
	for _, group := range groups {
		if !btrfsID.MatchString(group.Name()) {
			continue
		}
		dir := filepath.Join(base, group.Name())
		entries, _ := os.ReadDir(filepath.Join(dir, "devices"))
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		if !slices.Contains(names, source) {
			continue
		}
		info, err := os.ReadDir(filepath.Join(dir, "devinfo"))
		complete := err == nil && len(info) == len(names)
		for _, entry := range info {
			complete = complete && readText(filepath.Join(dir, "devinfo", entry.Name(), "missing")) == "0"
		}
		return names, complete
	}
	return []string{source}, false
}

func supportsTrim(fs string) bool {
	switch fs {
	case "ext4", "xfs", "btrfs", "vfat":
		return true
	}
	return false
}

func visibleMount(m Mount, all map[uint64]Mount, paths map[string]int) bool {
	// A stacked ancestor can hide every mount below it, including unique paths.
	for hops := 0; hops < len(all); hops++ {
		if paths[m.Point] != 1 {
			return false
		}
		parent, known := all[m.parentID]
		if !known || parent.ID == m.ID {
			return true
		}
		m = parent
	}
	return false
}

func (c Collector) mounts(ctx context.Context, t topology) ([]Mount, domain.Availability, string) {
	file, err := os.Open(c.path("/proc/self/mountinfo"))
	if err != nil {
		return nil, filesystemAvailability(err), "mountinfo tidak dapat dibaca."
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, mountInfoLimit+1))
	if err != nil {
		return nil, filesystemAvailability(err), "mountinfo tidak dapat dibaca."
	}
	if len(data) > mountInfoLimit {
		return nil, domain.Partial, "mountinfo melebihi batas pembacaan."
	}
	all, parseErr := parseMountInfo(data)
	counts := make(map[string]int)
	byID := make(map[uint64]Mount)
	for _, m := range all {
		if _, exists := byID[m.ID]; exists {
			parseErr = errors.New("duplicate mount ID")
		}
		counts[m.Point]++
		byID[m.ID] = m
	}
	var mounts []Mount
	for _, m := range all {
		if err := ctx.Err(); err != nil {
			return nil, domain.Error, "Pemeriksaan mount dibatalkan."
		}
		name := t.byID[m.DeviceID]
		if m.Point != "/" && !strings.HasPrefix(m.Source, "/dev/") && !supportsTrim(m.FSType) && name == "" {
			continue
		}
		if m.FSType == "squashfs" && strings.HasPrefix(name, "loop") {
			continue
		}
		deviceRoots := []string{name}
		membershipKnown := name != ""
		if m.FSType == "btrfs" {
			deviceRoots, membershipKnown = c.btrfsDevices(c.sourceDevice(m.Source, t))
		}
		if m.FSType == "xfs" {
			for _, option := range m.MountOptions {
				key, value, _ := strings.Cut(option, "=")
				if key == "logdev" || key == "rtdev" {
					device := c.sourceDevice(mountEscape.Replace(value), t)
					membershipKnown = membershipKnown && device != ""
					deviceRoots = append(deviceRoots, device)
				}
			}
		}
		layers, complete, blocked := t.layers(deviceRoots)
		m.Layers = layers
		switch {
		case m.ReadOnly || blocked:
			m.Discard, m.Evidence = domain.Unsupported, "Mount read-only atau satu lapisan block melaporkan discard_max_bytes=0."
		case !supportsTrim(m.FSType):
			m.Discard, m.Evidence = domain.Unsupported, "Audit FITRIM belum mendukung tipe filesystem ini."
		case m.Root != "/":
			m.Discard, m.Evidence = domain.Partial, "Mount subtree atau subvolume. FITRIM dapat mencakup seluruh filesystem."
		case parseErr != nil || !visibleMount(m, byID, counts):
			m.Discard, m.Evidence = domain.Unknown, "Identitas mount ambigu atau tertutup mount lain."
		case !membershipKnown || !complete:
			m.Discard, m.Evidence = domain.Unknown, "Jalur block atau kemampuan discard belum seluruhnya terbaca."
		default:
			m.Discard, m.Evidence = domain.Available, "Discard tersedia di setiap lapisan block yang teridentifikasi."
		}
		mounts = append(mounts, m)
	}
	if parseErr != nil {
		return mounts, domain.Partial, "Sebagian entri mountinfo tidak dapat dibaca."
	}
	if len(mounts) == 0 {
		return nil, domain.Unknown, "Tidak ada mount block yang terlihat di namespace ini."
	}
	return mounts, domain.Available, ""
}

func filesystemAvailability(err error) domain.Availability {
	if errors.Is(err, os.ErrPermission) {
		return domain.PermissionRequired
	}
	if errors.Is(err, os.ErrNotExist) {
		return domain.Unsupported
	}
	return domain.Error
}

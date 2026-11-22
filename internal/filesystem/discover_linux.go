package filesystem

import (
	"context"
	"encoding/json"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func (v Volume) CheckCandidate() bool { return v.OfflineCandidate() }

func discover(ctx context.Context, runner sysrun.Runner) Snapshot {
	path, err := runner.LookPath("lsblk")
	if err != nil {
		return Snapshot{Availability: domain.DependencyMissing, Detail: "lsblk dari util-linux tidak tersedia."}
	}
	got := runner.Run(ctx, path, "--json", "--paths", "--list", "--bytes", "--output", "NAME,MAJ:MIN,TYPE,FSTYPE,UUID,RO,MOUNTPOINTS,SIZE")
	s := Snapshot{Availability: availability(got), Backend: "e2fsprogs"}
	if s.Availability != domain.Available {
		s.Detail = "Inventaris volume lsblk tidak tersedia."
		return s
	}
	var data struct {
		Volumes *[]Volume `json:"blockdevices"`
	}
	if err := json.Unmarshal(got.Output, &data); err != nil || data.Volumes == nil {
		s.Availability, s.Detail = domain.Error, "JSON inventaris volume lsblk tidak lengkap."
		return s
	}
	seen := make(map[string]bool)
	for _, v := range *data.Volumes {
		if !devicePath.MatchString(v.Device) || !numericID.MatchString(v.ID) || seen[v.ID] {
			s.Availability = domain.Partial
			continue
		}
		seen[v.ID] = true
		v.Kind, v.Filesystem = domain.SafeText(v.Kind), domain.SafeText(v.Filesystem)
		for i, mount := range v.Mounts {
			v.Mounts[i] = domain.SafeText(mount)
		}
		s.Volumes = append(s.Volumes, v)
	}
	return s
}

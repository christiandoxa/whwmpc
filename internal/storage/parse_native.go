package storage

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func nativeCommandStatus(result sysrun.Result) domain.Availability {
	switch {
	case errors.Is(result.Err, fs.ErrNotExist), result.Err == nil && result.ExitCode == 127:
		return domain.DependencyMissing
	case errors.Is(result.Err, fs.ErrPermission), result.Err == nil && result.ExitCode == 13:
		return domain.PermissionRequired
	case result.Err != nil || result.ExitCode != 0:
		return domain.Error
	default:
		return domain.Available
	}
}

func nativeDriveStatus(drives []Drive) domain.Availability {
	if len(drives) == 0 {
		return domain.Unknown
	}
	first := drives[0].Availability
	for _, d := range drives[1:] {
		if first != d.Availability {
			return domain.Partial
		}
	}
	return first
}

type plistValue struct {
	XMLName xml.Name
	Text    string   `xml:",chardata"`
	List    []string `xml:"string"`
}

func plistFields(data []byte) (map[string]plistValue, error) {
	var doc struct {
		XMLName xml.Name `xml:"plist"`
		Dict    struct {
			Fields []plistValue `xml:",any"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Dict.Fields)%2 != 0 {
		return nil, errors.New("plist dictionary has unbalanced fields")
	}
	out := make(map[string]plistValue, len(doc.Dict.Fields)/2)
	for i := 0; i < len(doc.Dict.Fields); i += 2 {
		key := doc.Dict.Fields[i]
		if key.XMLName.Local != "key" || key.Text == "" || doc.Dict.Fields[i+1].XMLName.Local == "key" {
			return nil, errors.New("invalid plist dictionary field")
		}
		if _, exists := out[key.Text]; exists {
			return nil, errors.New("duplicate plist dictionary field")
		}
		out[key.Text] = doc.Dict.Fields[i+1]
	}
	return out, nil
}

var wholeDarwinDisk = regexp.MustCompile(`^disk[0-9]{1,6}$`)

func parseDarwinDisks(data []byte) ([]string, error) {
	doc, err := plistFields(data)
	if err != nil || doc["WholeDisks"].XMLName.Local != "array" {
		return nil, errors.New("diskutil did not return a disk inventory")
	}
	disks := doc["WholeDisks"].List
	seen := map[string]bool{}
	for _, value := range disks {
		if !wholeDarwinDisk.MatchString(value) || seen[value] {
			return nil, errors.New("unexpected disk identifier")
		}
		seen[value] = true
	}
	return disks, nil
}

func parseDarwinSMART(disk string, data []byte, now time.Time) Drive {
	drive := Drive{Name: disk, CapturedAt: now, Availability: domain.Error}
	doc, err := plistFields(data)
	if err != nil || doc["DeviceIdentifier"].Text != disk || doc["WholeDisk"].XMLName.Local != "true" {
		drive.Detail = "Disk Utility tidak mengembalikan metadata yang cocok dengan disk fisik."
		return drive
	}
	drive.Model = cleanLabel(doc["MediaName"].Text)
	drive.Transport = cleanLabel(doc["BusProtocol"].Text)
	switch doc["SolidState"].XMLName.Local {
	case "true":
		v := false
		drive.Rotational = &v
	case "false":
		v := true
		drive.Rotational = &v
	}
	switch strings.ToLower(strings.TrimSpace(doc["SMARTStatus"].Text)) {
	case "verified":
		passed := true
		drive.Health, drive.Availability = &SMART{Passed: &passed}, domain.Available
	case "failing":
		passed := false
		drive.Health, drive.Availability = &SMART{Passed: &passed}, domain.Available
	case "not supported", "not supported for this device":
		drive.Availability = domain.Unsupported
	default:
		drive.Availability = domain.Unknown
	}
	return drive
}

func parseDarwinTrim(data []byte) ([]TrimPolicy, error) {
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(data, &profile); err != nil || profile == nil {
		return nil, errors.New("invalid system_profiler report")
	}
	var out []TrimPolicy
	for _, item := range []struct{ section, scope string }{{"SPNVMeDataType", "nvme"}, {"SPSerialATADataType", "sata"}} {
		if section, ok := profile[item.section]; ok {
			var entries []any
			if err := json.Unmarshal(section, &entries); err != nil {
				return nil, err
			}
			collectDarwinTrim(entries, item.scope, &out, 0)
		}
	}
	return out, nil
}

func collectDarwinTrim(value any, scope string, out *[]TrimPolicy, depth int) {
	if depth >= 16 || len(*out) >= 32 {
		return
	}
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			collectDarwinTrim(item, scope, out, depth+1)
		}
	case map[string]any:
		if support, ok := node["spsata_trim_support"].(string); ok {
			switch strings.ToLower(support) {
			case "yes", "spdisplays_yes":
				*out = append(*out, TrimPolicy{Scope: scope, Enabled: true})
			case "no", "spdisplays_no":
				*out = append(*out, TrimPolicy{Scope: scope})
			}
		}
		for _, key := range slices.Sorted(maps.Keys(node)) {
			collectDarwinTrim(node[key], scope, out, depth+1)
		}
	}
}

type windowsReliability struct {
	Temperature *int64  `json:"temperature"`
	Wear        *uint64 `json:"wear"`
	Hours       *uint64 `json:"hours"`
	Read        *uint64 `json:"read"`
	Write       *uint64 `json:"write"`
}

type windowsDisk struct {
	Model         string              `json:"model"`
	Bus           *uint16             `json:"bus"`
	Media         *uint16             `json:"media"`
	Health        *uint16             `json:"health"`
	CounterStatus string              `json:"counterStatus"`
	Counters      *windowsReliability `json:"counters"`
}

const windowsSMARTDriveLimit = 65

func parseWindowsSMART(data []byte, now time.Time) ([]Drive, error) {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte{0xef, 0xbb, 0xbf})
	var rows []windowsDisk
	if err := json.Unmarshal(data, &rows); err != nil || rows == nil || len(rows) > windowsSMARTDriveLimit {
		return nil, errors.New("invalid Windows storage response")
	}
	buses := map[uint16]string{1: "SCSI", 3: "ATA", 7: "USB", 8: "RAID", 10: "SAS", 11: "SATA", 15: "Virtual", 16: "Storage Spaces", 17: "NVMe"}
	out := make([]Drive, 0, len(rows))
	for i, row := range rows {
		drive := Drive{Name: fmt.Sprintf("Physical disk %d", i+1), Model: cleanLabel(row.Model), Availability: domain.Unknown, CapturedAt: now}
		if row.Bus != nil {
			drive.Transport = buses[*row.Bus]
		}
		if row.Media != nil && (*row.Media == 3 || *row.Media == 4) {
			rotational := *row.Media == 3
			drive.Rotational = &rotational
		}
		if row.Health != nil {
			switch *row.Health {
			case 0:
				drive.HealthStatus = "healthy"
			case 1:
				drive.HealthStatus = "warning"
			case 2:
				drive.HealthStatus = "unhealthy"
			}
		}
		if row.Counters != nil {
			x := row.Counters
			h := &SMART{}
			if x.Temperature != nil && *x.Temperature > 0 && *x.Temperature <= 150 {
				h.TemperatureC = x.Temperature
			}
			if x.Wear != nil && *x.Wear <= 100 {
				h.PercentageUsed = x.Wear
			}
			for _, counter := range []struct {
				value *uint64
				dest  *string
			}{{x.Hours, &h.PowerOnHours}, {x.Read, &h.ReadErrorsUncorrected}, {x.Write, &h.WriteErrorsUncorrected}} {
				if counter.value != nil {
					*counter.dest = strconv.FormatUint(*counter.value, 10)
				}
			}
			if h.TemperatureC != nil || h.PercentageUsed != nil || h.PowerOnHours != "" || h.ReadErrorsUncorrected != "" || h.WriteErrorsUncorrected != "" {
				drive.Health = h
			}
		}
		switch {
		case drive.HealthStatus != "" && drive.Health != nil:
			drive.Availability = domain.Available
		case drive.HealthStatus != "" || drive.Health != nil:
			drive.Availability = domain.Partial
			if row.CounterStatus == "permission" {
				drive.Detail = "Akses baca counter reliabilitas perlu izin administrator."
			}
		case row.CounterStatus == "permission":
			drive.Availability = domain.PermissionRequired
		case row.CounterStatus == "missing":
			drive.Availability = domain.DependencyMissing
		case row.CounterStatus == "error":
			drive.Availability = domain.Error
		}
		out = append(out, drive)
	}
	return out, nil
}

var windowsTrimSetting = regexp.MustCompile(`(?mi)^(NTFS|ReFS)\s+DisableDeleteNotify\s*=\s*([01])(?:\s+\([^\r\n]*\))?\s*$`)

func parseWindowsTrim(out string) []TrimPolicy {
	var policy []TrimPolicy
	seen := map[string]bool{}
	for _, m := range windowsTrimSetting.FindAllStringSubmatch(out, -1) {
		fs := strings.ToLower(m[1])
		if seen[fs] {
			return nil
		}
		seen[fs] = true
		policy = append(policy, TrimPolicy{Scope: fs, Enabled: m[2] == "0"})
	}
	return policy
}

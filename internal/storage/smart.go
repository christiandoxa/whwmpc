package storage

import (
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type smartDocument struct {
	Format []int `json:"json_format_version"`
	Tool   struct {
		ExitStatus *int `json:"exit_status"`
		Messages   []struct {
			Text string `json:"string"`
		} `json:"messages"`
	} `json:"smartctl"`
	Device struct {
		Protocol string `json:"protocol"`
	} `json:"device"`
	Model   string `json:"model_name"`
	Product string `json:"product"`
	Support struct {
		Available *bool `json:"available"`
		Enabled   *bool `json:"enabled"`
	} `json:"smart_support"`
	Status struct {
		Passed *bool `json:"passed"`
	} `json:"smart_status"`
	Temperature struct {
		Current *int64 `json:"current"`
	} `json:"temperature"`
	PowerOn map[string]json.RawMessage `json:"power_on_time"`
	NVMe    struct {
		CriticalWarning *uint64 `json:"critical_warning"`
		Spare           *uint64 `json:"available_spare"`
		SpareThreshold  *uint64 `json:"available_spare_threshold"`
		PercentageUsed  *uint64 `json:"percentage_used"`
		TemperatureC    *int64  `json:"temperature"`
	} `json:"nvme_smart_health_information_log"`
	ATA struct {
		Table []struct {
			ID         uint8                      `json:"id"`
			Name       string                     `json:"name"`
			Value      *uint8                     `json:"value"`
			Threshold  *uint8                     `json:"thresh"`
			WhenFailed string                     `json:"when_failed"`
			Raw        map[string]json.RawMessage `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
}

func cleanLabel(value string) string {
	value = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, value)
	runes := []rune(strings.TrimSpace(value))
	return string(runes[:min(len(runes), 120)])
}

var decimal = regexp.MustCompile(`^[0-9]{1,39}$`)

// Counter strings keep the full unsigned 128-bit value interoperable in reports.
func counter(fields map[string]json.RawMessage, key string) (string, bool) {
	if le := fields[key+"_le"]; len(le) != 0 && string(le) != "null" {
		var vals []*uint8
		if json.Unmarshal(le, &vals) != nil || len(vals) > 16 {
			return "", false
		}
		bytes := make([]byte, len(vals))
		for i, n := range vals {
			if n == nil {
				return "", false
			}
			bytes[i] = *n
		}
		// The byte array is exact even when smartctl's decimal formatter has limited precision.
		slices.Reverse(bytes)
		return new(big.Int).SetBytes(bytes).String(), true
	}
	text := string(fields[key])
	if encoded := fields[key+"_s"]; len(encoded) != 0 && string(encoded) != "null" {
		if err := json.Unmarshal(encoded, &text); err != nil {
			return "", false
		}
	}
	if text == "" || text == "null" {
		return "", true
	}
	if !decimal.MatchString(text) {
		return "", false
	}
	value, ok := new(big.Int).SetString(text, 10)
	if !ok || value.BitLen() > 128 {
		return "", false
	}
	return value.String(), true
}

var smartFlagNames = []string{
	"invalid_arguments", "device_open_or_identification_failed", "smart_command_or_checksum_failed",
	"disk_failing", "current_prefailure_threshold", "historical_attribute_threshold", "error_log_records",
	"self_test_log_errors",
}

func decodeSMART(output []byte, exitCode int) (Drive, error) {
	var doc *smartDocument
	if err := json.Unmarshal(output, &doc); err != nil {
		return Drive{}, err
	}
	if doc == nil || (len(doc.Format) > 0 && doc.Format[0] != 1) {
		return Drive{}, errors.New("unknown smartctl JSON format")
	}
	if exitCode < 0 || exitCode > 255 || (doc.Tool.ExitStatus != nil && *doc.Tool.ExitStatus != exitCode) {
		return Drive{}, errors.New("inconsistent smartctl exit status")
	}
	var raw struct {
		NVMe map[string]json.RawMessage `json:"nvme_smart_health_information_log"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return Drive{}, err
	}

	health := &SMART{
		ExitCode: exitCode, Passed: doc.Status.Passed, TemperatureC: doc.Temperature.Current,
		CriticalWarning: doc.NVMe.CriticalWarning, Spare: doc.NVMe.Spare,
		SpareThreshold: doc.NVMe.SpareThreshold, PercentageUsed: doc.NVMe.PercentageUsed,
	}
	for bit, name := range smartFlagNames {
		if exitCode&(1<<bit) != 0 {
			health.ExitFlags = append(health.ExitFlags, name)
		}
	}
	if health.TemperatureC == nil {
		health.TemperatureC = doc.NVMe.TemperatureC
	}

	countersOK := true
	readCounter := func(fields map[string]json.RawMessage, key string) string {
		value, ok := counter(fields, key)
		countersOK = countersOK && ok
		return value
	}
	health.PowerOnHours = readCounter(raw.NVMe, "power_on_hours")
	if health.PowerOnHours == "" {
		health.PowerOnHours = readCounter(doc.PowerOn, "hours")
	}
	health.MediaErrors = readCounter(raw.NVMe, "media_errors")
	health.UnsafeShutdowns = readCounter(raw.NVMe, "unsafe_shutdowns")
	health.DataUnitsRead = readCounter(raw.NVMe, "data_units_read")
	health.DataUnitsWritten = readCounter(raw.NVMe, "data_units_written")
	for _, entry := range doc.ATA.Table {
		health.ATAAttributes = append(health.ATAAttributes, ATAAttribute{
			ID: entry.ID, Name: cleanLabel(entry.Name), Normalized: entry.Value, Threshold: entry.Threshold,
			Raw: readCounter(entry.Raw, "value"), WhenFailed: cleanLabel(entry.WhenFailed),
		})
	}

	drive := Drive{Model: cleanLabel(doc.Model), Protocol: cleanLabel(doc.Device.Protocol), Health: health}
	if drive.Model == "" {
		drive.Model = cleanLabel(doc.Product)
	}
	var messages strings.Builder
	for _, m := range doc.Tool.Messages {
		messages.WriteString(strings.ToLower(m.Text))
		messages.WriteByte('\n')
	}
	drive.Availability, drive.Detail = smartAvailability(health, doc.Support.Available, doc.Support.Enabled, messages.String(), countersOK)
	return drive, nil
}

func smartAvailability(h *SMART, supported, enabled *bool, messages string, countersOK bool) (domain.Availability, string) {
	measured := h.Passed != nil || h.CriticalWarning != nil || h.TemperatureC != nil ||
		h.Spare != nil || h.SpareThreshold != nil || h.PercentageUsed != nil || len(h.ATAAttributes) > 0 ||
		h.PowerOnHours != "" || h.MediaErrors != "" || h.UnsafeShutdowns != "" || h.DataUnitsRead != "" ||
		h.DataUnitsWritten != ""
	incomplete := h.Passed == nil && h.CriticalWarning == nil
	switch {
	case denied(messages) && !measured:
		return domain.PermissionRequired, "Izin untuk membaca data SMART dibutuhkan."
	case !measured && (strings.Contains(messages, "unknown usb bridge") || strings.Contains(messages, "please specify device type")):
		return domain.Unsupported, "USB bridge memerlukan tipe perangkat yang cocok dan dukungan pass-through SMART."
	case !measured && supported != nil && !*supported:
		return domain.Unsupported, "Perangkat melaporkan SMART tidak tersedia."
	case !measured && enabled != nil && !*enabled:
		return domain.Unknown, "SMART dinonaktifkan pada perangkat."
	case !measured && (strings.Contains(messages, "standby") || strings.Contains(messages, "sleep mode")):
		return domain.Unknown, "Pemeriksaan ditunda karena perangkat dalam mode daya rendah."
	case h.ExitCode&(SMARTInvalidArguments|SMARTDeviceOpenFailed|SMARTCommandFailed) != 0:
		if measured {
			return domain.Partial, "Sebagian perintah pembacaan SMART gagal."
		}
		return domain.Error, "smartctl gagal membuka perangkat atau membaca SMART."
	case !measured:
		return domain.Unknown, "smartctl tidak memberikan metrik kesehatan."
	case !countersOK:
		return domain.Partial, "Sebagian counter SMART tidak valid."
	case incomplete:
		return domain.Partial, "Metrik tersedia sebagian, status kesehatan SMART tidak dilaporkan."
	default:
		return domain.Available, ""
	}
}

func denied(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "permission denied") || strings.Contains(text, "operation not permitted") ||
		strings.Contains(text, "access denied") || strings.Contains(text, "need root") ||
		strings.Contains(text, "insufficient permissions") || strings.Contains(text, "insufficient privileges")
}

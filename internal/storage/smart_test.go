package storage

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func TestSMARTNVMeLosslessAndNonzero(t *testing.T) {
	fixture, err := os.ReadFile("testdata/nvme.json")
	if err != nil {
		t.Fatal(err)
	}
	drive, err := decodeSMART(fixture, SMARTDiskFailing)
	if err != nil {
		t.Fatal(err)
	}
	h := drive.Health
	if drive.Availability != domain.Available || h.Passed == nil || *h.Passed ||
		h.CriticalWarning == nil || *h.CriticalWarning != 4 || h.TemperatureC == nil || *h.TemperatureC != 48 ||
		h.Spare == nil || *h.Spare != 83 || h.SpareThreshold == nil || *h.SpareThreshold != 10 ||
		h.PercentageUsed == nil || *h.PercentageUsed != 144 || !slices.Equal(h.ExitFlags, []string{"disk_failing"}) {
		t.Fatalf("unexpected parsed health: %+v / %+v", drive, h)
	}
	if h.DataUnitsRead != "340282366920938463463374607431768211455" || h.DataUnitsWritten != "273" ||
		h.MediaErrors != "18446744073709551618" || h.UnsafeShutdowns != "27" || h.PowerOnHours != "0" {
		t.Fatalf("loss of counter precision: %+v", h)
	}
	data, err := json.Marshal(drive)
	if err != nil || strings.Contains(string(data), "private-fixture-serial") ||
		!strings.Contains(string(data), `"media_errors":"18446744073709551618"`) {
		t.Fatalf("unsafe report fields: %s (%v)", data, err)
	}
}

func TestSMARTAvailability(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		exit      int
		want      domain.Availability
		wantError bool
	}{
		{"no metadata", `{}`, 0, domain.Unknown, false},
		{"partial", `{"temperature":{"current":40}}`, 0, domain.Partial, false},
		{"permission", `{"smartctl":{"messages":[{"string":"Operation not permitted"}]}}`, 2, domain.PermissionRequired, false},
		{"bridge", `{"smartctl":{"messages":[{"string":"Unknown USB bridge [0x1234]"}]}}`, 1, domain.Unsupported, false},
		{"standby", `{"smartctl":{"messages":[{"string":"Device is in STANDBY mode"}]}}`, 2, domain.Unknown, false},
		{"unsupported", `{"smart_support":{"available":false}}`, 0, domain.Unsupported, false},
		{"historical masks", `{"smart_status":{"passed":true}}`, 224, domain.Available, false},
		{"command failure still has data", `{"smartctl":{"exit_status":12},"smart_status":{"passed":false}}`, 12, domain.Partial, false},
		{"inconsistent exit status", `{"smartctl":{"exit_status":4}}`, 0, "", true},
		{"non-object", `null`, 0, "", true},
		{"malformed", `{"smart_status":{"passed":true`, 0, "", true},
		{"unknown JSON version", `{"json_format_version":[2,0]}`, 0, "", true},
		{"128 bit overflow", `{"smart_status":{"passed":true},"nvme_smart_health_information_log":{"media_errors":340282366920938463463374607431768211456}}`, 0, domain.Partial, false},
		{"invalid LE byte", `{"smart_status":{"passed":true},"nvme_smart_health_information_log":{"media_errors_le":[1,null]}}`, 0, domain.Partial, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := decodeSMART([]byte(tt.output), tt.exit)
			if (err != nil) != tt.wantError || report.Availability != tt.want {
				t.Fatalf("availability %q, error %v", report.Availability, err)
			}
		})
	}
}

func TestSMARTUsesExactByteCounterOverRoundedDecimal(t *testing.T) {
	drive, err := decodeSMART([]byte(`{"smart_status":{"passed":true},"nvme_smart_health_information_log":{
		"data_units_read":18446744073709551616, "data_units_read_s":"18446744073709551616",
		"data_units_read_le":[1,0,0,0,0,0,0,0,1]
	}}`), 0)
	if err != nil || drive.Health.DataUnitsRead != "18446744073709551617" {
		t.Fatalf("byte and decimal precedence: %+v, %v", drive, err)
	}
}

func TestSMARTATAAttributesRetainVendorData(t *testing.T) {
	drive, err := decodeSMART([]byte(`{
		"model_name":"Fixture\u001b[31m\u202e\nDrive",
		"device":{"protocol":"ATA"},
		"ata_smart_attributes":{"table":[{"id":233,"name":"Vendor_Specific","value":96,"thresh":0,"when_failed":"","raw":{"value":35}}]},
		"smart_status":{"passed":true},
		"power_on_time":{"hours":400}
	}`), 0)
	if err != nil || drive.Model != "Fixture[31mDrive" || drive.Protocol != "ATA" {
		t.Fatalf("decode ATA: %+v, %v", drive, err)
	}
	attrs := drive.Health.ATAAttributes
	if drive.Health.PercentageUsed != nil || drive.Health.PowerOnHours != "400" || len(attrs) != 1 ||
		attrs[0].ID != 233 || attrs[0].Name != "Vendor_Specific" || attrs[0].Raw != "35" {
		t.Fatalf("loss or generic interpretation of ATA attributes: %+v", drive.Health)
	}
}

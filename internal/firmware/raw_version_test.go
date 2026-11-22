package firmware

import (
	"errors"
	"strings"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const rawGUID = "78c7f535-9b96-43c8-8e4c-84415507151c"
const rawAppstreamID = "com.dell.uefi78c7f535.firmware"

func rawVersionTarget() (Device, Release) {
	return Device{ID: testID, Plugin: "uefi_capsule", GUIDs: []string{rawGUID}, VersionFormat: "hex"},
		Release{Version: "0x02000d00", AppstreamID: rawAppstreamID, Remote: LVFS, SHA256: testDigest, Flags: []string{"trusted-metadata"}}
}

func TestPackageMatchesRequiresExactRawVersionEvidence(t *testing.T) {
	device, want := rawVersionTarget()
	const trust = `"Flags":["trusted-payload","trusted-metadata"]`
	const metadata = `{"AppstreamId":"` + rawAppstreamID + `",` + trust + `}`
	const digest = `{"RemoteId":"lvfs","Checksum":["` + testDigest + `"]}`
	details := `{"Devices":[{"DeviceId":"` + testID + `","Plugin":"uefi_capsule","Guid":["` + rawGUID + `"],` +
		`"VersionFormat":"hex","UpdateError":"->convert_version not implemented","Releases":[` + metadata + `,` + digest + `]}]}`
	run := runner{responses: map[string]sysrun.Result{"get-details": result(details)}}
	if err := PackageMatches(t.Context(), run, "/tmp/demo.cab", device, want); !errors.Is(err, ErrRawVersion) {
		t.Fatalf("supported raw version did not request metainfo verification: %v", err)
	}
	for _, test := range []struct{ name, from, to string }{
		{"different error", "->convert_version not implemented", "requires AC"},
		{"different device", testID, strings.Repeat("2", 40)},
		{"different GUID", rawGUID, "88c7f535-9b96-43c8-8e4c-84415507151c"},
		{"additional GUID", `"` + rawGUID + `"`, `"` + rawGUID + `","extra"`},
		{"different format", `"hex"`, `"number"`},
		{"different component", rawAppstreamID, "org.example.other.firmware"},
		{"unsigned metadata", trust, `"Flags":["trusted-payload"]`},
		{"unsigned payload", trust, `"Flags":["trusted-metadata"]`},
		{"different remote", `"lvfs"`, `"lvfs-testing"`},
		{"different digest", testDigest, strings.Repeat("b", 64)},
		{"present but different version", metadata, strings.TrimSuffix(metadata, "}") + `,"Version":"0x02000d01"}`},
		{"additional release", digest, digest + "," + metadata},
		{"combined release without separate digest", metadata + "," + digest, strings.TrimSuffix(metadata, "}") + "," + strings.TrimPrefix(digest, "{")},
	} {
		t.Run(test.name, func(t *testing.T) {
			run.responses["get-details"] = result(strings.Replace(details, test.from, test.to, 1))
			err := PackageMatches(t.Context(), run, "/tmp/demo.cab", device, want)
			if err == nil || errors.Is(err, ErrRawVersion) {
				t.Fatalf("ambiguous or untrusted evidence entered fallback: %v", err)
			}
		})
	}
}

func TestVerifyRawVersionBindsOneExactMetainfoRelease(t *testing.T) {
	device, want := rawVersionTarget()
	const id = `<id>` + rawAppstreamID + `</id>`
	const protocol = `<value key="LVFS::UpdateProtocol">org.uefi.capsule</value>`
	const release = `<release version="0x2000d00"/>`
	const guid = `<firmware type="flashed">` + rawGUID + `</firmware>`
	const metainfo = `<?xml version="1.0" encoding="UTF-8"?>
<component type="firmware">` + id + `
<provides>` + guid + `</provides>
<custom>` + protocol + `<value key="LVFS::VersionFormat">hex</value></custom>
<releases>` + release + `</releases>
</component>`
	if err := VerifyRawVersion([]byte(metainfo), device, want); err != nil {
		t.Fatalf("equivalent unsigned 32-bit versions failed: %v", err)
	}
	for _, test := range []struct{ name, from, to string }{
		{"wrong component ID", rawAppstreamID, "org.example.other.firmware"},
		{"duplicate component ID", id, id + id},
		{"nested ID value", rawAppstreamID, rawAppstreamID + "<id/>"},
		{"wrong GUID", rawGUID, "88c7f535-9b96-43c8-8e4c-84415507151c"},
		{"multiple GUIDs", guid, guid + guid},
		{"wrong GUID type", `type="flashed"`, `type="runtime"`},
		{"wrong protocol", "org.uefi.capsule", "org.example.unknown"},
		{"duplicate protocol", protocol, protocol + protocol},
		{"wrong version", "0x2000d00", "0x2000d01"},
		{"decimal raw version", "0x2000d00", "33557760"},
		{"out of range", "0x2000d00", "0x100000000"},
		{"duplicate version attribute", release, `<release version="0x2000d01" version="0x2000d00"/>`},
		{"multiple releases", release, release + release},
		{"multiple root elements", "</component>", "</component><component/>"},
		{"second metainfo XML", "</component>", "</component>" + metainfo},
		{"nested component", "</component>", "<component/></component>"},
		{"trailing text", "</component>", "</component>fpm"},
		{"leading text", `<component type="firmware">`, `extra<component type="firmware">`},
		{"namespace on identifier", id, `<id xmlns="urn:other">` + rawAppstreamID + `</id>`},
		{"document declaration", `<component type="firmware">`, `<!DOCTYPE component><component type="firmware">`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(strings.Replace(metainfo, test.from, test.to, 1))
			if err := VerifyRawVersion(raw, device, want); err == nil {
				t.Fatal("invalid or ambiguous metainfo was accepted")
			}
		})
	}
	if err := VerifyRawVersion([]byte(metainfo+strings.Repeat(" ", maxMetainfoSize)), device, want); err == nil {
		t.Fatal("oversized metainfo was accepted")
	}
}

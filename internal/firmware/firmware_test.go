package firmware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const testID = "1111111111111111111111111111111111111111"
const testDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testURL = "https://cdn.fwupd.org/downloads/example.cab"

type runner struct {
	responses map[string]sysrun.Result
	commands  *[]string
	missing   bool
}

func (r runner) LookPath(string) (string, error) {
	if r.missing {
		return "", os.ErrNotExist
	}
	return "/usr/bin/fwupdmgr", nil
}

func (r runner) Run(_ context.Context, _ string, args ...string) sysrun.Result {
	if !slices.Contains(args, "--no-metadata-check") || !slices.Contains(args, "--no-unreported-check") {
		return sysrun.Result{Err: errors.New("missing offline guards")}
	}
	if r.commands != nil {
		*r.commands = append(*r.commands, args[5])
	}
	if got, ok := r.responses[args[5]]; ok {
		return got
	}
	return sysrun.Result{Err: errors.New("unexpected command")}
}

func result(contents string) sysrun.Result { return sysrun.Result{Output: []byte(contents)} }

func inventory(release string) string {
	candidate := ""
	if release != "" {
		candidate = `,"Releases":[` + release + `]`
	}
	return `{"Devices":[{"DeviceId":"` + testID + `","Plugin":"uefi_capsule","Version":"1.0","Guid":["firmware-guid"],"Flags":["supported","updatable"],"Serial":"omit-me"` + candidate + `}]}`
}

func release(remote, flags string) string {
	return `{"Version":"2.0","RemoteId":"` + remote + `","Flags":` + flags +
		`,"Checksum":["` + testDigest + `"],"Locations":["` + testURL + `"]}`
}

func TestInventorySelectsTrustedExactUpgrade(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fwupd inventory")
	}
	const flags = `["trusted-metadata","is-upgrade"]`
	ctx := context.Background()
	var calls []string
	r := runner{responses: map[string]sysrun.Result{
		"get-devices": result(inventory("")),
		"get-updates": result(inventory(release("lvfs", flags))),
	}, commands: &calls}
	s := Collect(ctx, r, false)
	if !slices.Equal(calls, []string{"get-devices"}) || s.Availability != domain.Available || len(s.Devices) != 1 {
		t.Fatalf("installed-only query %#v :: %#v", calls, s)
	}
	s = Collect(ctx, r, true)
	id, got, err := Select(s, testID+"@2.0")
	if err != nil || id.ID != testID || got.SHA256 != testDigest || got.URL != testURL {
		t.Fatalf("selection %#v, %#v, %v", id, got, err)
	}
	for _, candidate := range []string{
		release("private", flags),
		release("lvfs", `["is-upgrade"]`),
		release("lvfs", `["trusted-metadata","is-downgrade"]`),
		strings.Replace(release("lvfs", flags), "cdn.fwupd.org", "cdn.fwupd.org.invalid", 1),
	} {
		r.responses["get-updates"] = result(inventory(candidate))
		if _, _, err := Select(Collect(ctx, r, true), testID+"@2.0"); err == nil {
			t.Fatalf("unverified catalog accepted: %s", candidate)
		}
	}
	for _, target := range []string{"--force@1", testID + "@--force", testID + "@2/3"} {
		if _, _, err := SplitTarget(target); err == nil {
			t.Fatalf("unsafe target %q accepted", target)
		}
	}
}

func TestUnknownMissingPermissionAndMalformed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fwupd inventory")
	}
	ctx := context.Background()
	if s := Collect(ctx, runner{missing: true}, false); s.Availability != domain.DependencyMissing {
		t.Fatalf("unexpected missing result: %+v", s)
	}
	for _, pair := range []struct {
		in   sysrun.Result
		want domain.Availability
	}{
		{sysrun.Result{ExitCode: 1, Stderr: []byte("Permission denied")}, domain.PermissionRequired},
		{result(`{}`), domain.Error},
		{result(`{"Devices":[{"Version":"1"}]}`), domain.Error},
	} {
		got := Collect(ctx, runner{responses: map[string]sysrun.Result{"get-devices": pair.in}}, false)
		if got.Availability != pair.want {
			t.Fatalf("unexpected %s, want %s", got.Availability, pair.want)
		}
	}
}

func TestRebootHandoffUsesVerifiedInstalledAndLastResult(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fwupd inventory")
	}
	for _, step := range []struct {
		name, installed, previous, release string
		state                              int
		want                               string
		availability                       domain.Availability
	}{
		{"pending", "1.0", "1.0", "2.0", 1, "reboot_required", domain.Partial},
		{"needs reboot", "2.0", "1.0", "2.0", 4, "reboot_required", domain.Partial},
		{"installed version differs", "1.0", "1.0", "2.0", 2, "unknown", domain.Partial},
		{"previous version matches only", "2.0", "2.0", "3.0", 2, "unknown", domain.Unknown},
		{"installed and attempted versions match", "2.0", "1.0", "2.0", 2, "verified", domain.Available},
		{"explicit failure", "1.0", "1.0", "2.0", 3, "failed", domain.Error},
	} {
		t.Run(step.name, func(t *testing.T) {
			history := fmt.Sprintf(`{"Devices":[{"DeviceId":%q,"Version":%q,"UpdateState":%d,"Releases":[{"Version":%q}]}]}`,
				testID, step.previous, step.state, step.release)
			got := Outcome(context.Background(), runner{responses: map[string]sysrun.Result{
				"get-devices": result(strings.Replace(inventory(""), `"1.0"`, fmt.Sprintf("%q", step.installed), 1)),
				"get-history": result(history),
			}}, testID+"@2.0")
			if got.State != step.want || got.Availability != step.availability {
				t.Fatalf("handoff state %+v, want %q with %q", got, step.want, step.availability)
			}
		})
	}
}

func TestRebootHandoffRequiresUnambiguousHistory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fwupd inventory")
	}
	record := fmt.Sprintf(`{"DeviceId":%q,"Version":"1.0","UpdateState":2,"Releases":[{"Version":"2.0"}]}`, testID)
	for _, history := range []string{
		`{"Devices":[]}`,
		`{"Devices":[` + record + `,` + record + `]}`,
		`{"Devices":[` + strings.Replace(record, `[{"Version":"2.0"}]`, `[]`, 1) + `]}`,
		`{"Devices":[` + strings.Replace(record, testID, strings.Repeat("2", 40), 1) + `]}`,
	} {
		got := Outcome(context.Background(), runner{responses: map[string]sysrun.Result{
			"get-devices": result(strings.Replace(inventory(""), `"1.0"`, `"2.0"`, 1)),
			"get-history": result(history),
		}}, testID+"@2.0")
		if got.State != "unknown" || got.Availability != domain.Unknown {
			t.Fatalf("unproven handoff accepted %s: %+v", history, got)
		}
	}
}

func TestRebootHandoffRejectsEmulatedActiveVersion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fwupd inventory")
	}
	devices := fmt.Sprintf(`{"Devices":[{"DeviceId":%q,"Version":"2.0","Flags":["emulated"]}]}`, testID)
	history := fmt.Sprintf(`{"Devices":[{"DeviceId":%q,"Version":"1.0","UpdateState":2,"Releases":[{"Version":"2.0"}]}]}`, testID)
	got := Outcome(context.Background(), runner{responses: map[string]sysrun.Result{
		"get-devices": result(devices),
		"get-history": result(history),
	}}, testID+"@2.0")
	if got.State != "unknown" || got.Availability != domain.Unknown || got.Installed != "" {
		t.Fatalf("emulated version accepted as installed: %+v", got)
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCabinetRedirectMatchesOfficialMirrorAndName(t *testing.T) {
	const original = "https://fwupd.org/downloads/example.cab"
	const mirror = "https://r2.fwupd.org/lvfs-prod/example.cab"
	if officialURI(mirror, true) {
		t.Fatal("mirror accepted as the starting URL")
	}
	cabinet := []byte("fixture cabinet data")
	sum := sha256.Sum256(cabinet)
	release := Release{URL: original, SHA256: hex.EncodeToString(sum[:])}
	for _, test := range []struct {
		name, location string
		allowed        bool
	}{
		{"R2 production mirror", mirror, true},
		{"existing LVFS CDN", testURL, true},
		{"different cabinet", "https://r2.fwupd.org/lvfs-prod/other.cab", false},
		{"different bucket", "https://r2.fwupd.org/lvfs-testing/example.cab", false},
		{"reserved query", mirror + "?download=1", false},
		{"unrelated host", "https://r2.fwupd.org.invalid/lvfs-prod/example.cab", false},
		{"plain HTTP", "http://r2.fwupd.org/lvfs-prod/example.cab", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{
				CheckRedirect: checkCabinetRedirect,
				Transport: transport(func(req *http.Request) (*http.Response, error) {
					requests++
					if req.URL.String() == original {
						return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {test.location}}}, nil
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(cabinet))}, nil
				}),
			}
			err := download(t.Context(), client, release, io.Discard)
			if test.allowed && (err != nil || requests != 2) || !test.allowed && (err == nil || requests != 1) {
				t.Fatalf("requests=%d, error=%v", requests, err)
			}
		})
	}
}

func TestPackageDigestAndTargetBinding(t *testing.T) {
	ctx := context.Background()
	cab := []byte("fixture cabinet data")
	sum := sha256.Sum256(cab)
	selected := Release{Version: "2.0", URL: testURL, SHA256: hex.EncodeToString(sum[:])}
	transport := transport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != testURL || req.Method != http.MethodGet {
			t.Fatalf("unbound request %v", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(cab))}, nil
	})
	client := &http.Client{Transport: transport}
	var payload bytes.Buffer
	if err := download(ctx, client, selected, &payload); err != nil || !bytes.Equal(payload.Bytes(), cab) {
		t.Fatalf("valid download: %v", err)
	}
	selected.SHA256 = testDigest
	if err := download(ctx, client, selected, io.Discard); err == nil {
		t.Fatal("changed bytes accepted")
	}
	details := inventory(`{"Version":"2.0","Flags":["trusted-payload"]},` +
		`{"RemoteId":"lvfs","Checksum":["` + testDigest + `"]}`)
	dev := Device{ID: testID, GUIDs: []string{"firmware-guid"}}
	r := runner{responses: map[string]sysrun.Result{"get-details": result(details)}}
	if err := PackageMatches(ctx, r, "/tmp/demo.cab", dev, selected); err != nil {
		t.Fatalf("valid package %v", err)
	}
	for _, bad := range []string{
		strings.Replace(details, "trusted-payload", "trusted-metadata", 1),
		strings.Replace(details, "firmware-guid", "wrong-device", 1),
		strings.Replace(details, `"Version":"2.0"`, `"Version":"3.0"`, 1),
		strings.Replace(details, testDigest, strings.Repeat("b", 64), 1),
	} {
		r.responses["get-details"] = result(bad)
		if err := PackageMatches(ctx, r, "/tmp/demo.cab", dev, selected); err == nil {
			t.Fatalf("unmatched cabinet accepted: %s", bad)
		}
	}
}

func TestOfficialRemoteMustRemainFresh(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	remote := Remote{Mtime: now.Add(-5 * time.Hour).Unix(), ChecksumSig: testDigest}
	if !remote.Fresh(now) || remote.Fresh(now.Add(8*24*time.Hour)) || remote.Fresh(now.Add(-6*time.Hour)) {
		t.Fatal("metadata freshness misclassified")
	}
}

func TestOfficialRemoteReadsCachePathsWithoutChangingSignedRemoteIdentity(t *testing.T) {
	const cached = "/var/lib/fwupd/metadata/lvfs/firmware.xml.zst"
	const uri = "https://cdn.fwupd.org/downloads/firmware.xml.zst"
	r, err := OfficialRemote(context.Background(), runner{responses: map[string]sysrun.Result{
		"get-remotes": result(fmt.Sprintf(`{"Remotes":[{"Id":"lvfs","Enabled":true,"Kind":"download","MetadataUri":%q,"MetadataUriSig":%q,"FirmwareBaseUri":"https://fwupd.org/downloads","Mtime":1700000000,"FilenameCache":%q,"FilenameCacheSig":%q}]}`,
			uri, uri+".jcat", cached, cached+".jcat")),
	}}, false)
	if err != nil || r.FilenameCache != cached || r.FilenameCacheSig != cached+".jcat" {
		t.Fatalf("remote did not expose its cache paths: %+v %v", r, err)
	}
	encoded, err := json.Marshal(r)
	if err != nil || bytes.Contains(encoded, []byte(cached)) {
		t.Fatalf("cache paths changed remote identity: %s %v", encoded, err)
	}
}

package firmware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const maxCabinetSize = 128 << 20

// Download downloads only the official CAB recorded in the plan and hashes its exact bytes.
func Download(ctx context.Context, release Release, dst io.Writer) error {
	client := &http.Client{
		Timeout: 12 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || !officialURI(req.URL.String(), true) {
				return errors.New("pengalihan unduhan firmware di luar LVFS resmi")
			}
			return nil
		},
	}
	return download(ctx, client, release, dst)
}

func download(ctx context.Context, client *http.Client, release Release, dst io.Writer) error {
	if !officialURI(release.URL, true) || !checksum.MatchString(release.SHA256) {
		return errors.New("URL atau SHA-256 CAB LVFS tidak valid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxCabinetSize {
		return fmt.Errorf("unduhan CAB ditolak, status HTTP %d atau ukuran melampaui batas", resp.StatusCode)
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, sum), io.LimitReader(resp.Body, maxCabinetSize+1))
	if err != nil {
		return err
	}
	if n == 0 || n > maxCabinetSize || hex.EncodeToString(sum.Sum(nil)) != release.SHA256 {
		return errors.New("SHA-256 atau ukuran CAB hasil unduhan tidak sesuai rencana")
	}
	return nil
}

// PackageMatches independently checks the downloaded CAB through the fwupd daemon.
func PackageMatches(ctx context.Context, runner sysrun.Runner, path string, expected Device, want Release) error {
	r := query(ctx, runner, Args("get-details", path))
	var doc struct{ Devices []Device }
	if availability(r) != domain.Available || json.Unmarshal(r.Output, &doc) != nil || len(doc.Devices) != 1 {
		return errors.New("CAB bukan paket satu-komponen yang dapat diverifikasi oleh fwupd")
	}
	component := doc.Devices[0]
	matchedDevice := slices.ContainsFunc(component.GUIDs, func(guid string) bool {
		return slices.Contains(expected.GUIDs, guid)
	})
	matchedRelease := slices.ContainsFunc(component.Releases, func(r Release) bool {
		return r.Version == want.Version && slices.Contains(r.Flags, "trusted-payload")
	})
	matchedDigest := slices.ContainsFunc(component.Releases, func(r Release) bool {
		return r.Remote == LVFS && slices.Contains(r.Checksums, want.SHA256)
	})
	if component.ID != expected.ID || !matchedDevice || !matchedRelease || !matchedDigest {
		return errors.New("perangkat, versi, tanda tangan payload, atau checksum CAB tidak cocok dengan rencana")
	}
	return nil
}

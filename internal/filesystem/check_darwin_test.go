package filesystem

import (
	"context"
	"os"
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func TestNativeFilesystemDiscovery(t *testing.T) {
	if os.Getenv("WHWMPC_TEST_NATIVE") != "1" {
		t.Skip("run on the native macOS CI host")
	}
	got := Discover(context.Background(), nil)
	if got.Backend != "fsck_apfs" || got.Availability != domain.Available || len(got.Volumes) == 0 {
		t.Fatalf("APFS inventory unavailable: %+v", got)
	}
	for _, v := range got.Volumes {
		if v.CheckCandidate() && v.Mounted() {
			return
		}
	}
	t.Fatalf("APFS inventory has no mounted candidate: %+v", got)
}

package workflow

import (
	"testing"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

func TestCombinedCoverageRequiresEvidenceForPartial(t *testing.T) {
	cases := []struct {
		statuses []domain.Availability
		want     domain.Availability
	}{
		{[]domain.Availability{domain.Available, domain.Available}, domain.Available},
		{[]domain.Availability{domain.Available, domain.Unsupported}, domain.Partial},
		{[]domain.Availability{domain.Unknown, domain.Unsupported, domain.Error}, domain.Error},
		{[]domain.Availability{domain.Unknown, domain.DependencyMissing, domain.PermissionRequired}, domain.PermissionRequired},
		{[]domain.Availability{domain.Unknown, domain.Unsupported}, domain.Unknown},
	}
	for _, tt := range cases {
		if got := combine(tt.statuses...); got != tt.want {
			t.Errorf("%v: %s want %s", tt.statuses, got, tt.want)
		}
	}
}

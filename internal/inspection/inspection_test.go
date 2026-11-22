package inspection

import (
	"context"
	"reflect"
	"testing"
)

func TestDemoDoesNotUseNativeRunner(t *testing.T) {
	ctx := context.Background()
	s := New(true)
	s.runner = nil
	if !reflect.DeepEqual(s.SMART(ctx, false), s.SMART(ctx, true)) || s.TRIM(ctx).Mounts[0].LastTrim == nil {
		t.Fatal("fixture data changed")
	}
	if s.Power(ctx).Profiles.Active != "balanced" || !s.Doctor(ctx).Demo {
		t.Fatal("fixture capability data missing")
	}
}

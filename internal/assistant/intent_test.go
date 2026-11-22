package assistant

import (
	"slices"
	"testing"
)

func TestRoute(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  []Intent
	}{
		{"Kenapa komputernya LAMBAT?", []Intent{Slow}},
		{"Check SSD SMART and TRIM", []Intent{Storage, Trim}},
		{"CPU clock dan profil daya", []Intent{Power}},
		{"caSSDno", nil},
		{"; rm -rf /", nil},
	} {
		if got := Route(tt.input); !slices.Equal(got, tt.want) {
			t.Errorf("Route(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestContext(t *testing.T) {
	if Context("kerja harian") != Daily || Context("Heavy build") != Build || Context("other") != "" {
		t.Fatal("workload routing failed")
	}
}

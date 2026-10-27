package parse

import (
	"strings"
	"testing"
	"time"

	"lifeagent/internal/timeparse"
)

var ekb, _ = time.LoadLocation("Asia/Yekaterinburg")

// Wednesday 2026-10-28, noon.
var now = time.Date(2026, 10, 28, 12, 0, 0, 0, ekb)

func s(v string) *string { return &v }

func ip(v int) *int { return &v }

func rel(days int) *timeparse.Date { return &timeparse.Date{Type: "relative", Days: &days} }

func wday(v, week string) *timeparse.Date {
	d := timeparse.Date{Type: "weekday", Value: v}
	if week != "" {
		d.Week = &week
	}
	return &d
}

func eqp(t *testing.T, name string, got *string, want string) {
	t.Helper()
	switch {
	case want == "" && got != nil:
		t.Errorf("%s = %q, want none", name, *got)
	case want != "" && got == nil:
		t.Errorf("%s missing, want %q", name, want)
	case want != "" && *got != want:
		t.Errorf("%s = %q, want %q", name, *got, want)
	}
}

func join(v []string) string { return strings.Join(v, ",") }

func absDate(v string) *timeparse.Date { return &timeparse.Date{Type: "absolute", Value: v} }

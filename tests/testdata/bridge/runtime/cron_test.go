package runtime

import (
	"testing"
	"time"
)

func TestCronScheduleMatchesPreview(t *testing.T) {
	for _, tc := range []struct{ expression, from, next string }{
		{"0 8 * * *", "2026-10-31T07:59:59Z", "2026-10-31T08:00:00Z"},
		{"0 0 8 * * MON", "2026-10-31T08:00:00Z", "2026-11-02T08:00:00Z"},
		{"0 0 8 * * 7", "2026-10-31T08:00:00Z", "2026-11-01T08:00:00Z"},
		{"0 0 8 * * 5-7", "2026-10-31T08:00:00Z", "2026-11-01T08:00:00Z"},
		{"0 0 8 1 * MON", "2026-10-31T08:00:00Z", "2026-11-01T08:00:00Z"},
		{"* * * * * *", "2026-10-31T08:00:01Z", "2026-10-31T08:00:02Z"},
		{"0 0 0 29 FEB *", "2026-10-31T08:00:00Z", "2028-02-29T00:00:00Z"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			from, _ := time.Parse(time.RFC3339, tc.from)
			want, _ := time.Parse(time.RFC3339, tc.next)
			runs, err := nextCronRuns(tc.expression, 1, from)
			if err != nil || len(runs) != 1 || runs[0] != want.UnixMilli() {
				t.Fatalf("next = %v, %v; want %v", runs, err, want)
			}
			if !cronMatches(tc.expression, want.Add(300*time.Millisecond)) {
				t.Fatal("scheduler disagrees with preview")
			}
		})
	}
}

func TestCronRejectsUnsupportedOrImpossibleSchedules(t *testing.T) {
	for _, expression := range []string{"", "0 0 * *", "0 0 0 L * *", "0 0 * * MON#2", "0 0 * * 17", "0 0 * * 7/0"} {
		if _, err := parseCron(expression); err == nil {
			t.Errorf("accepted %q", expression)
		}
	}
	if _, err := nextCronRuns("0 0 0 31 FEB *", 1, time.Now()); err == nil {
		t.Fatal("accepted impossible date")
	}
	if _, err := nextCronRuns("* * * * *", 1000000, time.Now()); err == nil {
		t.Fatal("accepted unbounded result count")
	}
}

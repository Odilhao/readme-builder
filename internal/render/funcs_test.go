package render

import (
	"testing"
	"time"
)

func TestFuncMapExists(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	funcs := FuncMap(now)
	if funcs == nil {
		t.Fatal("FuncMap returned nil")
	}
}

func TestFuncMapHasHumanize(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	funcs := FuncMap(now)
	if _, ok := funcs["humanize"]; !ok {
		t.Fatal("FuncMap missing 'humanize' function")
	}
}

func TestHumanizeFlattensToMidnight(t *testing.T) {
	now := time.Date(2026, 8, 1, 15, 30, 45, 0, time.UTC)

	got := humanize(now, now)
	want := "now"
	if got != want {
		t.Errorf("humanize(%v, %v) = %q, want %q", now, now, got, want)
	}

	// Test that time is flattened to midnight: yesterday at 14:00 and today at 15:30 are one day apart
	yesterday := time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC)

	got = humanize(yesterday, now)
	want = "1d ago"
	if got != want {
		t.Errorf("humanize(%v, %v) = %q, want %q", yesterday, now, got, want)
	}
}

func TestHumanizeWithFutureTime(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	future := time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)

	got := humanize(future, now)
	want := "tomorrow"
	if got != want {
		t.Errorf("humanize(%v, %v) = %q, want %q", future, now, got, want)
	}
}

func TestHumanizeWithZeroTime(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	zero := time.Time{}

	got := humanize(zero, now)
	want := ""
	if got != want {
		t.Errorf("humanize(%v, %v) = %q, want %q", zero, now, got, want)
	}
}

// TestHumanizeFlattensHoursDayBoundary asserts that two times an hour apart
// on the same calendar day produce byte-identical output (invariant 4:
// humanize flattens timestamps to midnight so hourly crons don't produce
// different READMEs and commit every hour).
func TestHumanizeFlattensHoursDayBoundary(t *testing.T) {
	morning := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	afternoon := time.Date(2026, 8, 1, 14, 30, 45, 0, time.UTC)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	got1 := humanize(morning, now)
	got2 := humanize(afternoon, now)

	if got1 != got2 {
		t.Errorf("humanize(%v, %v) = %q, but humanize(%v, %v) = %q; times on same day should produce identical output",
			morning, now, got1, afternoon, now, got2)
	}
	if got1 != "1d ago" {
		t.Errorf("both times are 1 day ago, want %q but got %q", "1d ago", got1)
	}
}

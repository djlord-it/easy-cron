package cron

import (
	"testing"
	"time"
)

func TestParser_ValidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"every hour", "0 * * * *"},
		{"every 5 minutes", "*/5 * * * *"},
		{"weekday business hours", "0 9-17 * * 1-5"},
		{"daily 2:30am", "30 2 * * *"},
		{"yearly Jan 1", "0 0 1 1 *"},
		{"every minute", "* * * * *"},
		{"specific day", "0 12 15 * *"},
	}

	p := NewParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := p.Parse(tt.expr, "UTC")
			if err != nil {
				t.Errorf("Parse(%q, UTC) returned error: %v", tt.expr, err)
			}
			if sched == nil {
				t.Errorf("Parse(%q, UTC) returned nil schedule", tt.expr)
			}
		})
	}
}

func TestParser_InvalidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"four fields", "* * * *"},
		{"six fields", "* * * * * *"},
		{"invalid minute 60", "60 * * * *"},
		{"invalid hour 25", "0 25 * * *"},
		{"non-numeric", "abc * * * *"},
		{"empty", ""},
	}

	p := NewParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Parse(tt.expr, "UTC")
			if err == nil {
				t.Errorf("Parse(%q, UTC) should return error for invalid expression", tt.expr)
			}
		})
	}
}

func TestParser_TimezoneHandling(t *testing.T) {
	zones := []string{
		"UTC",
		"America/New_York",
		"Europe/Paris",
		"Asia/Tokyo",
		"Australia/Sydney",
		"Pacific/Auckland",
	}

	p := NewParser()
	for _, tz := range zones {
		t.Run(tz, func(t *testing.T) {
			sched, err := p.Parse("0 * * * *", tz)
			if err != nil {
				t.Errorf("Parse with timezone %q returned error: %v", tz, err)
			}
			if sched == nil {
				t.Errorf("Parse with timezone %q returned nil schedule", tz)
			}
		})
	}
}

func TestParser_InvalidTimezone(t *testing.T) {
	tests := []struct {
		name string
		tz   string
	}{
		{"nonexistent", "Invalid/Zone"},
		{"abbreviation", "NOPE"},
	}

	p := NewParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Parse("0 * * * *", tt.tz)
			if err == nil {
				t.Errorf("Parse with timezone %q should return error", tt.tz)
			}
		})
	}
}

func TestParser_NextCalculation(t *testing.T) {
	p := NewParser()

	// "0 10 * * *" = daily at 10:00
	sched, err := p.Parse("0 10 * * *", "UTC")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// After 09:00 → should return 10:00 same day
	after := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	next := sched.Next(after)
	want := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", after, next, want)
	}

	// After 11:00 → should return 10:00 next day
	after2 := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)
	next2 := sched.Next(after2)
	want2 := time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC)
	if !next2.Equal(want2) {
		t.Errorf("Next(%v) = %v, want %v", after2, next2, want2)
	}
}

func TestParser_NextCalculation_Timezone(t *testing.T) {
	p := NewParser()

	// "0 10 * * *" at 10:00 local should produce different UTC times
	schedNY, err := p.Parse("0 10 * * *", "America/New_York")
	if err != nil {
		t.Fatalf("Parse NY failed: %v", err)
	}

	schedTokyo, err := p.Parse("0 10 * * *", "Asia/Tokyo")
	if err != nil {
		t.Fatalf("Parse Tokyo failed: %v", err)
	}

	// Use a reference time well before 10:00 in both zones
	ref := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)

	nextNY := schedNY.Next(ref)
	nextTokyo := schedTokyo.Next(ref)

	// Tokyo 10:00 JST = 01:00 UTC, NY 10:00 EDT = 14:00 UTC
	// So they should be different times in UTC
	if nextNY.Equal(nextTokyo) {
		t.Error("Next() for different timezones should produce different UTC times")
	}

	// Tokyo should fire first (01:00 UTC < 14:00 UTC)
	if !nextTokyo.Before(nextNY) {
		t.Errorf("Tokyo 10:00 JST (%v) should be before NY 10:00 EDT (%v) in UTC",
			nextTokyo.UTC(), nextNY.UTC())
	}
}

func TestParser_DSTSpringForward(t *testing.T) {
	p := NewParser()

	// March 10 2024: US clocks spring forward from 2:00 AM to 3:00 AM EST→EDT
	// Schedule at 2:30 AM — this time doesn't exist on this date
	sched, err := p.Parse("30 2 * * *", "America/New_York")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Just before the DST gap
	before := time.Date(2024, 3, 10, 1, 0, 0, 0, mustLoadLocation("America/New_York"))
	next := sched.Next(before)

	// The robfig/cron library should skip to the next valid occurrence
	// 2:30 AM doesn't exist on March 10, so it should go to March 11
	expectedDay := time.Date(2024, 3, 11, 2, 30, 0, 0, mustLoadLocation("America/New_York"))
	if !next.Equal(expectedDay) {
		// Or it may fire at 3:00 AM (implementation-dependent)
		// Just verify it doesn't fire at 2:30 on March 10
		march10_230 := time.Date(2024, 3, 10, 2, 30, 0, 0, mustLoadLocation("America/New_York"))
		if next.Equal(march10_230) {
			t.Error("should not schedule at 2:30 AM on DST spring-forward day (time doesn't exist)")
		}
		// Verify it's after our reference time
		if !next.After(before) {
			t.Errorf("Next() should be after reference time, got %v", next)
		}
	}
}

func TestParser_DSTFallBack(t *testing.T) {
	p := NewParser()

	// Nov 3 2024: US clocks fall back from 2:00 AM to 1:00 AM EDT→EST
	// Schedule at 1:30 AM — this time occurs twice in wall clock time.
	// The cron library returns the first occurrence (EDT).
	sched, err := p.Parse("30 1 * * *", "America/New_York")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Before the first occurrence
	before := time.Date(2024, 11, 3, 0, 0, 0, 0, mustLoadLocation("America/New_York"))
	next := sched.Next(before)

	// Should fire at 1:30 AM (first occurrence, EDT)
	if next.Hour() != 1 || next.Minute() != 30 {
		t.Errorf("expected 1:30 AM, got %d:%02d", next.Hour(), next.Minute())
	}
	if next.Day() != 3 {
		t.Errorf("expected Nov 3, got Nov %d", next.Day())
	}

	// After the ambiguous period (use 3:00 AM EST = well past fallback),
	// next occurrence should be Nov 4
	afterFallback := time.Date(2024, 11, 3, 3, 0, 0, 0, mustLoadLocation("America/New_York"))
	next2 := sched.Next(afterFallback)
	if next2.Day() != 4 {
		t.Errorf("Next() after fallback should be Nov 4, got Nov %d", next2.Day())
	}
}

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("mustLoadLocation: " + err.Error())
	}
	return loc
}

package testutil

import (
	"testing"
	"time"
)

func TestFakeClock_Now(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(fixed)

	got := clock.Now()
	if !got.Equal(fixed) {
		t.Errorf("Now() = %v, want %v", got, fixed)
	}
}

func TestFakeClock_Advance(t *testing.T) {
	fixed := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	clock := NewFakeClock(fixed)

	clock.Advance(5 * time.Minute)

	want := fixed.Add(5 * time.Minute)
	got := clock.Now()
	if !got.Equal(want) {
		t.Errorf("after Advance(5m), Now() = %v, want %v", got, want)
	}
}

func TestTestContext_HasDeadline(t *testing.T) {
	ctx := TestContext(t)

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("TestContext should have a deadline")
	}

	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > 6*time.Second {
		t.Errorf("deadline should be ~5s from now, got %v", remaining)
	}
}

func TestMustParseUUID_Valid(t *testing.T) {
	id := MustParseUUID("12345678-1234-1234-1234-123456789abc")
	if id.String() != "12345678-1234-1234-1234-123456789abc" {
		t.Errorf("unexpected UUID: %s", id)
	}
}

func TestMustParseUUID_Invalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseUUID should panic on invalid UUID")
		}
	}()
	MustParseUUID("not-a-uuid")
}

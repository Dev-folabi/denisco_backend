package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeTime(t *testing.T) {
	// Both a 24-hour time input and an existing label resolve to the label
	// form the prototype displays.
	cases := map[string]string{
		"09:00":    "09:00 AM",
		"13:00":    "01:00 PM",
		"00:30":    "12:30 AM",
		"12:00":    "12:00 PM",
		"09:00 AM": "09:00 AM",
		"01:00 pm": "01:00 PM",
	}

	for input, want := range cases {
		got, err := NormalizeTime(input)
		if err != nil {
			t.Errorf("NormalizeTime(%q) returned error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeTime(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeTimeRejectsGarbage(t *testing.T) {
	for _, input := range []string{"", "25:00", "nine o'clock", "9am"} {
		if _, err := NormalizeTime(input); err == nil {
			t.Errorf("NormalizeTime(%q) accepted an invalid time", input)
		}
	}
}

func TestTimeToMinutesOrdersByClock(t *testing.T) {
	// Sorting by label alone would put "01:00 PM" before "09:00 AM".
	if TimeToMinutes("09:00 AM") >= TimeToMinutes("01:00 PM") {
		t.Error("TimeToMinutes() did not order morning before afternoon")
	}
	if got := TimeToMinutes("12:00 PM"); got != 12*60 {
		t.Errorf("TimeToMinutes(noon) = %d, want %d", got, 12*60)
	}
	if got := TimeToMinutes("12:30 AM"); got != 30 {
		t.Errorf("TimeToMinutes(half past midnight) = %d, want 30", got)
	}
}

func TestValidateDate(t *testing.T) {
	today := TodayString()
	if _, err := ValidateDate(today); err != nil {
		t.Errorf("ValidateDate(today) returned error: %v", err)
	}

	tomorrow := Today().AddDate(0, 0, 1).Format("2006-01-02")
	if got, err := ValidateDate("  " + tomorrow + "  "); err != nil || got != tomorrow {
		t.Errorf("ValidateDate() = (%q, %v), want the trimmed date", got, err)
	}
}

func TestValidateDateRejectsPastAndFarFuture(t *testing.T) {
	yesterday := Today().AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := ValidateDate(yesterday); !errors.Is(err, ErrDateInPast) {
		t.Errorf("ValidateDate(yesterday) error = %v, want ErrDateInPast", err)
	}

	// The booking window runs 31 days ahead, per the prototype.
	tooFar := Today().AddDate(0, 0, BookingDaysAhead+2).Format("2006-01-02")
	if _, err := ValidateDate(tooFar); err == nil {
		t.Error("ValidateDate() accepted a date beyond the booking window")
	}

	for _, input := range []string{"", "2026/10/05", "05-10-2026", "2026-13-45"} {
		if _, err := ValidateDate(input); err == nil {
			t.Errorf("ValidateDate(%q) accepted a malformed date", input)
		}
	}
}

func TestBookingLifecycle(t *testing.T) {
	active := true

	pending := &Booking{Status: StatusPending, Active: &active}
	if !pending.IsCancellable() || !pending.HoldsSlot() {
		t.Error("a pending booking should be cancellable and hold its slot")
	}

	confirmed := &Booking{Status: StatusConfirmed, Active: &active}
	if !confirmed.IsCancellable() {
		t.Error("a confirmed booking should still be cancellable")
	}

	completed := &Booking{Status: StatusCompleted}
	if completed.IsCancellable() {
		t.Error("a completed booking must not be cancellable")
	}
	if completed.HoldsSlot() {
		t.Error("HoldsSlot() should be false once `active` is unset")
	}
}

func TestReleasesSlot(t *testing.T) {
	// Cancelling or a no-show frees the time for someone else; the others
	// keep it blocked.
	cases := map[string]bool{
		StatusPending:   false,
		StatusConfirmed: false,
		StatusCompleted: false,
		StatusCancelled: true,
		StatusNoShow:    true,
	}

	for status, want := range cases {
		if got := ReleasesSlot(status); got != want {
			t.Errorf("ReleasesSlot(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestValidateStatus(t *testing.T) {
	for _, status := range []string{StatusPending, StatusConfirmed, StatusCompleted, StatusCancelled, StatusNoShow} {
		if err := ValidateStatus(status); err != nil {
			t.Errorf("ValidateStatus(%q) returned error: %v", status, err)
		}
	}

	for _, status := range []string{"", "maybe", "PENDING"} {
		if err := ValidateStatus(status); err == nil {
			t.Errorf("ValidateStatus(%q) accepted an unknown status", status)
		}
	}
}

func TestSeedTypesMatchPrototype(t *testing.T) {
	types := SeedTypes()

	if len(types) != 5 {
		t.Fatalf("SeedTypes() returned %d types, want the prototype's 5", len(types))
	}

	// Prices are stored in kobo: ₦15,000 is 1,500,000.
	first := types[0]
	if first.Code != "CT-01" || first.DurationMinutes != 60 || first.Price != 1_500_000 {
		t.Errorf("first type = %+v, want CT-01 / 60 mins / 1500000 kobo", first)
	}
	if got := first.DurationLabel(); got != "60 mins" {
		t.Errorf("DurationLabel() = %q, want \"60 mins\"", got)
	}

	for _, seedType := range types {
		if !seedType.Active {
			t.Errorf("seeded type %q is not active", seedType.Code)
		}
	}
}

func TestDefaultTimesAreInClockOrder(t *testing.T) {
	times := DefaultTimes()

	if len(times) != 4 {
		t.Fatalf("DefaultTimes() returned %d times, want the prototype's 4", len(times))
	}

	for i := 1; i < len(times); i++ {
		if TimeToMinutes(times[i-1]) >= TimeToMinutes(times[i]) {
			t.Errorf("DefaultTimes() is not in clock order: %v", times)
			break
		}
	}
}

func TestTodayUsesFarmTimezone(t *testing.T) {
	today := Today()

	if today.Location() != Lagos {
		t.Errorf("Today() location = %v, want the farm's timezone", today.Location())
	}
	if today.Hour() != 0 || today.Minute() != 0 {
		t.Error("Today() should be midnight, so date comparisons are whole days")
	}

	// A UTC evening is already the next day in Lagos for part of the year; the
	// point is simply that the date is derived in Lagos, not UTC.
	if _, offset := time.Now().In(Lagos).Zone(); offset != 3600 {
		t.Errorf("Lagos offset = %d seconds, want 3600 (UTC+1)", offset)
	}
}

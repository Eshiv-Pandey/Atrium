package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"atrium/internal/config"
	"atrium/internal/domain"
)

// checkInFailureReason is the message a member reads when the check-in button
// does not work, so a wrong branch here is a support ticket rather than a
// crash. It was factored out of CheckIn precisely so it could be tested without
// a database, and these are the tests that make that separation worth having.
//
// The order of the switch matters as much as the cases: a cancelled booking
// that is also outside its window has two true conditions, and the member needs
// to be told the useful one.

func bookingAt(start time.Time) *domain.Booking {
	return &domain.Booking{
		StartTime: start,
		EndTime:   start.Add(time.Hour),
		Status:    domain.BookingStatusConfirmed,
	}
}

func TestCheckInFailureReason(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	checkedIn := now.Add(-time.Minute)

	cases := []struct {
		name    string
		booking *domain.Booking
		// wantContains is a distinctive fragment of the message. Asserting on a
		// fragment rather than the whole string keeps the test from failing on
		// a comma, while still proving the right branch was taken — the branches
		// all return ErrConflict, so the error type alone cannot distinguish
		// them.
		wantContains string
	}{
		{
			name: "cancelled",
			booking: func() *domain.Booking {
				b := bookingAt(now)
				b.Status = domain.BookingStatusCancelled
				return b
			}(),
			wantContains: "cancelled",
		},
		{
			name: "released as a no-show",
			booking: func() *domain.Booking {
				b := bookingAt(now.Add(-time.Hour))
				b.Status = domain.BookingStatusReleased
				return b
			}(),
			wantContains: "released",
		},
		{
			name: "already checked in",
			booking: func() *domain.Booking {
				b := bookingAt(now)
				b.CheckedInAt = &checkedIn
				return b
			}(),
			wantContains: "already checked in",
		},
		{
			// Just outside the window: one second before it opens. The
			// interesting failures are one unit either side of a boundary, not
			// far from it.
			name:         "too early by a second",
			booking:      bookingAt(now.Add(config.CheckInWindowBefore).Add(time.Second)),
			wantContains: "opens",
		},
		{
			name:         "far too early",
			booking:      bookingAt(now.Add(6 * time.Hour)),
			wantContains: "opens",
		},
		{
			name:         "too late by a second",
			booking:      bookingAt(now.Add(-config.CheckInGracePeriod).Add(-time.Second)),
			wantContains: "window has closed",
		},
		{
			name:         "far too late",
			booking:      bookingAt(now.Add(-6 * time.Hour)),
			wantContains: "window has closed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkInFailureReason(tc.booking, now)
			if err == nil {
				t.Fatal("got no error, want a conflict")
			}
			// Every rejection is a 409. The HTTP layer maps on the sentinel, so
			// a branch returning a bare error would surface as a 500 for what is
			// an ordinary, expected refusal.
			if !errors.Is(err, domain.ErrConflict) {
				t.Errorf("error does not wrap ErrConflict: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Errorf("message %q does not mention %q", err.Error(), tc.wantContains)
			}
		})
	}
}

// TestCheckInFailureReason_Precedence pins the switch order.
//
// A booking can fail several conditions at once, and the branches are ordered
// so the member is told the one they can act on. "This booking was cancelled"
// explains the situation; "check-in opens 5m before the booking starts" for a
// cancelled booking is true, useless, and misleading — it implies waiting will
// help.
func TestCheckInFailureReason_Precedence(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	checkedIn := now.Add(-time.Minute)

	cases := []struct {
		name         string
		mutate       func(*domain.Booking)
		start        time.Time
		wantContains string
	}{
		{
			name:         "cancelled and long past its window",
			mutate:       func(b *domain.Booking) { b.Status = domain.BookingStatusCancelled },
			start:        now.Add(-6 * time.Hour),
			wantContains: "cancelled",
		},
		{
			name:         "cancelled and not yet open",
			mutate:       func(b *domain.Booking) { b.Status = domain.BookingStatusCancelled },
			start:        now.Add(6 * time.Hour),
			wantContains: "cancelled",
		},
		{
			name:         "released and long past its window",
			mutate:       func(b *domain.Booking) { b.Status = domain.BookingStatusReleased },
			start:        now.Add(-6 * time.Hour),
			wantContains: "released",
		},
		{
			// The check-in that arrives twice. The second is not "too late" even
			// if the grace period has since elapsed — it already succeeded, and
			// saying otherwise would suggest the member missed something.
			name:         "already checked in and past the window",
			mutate:       func(b *domain.Booking) { b.CheckedInAt = &checkedIn },
			start:        now.Add(-6 * time.Hour),
			wantContains: "already checked in",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := bookingAt(tc.start)
			tc.mutate(b)
			err := checkInFailureReason(b, now)
			if err == nil {
				t.Fatal("got no error, want a conflict")
			}
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Errorf("message %q does not lead with %q", err.Error(), tc.wantContains)
			}
		})
	}
}

// TestCheckInFailureReason_InsideWindowFallsThrough covers the default branch.
//
// If the booking is confirmed, unchecked-in, and inside its window, then the
// UPDATE should have matched and this function should never have been called.
// Reaching it means the SQL predicate and this switch disagree about the
// window — the one bug this function can have that a member would experience
// as "the button does nothing". The generic message is the honest answer, and
// this test documents that the boundaries are inclusive on both ends, matching
// the `>=` and `<=` in store.CheckIn.
func TestCheckInFailureReason_InsideWindowFallsThrough(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name  string
		start time.Time
	}{
		{"exactly when check-in opens", now.Add(config.CheckInWindowBefore)},
		{"exactly at the start time", now},
		{"midway through the grace period", now.Add(-config.CheckInGracePeriod / 2)},
		{"exactly at the end of the grace period", now.Add(-config.CheckInGracePeriod)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkInFailureReason(bookingAt(tc.start), now)
			if err == nil {
				t.Fatal("got no error, want the generic conflict")
			}
			if !strings.Contains(err.Error(), "cannot be checked in") {
				t.Errorf("message %q is not the generic fallback: a specific branch "+
					"claimed a booking inside its window was refusable, which "+
					"means this switch and store.CheckIn disagree about the window",
					err.Error())
			}
		})
	}
}

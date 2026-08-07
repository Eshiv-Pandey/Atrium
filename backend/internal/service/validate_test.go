package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"atrium/internal/config"
	"atrium/internal/domain"
)

// The 422 rows of the edge-case table. Every rule here is also a CHECK
// constraint in the schema, so the database is the backstop and these tests are
// not proving the data cannot go bad — they are proving the member gets told
// why in words, rather than receiving a constraint name.
//
// validate reads nothing from the service's dependencies, which is why a
// zero-valued BookingService is enough. That is worth stating rather than
// leaving as a surprise: if a future edit makes validate query the database,
// this file stops compiling, which is the right moment to notice.

func validateInput(t *testing.T, in CreateBookingInput, room *domain.Room) error {
	t.Helper()
	return (&BookingService{}).validate(in, room)
}

func testRoom(capacity int) *domain.Room {
	return &domain.Room{ID: uuid.New(), Name: "Conference A", Capacity: capacity}
}

// future returns a window `offset` from now, avoiding the past-booking rule for
// tests that are about some other rule entirely.
func future(offset, duration time.Duration) (time.Time, time.Time) {
	start := time.Now().Add(offset)
	return start, start.Add(duration)
}

func TestValidate(t *testing.T) {
	room := testRoom(6)

	cases := []struct {
		name string
		in   func() CreateBookingInput
		// wantErr is nil for the rows that must be accepted. Interleaving the
		// accepted cases with the rejected ones is deliberate: a validator that
		// rejects everything satisfies every negative assertion.
		wantErr      error
		wantContains string
	}{
		{
			name: "an ordinary hour-long booking tomorrow",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 4}
			},
		},
		{
			name: "end before start",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, time.Hour)
				return CreateBookingInput{Start: e, End: s, AttendeeCount: 1}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "after start",
		},
		{
			name: "zero-length booking",
			in: func() CreateBookingInput {
				s, _ := future(24*time.Hour, 0)
				return CreateBookingInput{Start: s, End: s, AttendeeCount: 1}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "after start",
		},
		{
			// One minute under the minimum. The interesting failure is adjacent
			// to the boundary, not far from it.
			name: "a minute shorter than the minimum",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, config.MinBookingDuration-time.Minute)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "at least",
		},
		{
			name: "exactly the minimum duration is allowed",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, config.MinBookingDuration)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
		},
		{
			name: "exactly the maximum duration is allowed",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, config.MaxBookingDuration)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
		},
		{
			name: "a minute longer than the maximum",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, config.MaxBookingDuration+time.Minute)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "at most",
		},
		{
			name: "starting in the past",
			in: func() CreateBookingInput {
				s, e := future(-2*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "past",
		},
		{
			// The clock-skew tolerance. A client whose clock is a few seconds
			// fast, or a request that spent a moment in flight, must not be told
			// its "now" is already history.
			name: "starting a few seconds ago is tolerated",
			in: func() CreateBookingInput {
				s, e := future(-10*time.Second, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
		},
		{
			name: "beyond the booking horizon",
			in: func() CreateBookingInput {
				s, e := future(config.MaxBookingHorizon+48*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "days ahead",
		},
		{
			name: "just inside the booking horizon",
			in: func() CreateBookingInput {
				s, e := future(config.MaxBookingHorizon-24*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 1}
			},
		},
		{
			name: "zero attendees",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: 0}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "at least one attendee",
		},
		{
			// Not merely invalid: a negative count would pass a naive
			// "count > capacity" check and reserve a room for nobody.
			name: "negative attendees",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: -3}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "at least one attendee",
		},
		{
			name: "filling the room exactly is allowed",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: room.Capacity}
			},
		},
		{
			name: "one attendee over capacity",
			in: func() CreateBookingInput {
				s, e := future(24*time.Hour, time.Hour)
				return CreateBookingInput{Start: s, End: e, AttendeeCount: room.Capacity + 1}
			},
			wantErr:      domain.ErrValidation,
			wantContains: "seats",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInput(t, tc.in(), room)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("valid booking rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("got no error, want a validation failure")
			}
			// Everything here must map to 422. A rule that returned a bare error
			// would surface as a 500 for what is an ordinary bad request.
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error does not wrap ErrValidation: %v", err)
			}
			if tc.wantContains != "" && !containsFold(err.Error(), tc.wantContains) {
				t.Errorf("message %q does not explain the rule (want mention of %q)",
					err.Error(), tc.wantContains)
			}
		})
	}
}

// TestValidate_CapacityNamesTheRoom checks the message is actionable.
//
// "Validation failed" makes a member guess. Naming the room and both numbers
// tells them whether to book a bigger room or bring fewer people, which is the
// only reason to validate this in Go when a CHECK constraint already refuses it.
func TestValidate_CapacityNamesTheRoom(t *testing.T) {
	room := testRoom(4)
	s, e := future(24*time.Hour, time.Hour)

	err := validateInput(t, CreateBookingInput{Start: s, End: e, AttendeeCount: 9}, room)
	if err == nil {
		t.Fatal("9 attendees in a 4-seat room was accepted")
	}
	for _, want := range []string{room.Name, "4", "9"} {
		if !containsFold(err.Error(), want) {
			t.Errorf("message %q omits %q", err.Error(), want)
		}
	}
}

// TestValidateWindow bounds availability queries.
//
// Without an upper bound, `?start=2000-01-01&end=2100-01-01` is a full scan that
// one anonymous request can trigger. The limit is not about correctness — the
// answer would be right — it is about a single request not being able to cost
// the database a year of index traversal.
func TestValidateWindow(t *testing.T) {
	now := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		window       Interval
		wantErr      bool
		wantContains string
	}{
		{"one day", Interval{now, now.Add(24 * time.Hour)}, false, ""},
		{"exactly the maximum", Interval{now, now.Add(maxWindow)}, false, ""},
		{"an hour past the maximum", Interval{now, now.Add(maxWindow + time.Hour)}, true, "at most"},
		{"a decade", Interval{now, now.Add(3650 * 24 * time.Hour)}, true, "at most"},
		{"zero length", Interval{now, now}, true, "after start"},
		{"inverted", Interval{now.Add(time.Hour), now}, true, "after start"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWindow(tc.window)

			if !tc.wantErr {
				if err != nil {
					t.Fatalf("valid window rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("got no error, want a validation failure")
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("error does not wrap ErrValidation: %v", err)
			}
			if !containsFold(err.Error(), tc.wantContains) {
				t.Errorf("message %q does not mention %q", err.Error(), tc.wantContains)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		// Absent or nonsensical becomes the default rather than an error: a
		// missing ?limit= is not a client mistake, it is a client not caring.
		{"absent", 0, 50},
		{"negative", -10, 50},
		{"a reasonable page", 25, 25},
		{"the maximum", 200, 200},
		// Capped, not rejected. The caller gets a smaller page and a cursor,
		// which is a working request rather than a 422 they must handle.
		{"above the maximum", 5000, 200},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLimit(tc.in); got != tc.want {
				t.Errorf("clampLimit(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// containsFold is a case-insensitive substring check, so an assertion about
// wording is not defeated by capitalisation.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

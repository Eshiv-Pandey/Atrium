package service

import (
	"testing"
	"time"
)

// FreeSlots is the function behind the availability timeline, so its edge cases
// are the ones a member sees as a wrong slot on screen. The window is a
// four-hour day (0-240 minutes) in every case below.
func TestFreeSlots(t *testing.T) {
	window := iv(0, 240)

	cases := []struct {
		name        string
		busy        []Interval
		minDuration time.Duration
		want        []Interval
	}{
		{"nothing booked leaves the whole window", nil, 0, []Interval{iv(0, 240)}},
		{
			"a booking in the middle leaves a gap either side",
			[]Interval{iv(60, 120)}, 0,
			[]Interval{iv(0, 60), iv(120, 240)},
		},
		{
			"a booking at the start leaves only the tail",
			[]Interval{iv(0, 60)}, 0,
			[]Interval{iv(60, 240)},
		},
		{
			"a booking at the end leaves only the head",
			[]Interval{iv(180, 240)}, 0,
			[]Interval{iv(0, 180)},
		},
		{"a booking covering the window leaves nothing", []Interval{iv(0, 240)}, 0, nil},
		{
			"a booking overrunning both ends leaves nothing",
			[]Interval{iv(-60, 300)}, 0, nil,
		},
		{
			// Busy time that starts before the window must not produce a
			// negative-length gap or shift the first free slot earlier.
			"a booking overlapping the start is clipped",
			[]Interval{iv(-30, 60)}, 0,
			[]Interval{iv(60, 240)},
		},
		{
			"a booking overlapping the end is clipped",
			[]Interval{iv(180, 300)}, 0,
			[]Interval{iv(0, 180)},
		},
		{
			"bookings entirely outside the window are ignored",
			[]Interval{iv(-120, -60), iv(300, 360)}, 0,
			[]Interval{iv(0, 240)},
		},
		{
			"two bookings leave three gaps",
			[]Interval{iv(60, 90), iv(150, 180)}, 0,
			[]Interval{iv(0, 60), iv(90, 150), iv(180, 240)},
		},
		{
			// The reason MergeIntervals merges touching spans. Back-to-back
			// bookings are one block, and a zero-length "free slot" between
			// them would render as a bookable gap of no duration.
			"back-to-back bookings produce no zero-length gap",
			[]Interval{iv(60, 120), iv(120, 180)}, 0,
			[]Interval{iv(0, 60), iv(180, 240)},
		},
		{
			"unsorted and overlapping input is normalised first",
			[]Interval{iv(150, 180), iv(60, 120), iv(100, 130)}, 0,
			[]Interval{iv(0, 60), iv(130, 150), iv(180, 240)},
		},
		{
			// A 15-minute sliver is not a bookable slot: the minimum booking
			// duration is 15 minutes, so anything shorter is noise in the UI.
			"gaps shorter than minDuration are discarded",
			[]Interval{iv(10, 60), iv(70, 240)}, 15 * time.Minute,
			nil,
		},
		{
			"a gap exactly equal to minDuration is kept",
			[]Interval{iv(0, 60), iv(75, 240)}, 15 * time.Minute,
			[]Interval{iv(60, 75)},
		},
		{
			"minDuration filters slivers but keeps real gaps",
			[]Interval{iv(30, 40), iv(45, 120)}, 15 * time.Minute,
			[]Interval{iv(0, 30), iv(120, 240)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertIntervals(t, FreeSlots(window, tc.busy, tc.minDuration), tc.want)
		})
	}
}

// TestFreeSlots_EmptyWindow covers the degenerate window.
//
// An inverted or zero-length window yields nothing rather than a negative-length
// slot. The API can be handed start >= end by a client, and the answer is "no
// availability", not a slot that runs backwards.
func TestFreeSlots_EmptyWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		window Interval
	}{
		{"zero length", iv(60, 60)},
		{"inverted", iv(120, 60)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FreeSlots(tc.window, nil, 0); len(got) != 0 {
				t.Errorf("got %s, want no free slots", fmtIntervals(got))
			}
		})
	}
}

// TestFreeSlots_NoZeroLengthOrInverted is a property check across the cases
// above: whatever FreeSlots returns must be usable as a booking.
//
// Table tests assert specific outputs; this asserts an invariant that holds for
// every output, which is what catches a case nobody thought to tabulate.
func TestFreeSlots_NoZeroLengthOrInverted(t *testing.T) {
	window := iv(0, 240)
	busySets := [][]Interval{
		nil,
		{iv(0, 240)},
		{iv(-60, 30)},
		{iv(210, 300)},
		{iv(60, 120), iv(120, 180)},
		{iv(0, 10), iv(10, 20), iv(20, 30)},
		{iv(30, 40), iv(35, 50), iv(200, 260)},
		{iv(240, 300), iv(-120, 0)},
	}

	for i, busy := range busySets {
		for _, got := range FreeSlots(window, busy, 0) {
			if got.IsEmpty() {
				t.Errorf("busy set %d: returned an empty or inverted slot %s", i, fmtIntervals([]Interval{got}))
			}
			if got.Start.Before(window.Start) || got.End.After(window.End) {
				t.Errorf("busy set %d: slot %s escapes the window", i, fmtIntervals([]Interval{got}))
			}
		}
	}
}

func TestIsFree(t *testing.T) {
	cases := []struct {
		name      string
		candidate Interval
		busy      []Interval
		want      bool
	}{
		{"nothing booked", iv(0, 60), nil, true},
		{"identical to a booking", iv(0, 60), []Interval{iv(0, 60)}, false},
		{"inside a booking", iv(15, 45), []Interval{iv(0, 60)}, false},
		{"straddling the start of a booking", iv(-30, 30), []Interval{iv(0, 60)}, false},
		{"straddling the end of a booking", iv(30, 90), []Interval{iv(0, 60)}, false},
		{"enclosing a booking", iv(-30, 90), []Interval{iv(0, 60)}, false},
		// Half-open bounds: touching is not overlapping. These two are the
		// back-to-back case, and the reason the schema uses '[)'.
		{"ending exactly when a booking starts", iv(-60, 0), []Interval{iv(0, 60)}, true},
		{"starting exactly when a booking ends", iv(60, 120), []Interval{iv(0, 60)}, true},
		{"clear of several bookings", iv(65, 85), []Interval{iv(0, 60), iv(90, 120)}, true},
		{"clashing with the second of several", iv(100, 110), []Interval{iv(0, 60), iv(90, 120)}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFree(tc.candidate, tc.busy); got != tc.want {
				t.Errorf("IsFree = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIntervalOverlapsIsSymmetric(t *testing.T) {
	pairs := [][2]Interval{
		{iv(0, 60), iv(30, 90)},
		{iv(0, 60), iv(60, 120)},
		{iv(0, 60), iv(0, 60)},
		{iv(0, 120), iv(30, 60)},
		{iv(0, 60), iv(120, 180)},
	}

	for _, p := range pairs {
		if a, b := p[0].Overlaps(p[1]), p[1].Overlaps(p[0]); a != b {
			t.Errorf("%s vs %s: Overlaps disagrees by argument order (%v / %v)",
				fmtIntervals(p[:1]), fmtIntervals(p[1:]), a, b)
		}
	}
}

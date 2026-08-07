package service

import (
	"testing"
	"time"

	// Embedded timezone data so LoadLocation works identically on every
	// platform. Windows has no system zoneinfo, and the DST test below would
	// otherwise pass locally and skip in CI, or the reverse.
	_ "time/tzdata"
)

// Tests describe intervals as minute offsets from a fixed base rather than as
// wall-clock literals. The arithmetic is what is under test, and iv(0, 60) is
// easier to check by eye than a pair of RFC 3339 strings.
var base = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }

func iv(startMin, endMin int) Interval {
	return Interval{Start: at(startMin), End: at(endMin)}
}

// fmtIntervals renders intervals as minute offsets so a failure reads as
// "[{0 60}]" rather than as two full timestamps per interval.
func fmtIntervals(got []Interval) string {
	if len(got) == 0 {
		return "[]"
	}
	out := "["
	for i, g := range got {
		if i > 0 {
			out += " "
		}
		out += time.Duration(g.Start.Sub(base)).String() + "-" + time.Duration(g.End.Sub(base)).String()
	}
	return out + "]"
}

func assertIntervals(t *testing.T, got, want []Interval) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d intervals %s, want %d %s", len(got), fmtIntervals(got), len(want), fmtIntervals(want))
	}
	for i := range want {
		if !got[i].Start.Equal(want[i].Start) || !got[i].End.Equal(want[i].End) {
			t.Errorf("interval %d: got %s, want %s", i, fmtIntervals(got[i:i+1]), fmtIntervals(want[i:i+1]))
		}
	}
}

func TestMergeIntervals(t *testing.T) {
	cases := []struct {
		name string
		in   []Interval
		want []Interval
	}{
		{"empty input", nil, nil},
		{"single interval", []Interval{iv(0, 60)}, []Interval{iv(0, 60)}},
		{
			"disjoint intervals are left alone",
			[]Interval{iv(0, 60), iv(120, 180)},
			[]Interval{iv(0, 60), iv(120, 180)},
		},
		{
			"overlapping intervals merge",
			[]Interval{iv(0, 60), iv(30, 90)},
			[]Interval{iv(0, 90)},
		},
		{
			// The case the doc comment calls out: 09:00-10:00 and 10:00-11:00
			// are one continuous busy block. Left adjacent, FreeSlots would
			// emit a zero-length gap between them.
			"touching intervals merge",
			[]Interval{iv(0, 60), iv(60, 120)},
			[]Interval{iv(0, 120)},
		},
		{
			// A fully-contained interval must not shrink the block it sits in.
			"contained interval does not shorten the enclosing one",
			[]Interval{iv(0, 120), iv(30, 60)},
			[]Interval{iv(0, 120)},
		},
		{
			"unsorted input is ordered",
			[]Interval{iv(120, 180), iv(0, 60)},
			[]Interval{iv(0, 60), iv(120, 180)},
		},
		{
			"same start, longer second",
			[]Interval{iv(0, 30), iv(0, 90)},
			[]Interval{iv(0, 90)},
		},
		{
			// Zero-length spans occupy no time and must not appear as busy.
			"zero-length intervals are dropped",
			[]Interval{iv(0, 0), iv(60, 120)},
			[]Interval{iv(60, 120)},
		},
		{"only zero-length intervals", []Interval{iv(0, 0), iv(30, 30)}, nil},
		{
			"three-way chain collapses to one",
			[]Interval{iv(0, 60), iv(45, 120), iv(100, 200)},
			[]Interval{iv(0, 200)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertIntervals(t, MergeIntervals(tc.in), tc.want)
		})
	}
}

// TestMergeIntervals_DoesNotMutateInput pins the promise the doc comment makes.
//
// MergeIntervals sorts, and sorting in place would reorder a slice the caller
// derived from query results. That reordering would be invisible here and
// surface somewhere unrelated, which is the worst kind of bug to find later.
func TestMergeIntervals_DoesNotMutateInput(t *testing.T) {
	in := []Interval{iv(120, 180), iv(0, 60), iv(60, 90)}
	before := make([]Interval, len(in))
	copy(before, in)

	MergeIntervals(in)

	assertIntervals(t, in, before)
}

// TestMergeIntervals_TouchingAcrossDST checks that merging follows elapsed time
// rather than wall-clock arithmetic.
//
// On 8 March 2026 New York skips 02:00-03:00. A block ending at 01:30 EST and
// one starting at 03:30 EDT are 1 hour apart in real time even though the clock
// labels differ by two, so they must NOT merge — and 01:30 to 03:00 EDT is a
// single continuous stretch that must. Everything in this package compares
// instants, so this holds; the test exists because a future refactor reaching
// for date components instead would break it silently.
func TestMergeIntervals_TouchingAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}

	// 01:30 EST, then the clock jumps 02:00 -> 03:00.
	before := time.Date(2026, 3, 8, 1, 30, 0, 0, ny)
	afterJump := before.Add(time.Hour) // 03:30 EDT by the wall clock

	merged := MergeIntervals([]Interval{
		{Start: before.Add(-time.Hour), End: before},
		{Start: before, End: afterJump},
	})

	// Two spans meeting exactly at `before` are one block, regardless of the
	// wall-clock discontinuity an hour later.
	assertIntervals(t, merged, []Interval{
		{Start: before.Add(-time.Hour), End: afterJump},
	})

	if got := merged[0].Duration(); got != 2*time.Hour {
		t.Errorf("merged block spans %s, want 2h of elapsed time", got)
	}
}

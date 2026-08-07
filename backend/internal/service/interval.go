// Package service holds business rules and orchestrates the store layer.
//
// Handlers translate HTTP; stores translate SQL; everything that constitutes a
// decision about how booking works lives here.
package service

import (
	"sort"
	"time"
)

// Interval is a half-open span [Start, End).
//
// Half-open is the same convention as the tstzrange('[)') in the exclusion
// constraint, and it is what makes back-to-back bookings work: a slot ending
// at 11:00 and one starting at 11:00 do not overlap. Using closed intervals
// anywhere in this file would reintroduce the off-by-one the schema was
// written to avoid.
type Interval struct {
	Start time.Time
	End   time.Time
}

// IsEmpty reports whether the interval covers no time.
func (iv Interval) IsEmpty() bool { return !iv.End.After(iv.Start) }

// Duration returns the length of the interval.
func (iv Interval) Duration() time.Duration { return iv.End.Sub(iv.Start) }

// Overlaps reports whether two half-open intervals share any instant.
func (iv Interval) Overlaps(other Interval) bool {
	return iv.Start.Before(other.End) && other.Start.Before(iv.End)
}

// MergeIntervals normalises a set of intervals into sorted, non-overlapping
// spans, joining any that overlap or touch.
//
// Touching intervals are merged, not left adjacent: 09:00-10:00 and
// 10:00-11:00 describe one continuous busy block from 09:00 to 11:00, and
// leaving them separate would make FreeSlots emit a zero-length gap between
// them.
func MergeIntervals(intervals []Interval) []Interval {
	if len(intervals) == 0 {
		return nil
	}

	// Copy before sorting: callers pass slices derived from query results and
	// should not observe them reordered underneath.
	sorted := make([]Interval, 0, len(intervals))
	for _, iv := range intervals {
		if !iv.IsEmpty() {
			sorted = append(sorted, iv)
		}
	}
	if len(sorted) == 0 {
		return nil
	}

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start.Equal(sorted[j].Start) {
			return sorted[i].End.Before(sorted[j].End)
		}
		return sorted[i].Start.Before(sorted[j].Start)
	})

	merged := []Interval{sorted[0]}
	for _, iv := range sorted[1:] {
		last := &merged[len(merged)-1]
		if iv.Start.After(last.End) {
			merged = append(merged, iv)
			continue
		}
		// Overlapping or touching: extend, but only if this interval reaches
		// further. A fully-contained interval must not shrink the block.
		if iv.End.After(last.End) {
			last.End = iv.End
		}
	}
	return merged
}

// FreeSlots subtracts busy intervals from a window, returning the gaps.
//
// Callers pass minDuration to discard slivers: a two-minute gap between
// meetings is not a bookable slot, and surfacing it in the UI would be noise.
func FreeSlots(window Interval, busy []Interval, minDuration time.Duration) []Interval {
	if window.IsEmpty() {
		return nil
	}

	free := make([]Interval, 0)
	cursor := window.Start

	for _, b := range MergeIntervals(busy) {
		// Ignore busy time outside the window entirely.
		if !b.End.After(window.Start) {
			continue
		}
		if !b.Start.Before(window.End) {
			break
		}

		if b.Start.After(cursor) {
			gap := Interval{Start: cursor, End: minTime(b.Start, window.End)}
			if gap.Duration() >= minDuration {
				free = append(free, gap)
			}
		}
		if b.End.After(cursor) {
			cursor = b.End
		}
		if !cursor.Before(window.End) {
			return free
		}
	}

	if cursor.Before(window.End) {
		gap := Interval{Start: cursor, End: window.End}
		if gap.Duration() >= minDuration {
			free = append(free, gap)
		}
	}
	return free
}

// IsFree reports whether the candidate interval is entirely unoccupied.
//
// This is a read-side convenience for showing availability, never the
// authority on whether a booking may proceed: between this returning true and
// an insert committing, another request can take the slot. The exclusion
// constraint is what actually decides.
func IsFree(candidate Interval, busy []Interval) bool {
	for _, b := range busy {
		if candidate.Overlaps(b) {
			return false
		}
	}
	return true
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

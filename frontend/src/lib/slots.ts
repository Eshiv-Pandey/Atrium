import type { Interval } from '@/api/schemas'
import { MS_PER_MINUTE, SLOT_MINUTES } from './time'

export type Slot = {
  start: Date
  end: Date
  /** Overlaps a confirmed booking. */
  busy: boolean
  /** Already ended. Free but unbookable. */
  past: boolean
}

/**
 * buildSlots divides a window into fixed slots and marks each one.
 *
 * Pure, and separated from the component on purpose: this is where the
 * off-by-one lives. It is also the piece worth testing directly, because the
 * boundary cases — a booking ending exactly when a slot starts, a slot
 * straddling "now" — are painful to set up through a rendered component and
 * trivial to assert on here.
 *
 * `busy` intervals come straight from the API. They are already merged and
 * sorted server-side, but nothing here depends on that.
 */
export function buildSlots(
  windowStart: Date,
  windowEnd: Date,
  busy: Interval[],
  now: Date,
  slotMinutes: number = SLOT_MINUTES,
): Slot[] {
  const slots: Slot[] = []
  const step = slotMinutes * MS_PER_MINUTE

  const busyRanges = busy.map((b) => ({
    start: new Date(b.start).getTime(),
    end: new Date(b.end).getTime(),
  }))

  for (let t = windowStart.getTime(); t + step <= windowEnd.getTime(); t += step) {
    const start = t
    const end = t + step

    slots.push({
      start: new Date(start),
      end: new Date(end),
      // Half-open comparison on both sides, matching the exclusion constraint's
      // '[)' bounds. A booking that ends at 11:00 does not make the 11:00 slot
      // busy — strict inequality on both ends is what makes back-to-back
      // bookings legal here as well as in the database.
      busy: busyRanges.some((b) => b.start < end && b.end > start),
      past: end <= now.getTime(),
    })
  }

  return slots
}

/**
 * selectionFrom resolves two clicked slots into an ordered range.
 *
 * Clicks arrive in whatever order the user makes them — dragging backwards
 * across a timeline is normal — so the anchor is not necessarily the earlier
 * one. Returning a normalised range means every caller downstream can assume
 * start < end.
 */
export function selectionFrom(anchor: Slot, focus: Slot): { start: Date; end: Date } {
  const first = anchor.start <= focus.start ? anchor : focus
  const second = anchor.start <= focus.start ? focus : anchor
  return { start: first.start, end: second.end }
}

/**
 * isSelectableRange reports whether a range can be booked.
 *
 * A range is only offered if every slot inside it is free. Without this a user
 * could anchor before a meeting and click after it, producing a selection that
 * spans someone else's booking and is rejected by the server — a 409 the UI
 * could have prevented, on a screen that was showing the conflict the whole
 * time.
 */
export function isSelectableRange(
  slots: Slot[],
  start: Date,
  end: Date,
): boolean {
  const covered = slots.filter(
    (s) => s.start.getTime() >= start.getTime() && s.end.getTime() <= end.getTime(),
  )
  return covered.length > 0 && covered.every((s) => !s.busy && !s.past)
}

/** Groups slots by hour, for a timeline that renders one row per hour. */
export function groupByHour(slots: Slot[]): { label: Date; slots: Slot[] }[] {
  const rows: { label: Date; slots: Slot[] }[] = []

  for (const slot of slots) {
    const hourStart = new Date(slot.start)
    hourStart.setMinutes(0, 0, 0)

    const last = rows[rows.length - 1]
    if (last && last.label.getTime() === hourStart.getTime()) {
      last.slots.push(slot)
    } else {
      rows.push({ label: hourStart, slots: [slot] })
    }
  }

  return rows
}

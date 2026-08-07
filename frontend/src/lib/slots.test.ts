import type { Interval } from '@/api/schemas'
import {
  buildSlots,
  groupByHour,
  isSelectableRange,
  selectionFrom,
} from './slots'

// Local-time construction throughout. buildSlots compares epoch milliseconds,
// so the zone the test runs in does not matter — but writing the fixtures in
// local terms keeps them readable next to the assertions.
const at = (h: number, m = 0) => new Date(2026, 5, 15, h, m, 0, 0)

const busyFrom = (startHour: number, endHour: number): Interval[] => [
  { start: at(startHour).toISOString(), end: at(endHour).toISOString() },
]

describe('buildSlots', () => {
  it('divides the window into fixed slots', () => {
    const slots = buildSlots(at(9), at(10), [], at(8), 15)

    expect(slots).toHaveLength(4)
    expect(slots[0].start).toEqual(at(9))
    expect(slots[0].end).toEqual(at(9, 15))
    expect(slots[3].end).toEqual(at(10))
  })

  it('never emits a slot that would run past the window', () => {
    // 50 minutes at 15-minute steps is three whole slots and a 5-minute
    // remainder. Emitting a short fourth would offer a booking the grid does not
    // actually contain.
    const slots = buildSlots(at(9), at(9, 50), [], at(8), 15)

    expect(slots).toHaveLength(3)
    expect(slots[2].end).toEqual(at(9, 45))
  })

  it('returns nothing for an empty or inverted window', () => {
    expect(buildSlots(at(9), at(9), [], at(8), 15)).toEqual([])
    expect(buildSlots(at(10), at(9), [], at(8), 15)).toEqual([])
  })

  it('marks only the slots a booking actually overlaps', () => {
    const slots = buildSlots(at(9), at(11), busyFrom(9, 10), at(8), 60)

    expect(slots.map((s) => s.busy)).toEqual([true, false])
  })

  // The half-open boundary, from both sides. This is the same '[)' rule the
  // exclusion constraint encodes, and getting it wrong here would grey out a
  // slot the server would happily accept.
  it('leaves the slot starting exactly when a booking ends free', () => {
    const slots = buildSlots(at(9), at(11), busyFrom(9, 10), at(8), 60)

    expect(slots[1].start).toEqual(at(10))
    expect(slots[1].busy).toBe(false)
  })

  it('leaves the slot ending exactly when a booking starts free', () => {
    const slots = buildSlots(at(9), at(11), busyFrom(10, 11), at(8), 60)

    expect(slots[0].end).toEqual(at(10))
    expect(slots[0].busy).toBe(false)
  })

  it('marks a slot busy when a booking covers only part of it', () => {
    // A 20-minute meeting inside an hour slot still makes the hour unbookable.
    const partial: Interval[] = [
      { start: at(9, 20).toISOString(), end: at(9, 40).toISOString() },
    ]
    const slots = buildSlots(at(9), at(10), partial, at(8), 60)

    expect(slots[0].busy).toBe(true)
  })

  it('handles unsorted and overlapping busy intervals', () => {
    // The API merges and sorts these, but nothing in buildSlots depends on it,
    // and this is the assertion that keeps that true.
    const messy: Interval[] = [
      { start: at(11).toISOString(), end: at(12).toISOString() },
      { start: at(9).toISOString(), end: at(9, 30).toISOString() },
      { start: at(9, 15).toISOString(), end: at(10).toISOString() },
    ]
    const slots = buildSlots(at(9), at(12), messy, at(8), 60)

    expect(slots.map((s) => s.busy)).toEqual([true, false, true])
  })

  it('marks a slot past only once it has fully ended', () => {
    // `past` is end <= now, so the slot containing "now" is still bookable for
    // its remaining minutes. Marking it past would make the current slot
    // unclickable for up to fifteen minutes.
    const slots = buildSlots(at(9), at(12), [], at(10, 30), 60)

    expect(slots.map((s) => s.past)).toEqual([true, false, false])
  })

  it('treats a slot ending exactly at now as past', () => {
    const slots = buildSlots(at(9), at(11), [], at(10), 60)

    expect(slots[0].past).toBe(true)
    expect(slots[1].past).toBe(false)
  })
})

describe('selectionFrom', () => {
  const slots = buildSlots(at(9), at(12), [], at(8), 60)

  it('orders a forward selection', () => {
    expect(selectionFrom(slots[0], slots[2])).toEqual({
      start: at(9),
      end: at(12),
    })
  })

  it('normalises a backwards drag to the same range', () => {
    // Dragging right-to-left across a timeline is ordinary, so the anchor is not
    // necessarily the earlier slot. Every caller downstream assumes start < end.
    expect(selectionFrom(slots[2], slots[0])).toEqual({
      start: at(9),
      end: at(12),
    })
  })

  it('handles a single slot selected by itself', () => {
    expect(selectionFrom(slots[1], slots[1])).toEqual({
      start: at(10),
      end: at(11),
    })
  })
})

describe('isSelectableRange', () => {
  it('accepts a range whose every slot is free', () => {
    const slots = buildSlots(at(9), at(12), [], at(8), 60)
    expect(isSelectableRange(slots, at(9), at(11))).toBe(true)
  })

  it('rejects a range that spans someone else’s booking', () => {
    // The case this exists for: anchor before a meeting, click after it. Without
    // the check the UI would submit a selection the server refuses with a 409,
    // on a screen that was showing the conflict the whole time.
    const slots = buildSlots(at(9), at(12), busyFrom(10, 11), at(8), 60)
    expect(isSelectableRange(slots, at(9), at(12))).toBe(false)
  })

  it('rejects a range containing a past slot', () => {
    const slots = buildSlots(at(9), at(12), [], at(10), 60)
    expect(isSelectableRange(slots, at(9), at(11))).toBe(false)
  })

  it('rejects a range that covers no whole slot', () => {
    // A zero-width or sub-slot range covers nothing, and `every` on an empty
    // array is true — so without the length guard this would report selectable.
    const slots = buildSlots(at(9), at(12), [], at(8), 60)
    expect(isSelectableRange(slots, at(9), at(9))).toBe(false)
    expect(isSelectableRange(slots, at(9, 10), at(9, 20))).toBe(false)
  })
})

describe('groupByHour', () => {
  it('puts the slots of one hour in a single row', () => {
    const rows = groupByHour(buildSlots(at(9), at(11), [], at(8), 15))

    expect(rows).toHaveLength(2)
    expect(rows[0].label).toEqual(at(9))
    expect(rows[0].slots).toHaveLength(4)
    expect(rows[1].label).toEqual(at(10))
  })

  it('labels a row by the containing hour, not by the slot start', () => {
    // Two 90-minute slots: 09:00–10:30 and 10:30–12:00. The second starts at
    // half past, and its row label is still 10:00 — the label names the hour the
    // row sits in, which is what a timeline axis renders.
    const rows = groupByHour(buildSlots(at(9), at(12), [], at(8), 90))

    expect(rows.map((r) => r.label)).toEqual([at(9), at(10)])
  })

  it('returns nothing for no slots', () => {
    expect(groupByHour([])).toEqual([])
  })
})

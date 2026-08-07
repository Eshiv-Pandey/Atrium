import {
  addDays,
  addMinutes,
  formatDuration,
  fromDateInputValue,
  isSameDay,
  roundUpToSlot,
  setTimeOfDay,
  startOfDay,
  toDateInputValue,
  toTimeInputValue,
  toWire,
} from './time'

describe('toWire', () => {
  it('emits an explicit offset the API will accept', () => {
    // The server rejects a naive timestamp rather than guessing a zone, so the
    // one thing that must never regress here is the trailing Z.
    const wire = toWire(new Date(Date.UTC(2026, 5, 15, 9, 30)))

    expect(wire).toBe('2026-06-15T09:30:00.000Z')
    expect(wire).toMatch(/(Z|[+-]\d{2}:\d{2})$/)
  })

  it('preserves the instant regardless of the local zone', () => {
    const instant = new Date(2026, 5, 15, 9, 30)
    expect(new Date(toWire(instant)).getTime()).toBe(instant.getTime())
  })
})

describe('startOfDay', () => {
  it('returns local midnight and does not mutate its argument', () => {
    const afternoon = new Date(2026, 5, 15, 14, 37, 12, 500)
    const midnight = startOfDay(afternoon)

    expect(midnight).toEqual(new Date(2026, 5, 15, 0, 0, 0, 0))
    // Mutating the input would corrupt a Date held in component state.
    expect(afternoon.getHours()).toBe(14)
  })
})

describe('addDays', () => {
  it('shifts by calendar days', () => {
    expect(addDays(new Date(2026, 5, 15, 9), 3)).toEqual(new Date(2026, 5, 18, 9))
    expect(addDays(new Date(2026, 5, 15, 9), -1)).toEqual(new Date(2026, 5, 14, 9))
  })

  it('crosses a month boundary', () => {
    expect(addDays(new Date(2026, 5, 30), 1)).toEqual(new Date(2026, 6, 1))
  })

  it('keeps the wall-clock time across a DST transition', () => {
    // The reason this uses setDate rather than adding 24 hours: on the ~2 days a
    // year a zone shifts, a day is 23 or 25 hours long and epoch arithmetic
    // lands an hour off. Asserted on the calendar fields, so it holds in every
    // zone — including those with no DST at all, where it is trivially true.
    const before = new Date(2026, 2, 7, 10, 0, 0, 0)
    const after = addDays(before, 1)

    expect(after.getDate()).toBe(8)
    expect(after.getHours()).toBe(10)
    expect(after.getMinutes()).toBe(0)
  })
})

describe('addMinutes', () => {
  it('adds and subtracts elapsed minutes', () => {
    const base = new Date(2026, 5, 15, 9, 50)
    expect(addMinutes(base, 15)).toEqual(new Date(2026, 5, 15, 10, 5))
    expect(addMinutes(base, -60)).toEqual(new Date(2026, 5, 15, 8, 50))
  })
})

describe('setTimeOfDay', () => {
  it('keeps the calendar day and replaces the time', () => {
    const d = setTimeOfDay(new Date(2026, 5, 15, 23, 59, 59, 999), 9)

    expect(d).toEqual(new Date(2026, 5, 15, 9, 0, 0, 0))
  })

  it('accepts an explicit minute', () => {
    expect(setTimeOfDay(new Date(2026, 5, 15), 18, 30)).toEqual(
      new Date(2026, 5, 15, 18, 30, 0, 0),
    )
  })
})

describe('roundUpToSlot', () => {
  it('snaps forward to the next boundary', () => {
    // "Now" is never 10:00, it is 10:07:23. Snapping forward gives a start that
    // lines up with the grid and cannot have fallen into the past by submit.
    expect(roundUpToSlot(new Date(2026, 5, 15, 10, 7, 23, 400), 15)).toEqual(
      new Date(2026, 5, 15, 10, 15, 0, 0),
    )
  })

  it('leaves a time already on a boundary alone, but clears sub-minute parts', () => {
    expect(roundUpToSlot(new Date(2026, 5, 15, 10, 30, 0, 0), 15)).toEqual(
      new Date(2026, 5, 15, 10, 30, 0, 0),
    )
    expect(roundUpToSlot(new Date(2026, 5, 15, 10, 30, 44, 999), 15)).toEqual(
      new Date(2026, 5, 15, 10, 30, 0, 0),
    )
  })

  it('rolls into the next hour, and the next day', () => {
    expect(roundUpToSlot(new Date(2026, 5, 15, 10, 52), 15)).toEqual(
      new Date(2026, 5, 15, 11, 0, 0, 0),
    )
    // 23:58 rounds past midnight; setMinutes carries the date for us.
    expect(roundUpToSlot(new Date(2026, 5, 15, 23, 58), 15)).toEqual(
      new Date(2026, 5, 16, 0, 0, 0, 0),
    )
  })
})

describe('date and time input values', () => {
  it('formats a date in local terms, not UTC', () => {
    // toISOString would give the UTC date, which is the previous or next day for
    // anyone whose offset pushes midnight across the boundary — a user in UTC+10
    // picking "today" would see yesterday selected.
    const local = new Date(2026, 0, 5, 23, 30)

    expect(toDateInputValue(local)).toBe('2026-01-05')
    expect(toDateInputValue(local)).toBe(
      `${local.getFullYear()}-${String(local.getMonth() + 1).padStart(2, '0')}-${String(
        local.getDate(),
      ).padStart(2, '0')}`,
    )
  })

  it('zero-pads single-digit months and days', () => {
    expect(toDateInputValue(new Date(2026, 2, 9))).toBe('2026-03-09')
  })

  it('round-trips through fromDateInputValue as local midnight', () => {
    const parsed = fromDateInputValue('2026-06-15')

    expect(parsed).toEqual(new Date(2026, 5, 15, 0, 0, 0, 0))
    expect(toDateInputValue(parsed)).toBe('2026-06-15')
  })

  it('formats a 24-hour zero-padded time', () => {
    expect(toTimeInputValue(new Date(2026, 5, 15, 9, 5))).toBe('09:05')
    expect(toTimeInputValue(new Date(2026, 5, 15, 18, 30))).toBe('18:30')
    expect(toTimeInputValue(new Date(2026, 5, 15, 0, 0))).toBe('00:00')
  })
})

describe('isSameDay', () => {
  it('compares calendar days rather than elapsed time', () => {
    expect(isSameDay(new Date(2026, 5, 15, 0, 1), new Date(2026, 5, 15, 23, 59))).toBe(
      true,
    )
    // Under two hours apart, but a different day.
    expect(isSameDay(new Date(2026, 5, 15, 23, 30), new Date(2026, 5, 16, 0, 30))).toBe(
      false,
    )
  })

  it('does not confuse the same day across years', () => {
    expect(isSameDay(new Date(2026, 5, 15), new Date(2027, 5, 15))).toBe(false)
  })
})

describe('formatDuration', () => {
  it.each([
    [15, '15m'],
    [45, '45m'],
    [60, '1h'],
    [90, '1h 30m'],
    [120, '2h'],
    [480, '8h'],
  ])('renders %i minutes as %s', (minutes, expected) => {
    const start = new Date(2026, 5, 15, 9)
    expect(formatDuration(start, addMinutes(start, minutes))).toBe(expected)
  })

  it('accepts wire strings as well as Dates', () => {
    expect(
      formatDuration('2026-06-15T09:00:00Z', '2026-06-15T10:30:00Z'),
    ).toBe('1h 30m')
  })
})

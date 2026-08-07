/**
 * Time helpers for the booking UI.
 *
 * All of these work in the browser's local zone and hand RFC 3339 strings with
 * an offset to the API. That is the one rule worth stating plainly: a naive
 * local timestamp on the wire is the most common source of booking bugs,
 * because the server has no way to know which zone it was written in and will
 * guess — usually UTC, usually wrong, and usually only in summer.
 */

/** Minutes per slot on the timeline. Matches the backend's 15-minute minimum. */
export const SLOT_MINUTES = 15

export const MS_PER_MINUTE = 60_000
export const MS_PER_HOUR = 60 * MS_PER_MINUTE

/**
 * toWire converts a Date to the format the API accepts.
 *
 * toISOString always emits UTC with a `Z`, which is a valid RFC 3339 offset —
 * the instant is preserved exactly, and the server stores timestamptz anyway.
 * Nothing here depends on the local offset surviving the round trip; the
 * frontend re-derives it on the way back.
 */
export function toWire(date: Date): string {
  return date.toISOString()
}

/** Parses an API timestamp. */
export function fromWire(iso: string): Date {
  return new Date(iso)
}

/**
 * startOfDay returns local midnight for a date.
 *
 * Deliberately built from the calendar fields rather than by subtracting the
 * time-of-day in milliseconds. On the ~2 days a year a zone shifts, a day is
 * 23 or 25 hours long, and arithmetic on the epoch lands an hour off.
 */
export function startOfDay(date: Date): Date {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  return d
}

/** addDays shifts by calendar days, which is DST-safe for the same reason. */
export function addDays(date: Date, days: number): Date {
  const d = new Date(date)
  d.setDate(d.getDate() + days)
  return d
}

export function addMinutes(date: Date, minutes: number): Date {
  return new Date(date.getTime() + minutes * MS_PER_MINUTE)
}

/**
 * setTimeOfDay returns the same calendar day at the given hour and minute.
 *
 * Used to build a business-hours window (09:00–18:00) on a chosen date.
 */
export function setTimeOfDay(date: Date, hours: number, minutes = 0): Date {
  const d = new Date(date)
  d.setHours(hours, minutes, 0, 0)
  return d
}

/**
 * roundUpToSlot snaps a time forward to the next slot boundary.
 *
 * The default booking window starts from "now", and "now" is never 10:00 — it
 * is 10:07:23. Snapping forward gives a start time that lines up with the
 * timeline grid and cannot land in the past between render and submit.
 */
export function roundUpToSlot(date: Date, slotMinutes = SLOT_MINUTES): Date {
  const d = new Date(date)
  d.setSeconds(0, 0)
  const remainder = d.getMinutes() % slotMinutes
  if (remainder !== 0) {
    d.setMinutes(d.getMinutes() + (slotMinutes - remainder))
  }
  return d
}

/** Formats a time for a timeline axis or a slot button: "2:30 PM". */
export function formatTime(value: Date | string): string {
  return timeFormatter.format(asDate(value))
}

/** Formats a date without the time: "Tue, 12 Aug". */
export function formatDate(value: Date | string): string {
  return dateFormatter.format(asDate(value))
}

/** Formats both, for a booking row: "Tue, 12 Aug, 2:30 PM". */
export function formatDateTime(value: Date | string): string {
  return `${formatDate(value)}, ${formatTime(value)}`
}

/**
 * formatRange collapses a same-day range: "Tue, 12 Aug · 2:30 – 4:00 PM".
 *
 * Repeating the date on both ends of a range that almost always falls within
 * one day is noise, and it makes the rare cross-midnight booking harder to
 * spot rather than easier.
 */
export function formatRange(start: Date | string, end: Date | string): string {
  const s = asDate(start)
  const e = asDate(end)

  if (isSameDay(s, e)) {
    return `${formatDate(s)} · ${formatTime(s)} – ${formatTime(e)}`
  }
  return `${formatDateTime(s)} – ${formatDateTime(e)}`
}

/** Formats a duration in whole hours and minutes: "1h 30m". */
export function formatDuration(start: Date | string, end: Date | string): string {
  const minutes = Math.round(
    (asDate(end).getTime() - asDate(start).getTime()) / MS_PER_MINUTE,
  )
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60

  if (hours === 0) return `${rest}m`
  if (rest === 0) return `${hours}h`
  return `${hours}h ${rest}m`
}

export function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  )
}

/**
 * toDateInputValue formats for an <input type="date">.
 *
 * That input requires YYYY-MM-DD in *local* terms. toISOString would give the
 * UTC date, which is the previous or next day for anyone whose offset pushes
 * midnight across the boundary — a user in UTC+10 picking "today" would see
 * yesterday selected.
 */
export function toDateInputValue(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

/** Parses a YYYY-MM-DD value from a date input as local midnight. */
export function fromDateInputValue(value: string): Date {
  const [y, m, d] = value.split('-').map(Number)
  return new Date(y, m - 1, d)
}

/** Formats for an <input type="time">: HH:MM, local, 24-hour. */
export function toTimeInputValue(date: Date): string {
  const h = String(date.getHours()).padStart(2, '0')
  const m = String(date.getMinutes()).padStart(2, '0')
  return `${h}:${m}`
}

function asDate(value: Date | string): Date {
  return typeof value === 'string' ? new Date(value) : value
}

// Formatters are constructed once. Intl.DateTimeFormat is expensive to build
// and these render in every row of every list.
//
// The undefined locale means "whatever this browser is set to", which is the
// right default: a booking app should show times the way its user already
// reads them, not the way the developer's machine does.
const timeFormatter = new Intl.DateTimeFormat(undefined, {
  hour: 'numeric',
  minute: '2-digit',
})

const dateFormatter = new Intl.DateTimeFormat(undefined, {
  weekday: 'short',
  day: 'numeric',
  month: 'short',
})

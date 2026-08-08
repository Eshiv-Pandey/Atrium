import { useEffect, useId, useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import { addDays, fromDateInputValue, isSameDay, startOfDay, toDateInputValue } from '@/lib/time'

export type DatePickerProps = {
  label: string
  /** Selected day as YYYY-MM-DD, or undefined for no selection. */
  value: string | undefined
  /** Earliest selectable day as YYYY-MM-DD. Days before it are disabled. */
  min?: string
  onChange: (value: string | undefined) => void
  id?: string
  disabled?: boolean
}

const WEEKDAYS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']

/**
 * DatePicker is a click-to-pick calendar, built rather than borrowed.
 *
 * A native <input type="date"> is a text field first and a calendar second: the
 * visible part invites typing MM/DD/YYYY a segment at a time, which is the slow,
 * error-prone path most people fall into. This opens straight onto a month grid
 * so a date is one click, and keeps the same YYYY-MM-DD string contract the rest
 * of the filter code already speaks, so it drops in where a date Field was.
 *
 * No new dependency: the whole app hand-builds its components, and a calendar is
 * a month of buttons in a grid. The date maths reuses the DST-safe helpers in
 * lib/time rather than doing epoch arithmetic, for the same reason those helpers
 * exist.
 */
export function DatePicker({ label, value, min, onChange, id: providedId, disabled }: DatePickerProps) {
  const generatedId = useId()
  const id = providedId ?? generatedId

  const selected = value ? fromDateInputValue(value) : null
  const minDay = min ? startOfDay(fromDateInputValue(min)) : null

  const [open, setOpen] = useState(false)
  const [viewMonth, setViewMonth] = useState(() => startOfMonth(selected ?? new Date()))

  const containerRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  // Reopen on the month of the current selection rather than wherever the user
  // last browsed, so the highlighted day is always in view. Keyed off the string
  // so a fresh Date identity does not re-run this every render.
  useEffect(() => {
    if (open) setViewMonth(startOfMonth(value ? fromDateInputValue(value) : new Date()))
  }, [open, value])

  // Close on an outside click or Escape. A calendar that stays open after you
  // click away from it, or that Escape does not dismiss, feels broken in a way a
  // native control never does.
  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: PointerEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false)
        triggerRef.current?.focus()
      }
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const pick = (day: Date) => {
    onChange(toDateInputValue(day))
    setOpen(false)
    triggerRef.current?.focus()
  }

  const days = monthGrid(viewMonth)
  const atMinMonth =
    minDay != null && startOfMonth(viewMonth).getTime() <= startOfMonth(minDay).getTime()

  return (
    <div className="relative" ref={containerRef}>
      <label
        htmlFor={id}
        className="block text-[0.68rem] font-semibold uppercase tracking-[0.16em] text-muted-fg"
      >
        {label}
      </label>

      <button
        ref={triggerRef}
        id={id}
        type="button"
        disabled={disabled}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'mt-2 flex w-full items-center justify-between rounded-lg border border-input bg-background/60 px-3.5 py-2.5 text-sm',
          'transition-colors focus-visible:outline-none focus-visible:border-primary/60 focus-visible:ring-2 focus-visible:ring-ring/60',
          'disabled:cursor-not-allowed disabled:opacity-50',
          selected ? 'text-foreground' : 'text-muted-fg',
        )}
      >
        <span data-numeric>{selected ? formatFullDate(selected) : 'Pick a date'}</span>
        <CalendarIcon className="h-4 w-4 shrink-0 text-muted-fg" />
      </button>

      {open ? (
        <div
          role="dialog"
          aria-label={`Choose ${label.toLowerCase()}`}
          className={cn(
            'absolute left-0 top-full z-50 mt-2 w-[19rem] rounded-xl border border-border/70 bg-card p-3 text-card-fg',
            'shadow-[0_24px_60px_-24px_oklch(0_0_0/0.9)] backdrop-blur-sm',
          )}
        >
          <div className="mb-2 flex items-center justify-between">
            <button
              type="button"
              onClick={() => setViewMonth(addMonths(viewMonth, -1))}
              disabled={atMinMonth}
              aria-label="Previous month"
              className={cn(
                'flex h-8 w-8 items-center justify-center rounded-lg text-muted-fg transition-colors',
                'hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                'disabled:pointer-events-none disabled:opacity-30',
              )}
            >
              ←
            </button>
            <span
              aria-live="polite"
              className="font-display text-sm font-bold uppercase tracking-[-0.01em]"
              data-numeric
            >
              {formatMonth(viewMonth)}
            </span>
            <button
              type="button"
              onClick={() => setViewMonth(addMonths(viewMonth, 1))}
              aria-label="Next month"
              className={cn(
                'flex h-8 w-8 items-center justify-center rounded-lg text-muted-fg transition-colors',
                'hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
              )}
            >
              →
            </button>
          </div>

          <div className="grid grid-cols-7 gap-1">
            {WEEKDAYS.map((w) => (
              <div
                key={w}
                aria-hidden="true"
                className="flex h-8 items-center justify-center text-[0.62rem] font-semibold uppercase tracking-[0.12em] text-muted-fg/70"
              >
                {w}
              </div>
            ))}

            {days.map((day) => {
              const inMonth = day.getMonth() === viewMonth.getMonth()
              const isDisabled = minDay != null && startOfDay(day).getTime() < minDay.getTime()
              const isSelected = selected != null && isSameDay(day, selected)
              const isToday = isSameDay(day, new Date())

              return (
                <button
                  key={toDateInputValue(day)}
                  type="button"
                  disabled={isDisabled}
                  aria-pressed={isSelected}
                  aria-label={formatFullDate(day)}
                  onClick={() => pick(day)}
                  className={cn(
                    'flex h-9 items-center justify-center rounded-lg text-sm tabular-nums transition-colors',
                    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                    'disabled:pointer-events-none disabled:opacity-30',
                    !inMonth && 'text-muted-fg/50',
                    isSelected
                      ? 'bg-primary font-semibold text-primary-fg'
                      : 'hover:bg-muted hover:text-foreground',
                    !isSelected && isToday && 'ring-1 ring-inset ring-primary/50',
                    !isSelected && inMonth && !isDisabled && 'text-foreground',
                  )}
                >
                  {day.getDate()}
                </button>
              )
            })}
          </div>
        </div>
      ) : null}
    </div>
  )
}

/** Local midnight on the first of this date's month. */
function startOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

/** Shifts by whole calendar months, clamping the day (Jan 31 + 1 month = Feb). */
function addMonths(date: Date, months: number): Date {
  return new Date(date.getFullYear(), date.getMonth() + months, 1)
}

/**
 * monthGrid returns the 42 days (6 weeks) covering a month, padded on both ends
 * with the neighbouring months' days so every row is full. Six weeks rather than
 * a variable count keeps the popover a fixed height as the user pages months.
 */
function monthGrid(month: Date): Date[] {
  const first = startOfMonth(month)
  const gridStart = addDays(first, -first.getDay())
  return Array.from({ length: 42 }, (_, i) => addDays(gridStart, i))
}

const fullDateFormatter = new Intl.DateTimeFormat(undefined, {
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  year: 'numeric',
})

const monthFormatter = new Intl.DateTimeFormat(undefined, {
  month: 'long',
  year: 'numeric',
})

/** "Tue, 12 Aug 2026", for the trigger's readout. */
function formatFullDate(date: Date): string {
  return fullDateFormatter.format(date)
}

/** "August 2026", for the calendar header. */
function formatMonth(date: Date): string {
  return monthFormatter.format(date)
}

function CalendarIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="3" y="4.5" width="18" height="16" rx="2" />
      <path d="M3 9h18M8 2.5v4M16 2.5v4" />
    </svg>
  )
}

import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { groupByHour, isSelectableRange, selectionFrom, type Slot } from '@/lib/slots'
import { formatTime } from '@/lib/time'
import { cn } from '@/lib/utils'

export type Selection = { start: Date; end: Date }

/**
 * The fill for each slot state, shared by the grid and the legend.
 *
 * One record rather than two lists because a legend that disagrees with the
 * thing it explains is worse than no legend, and these drifted the moment the
 * palette changed.
 *
 * `past` carries an explicit fill rather than leaning on opacity: at slot size
 * a dashed border plus 50% opacity reads clearly, but at the legend's 12px it
 * is an empty box next to three other empty boxes.
 */
const slotFill = {
  free: 'border-border/60 bg-foreground/[0.04]',
  selected: 'border-primary bg-primary',
  busy: 'border-border/60 bg-muted/80',
  past: 'border-dashed border-border/60 bg-foreground/[0.02]',
} as const

/**
 * Timeline renders a day as selectable slots.
 *
 * The interaction is deliberately the same one a spreadsheet uses, because
 * that is the one people already know: click sets an anchor, shift-click (or a
 * drag, or shift-arrow) extends from it. Nothing here invents a gesture.
 *
 * Keyboard operation is not a retrofit. Every slot is reachable with the arrow
 * keys via a roving tabindex, so the whole day is one tab stop rather than
 * ninety-six, and shift-arrow extends the selection exactly as shift-drag
 * does. A booking timeline that can only be driven by a mouse excludes both
 * screen-reader users and the keyboard-first crowd who are usually the ones
 * booking a room in a hurry.
 */
export function Timeline({
  slots,
  selection,
  onSelect,
}: {
  slots: Slot[]
  selection: Selection | null
  onSelect: (selection: Selection | null) => void
}) {
  const [anchorIndex, setAnchorIndex] = useState<number | null>(null)
  const [focusIndex, setFocusIndex] = useState<number>(() =>
    firstSelectableIndex(slots),
  )
  const dragging = useRef(false)
  const containerRef = useRef<HTMLDivElement>(null)

  // A change of day or a refetch replaces every slot. Keeping a stale anchor
  // would let the next shift-click extend from a slot that no longer exists.
  useEffect(() => {
    setAnchorIndex(null)
    setFocusIndex(firstSelectableIndex(slots))
  }, [slots])

  // A drag can end anywhere — over the page, outside the window, in another
  // tab. Listening on the window rather than on the slots is what stops the
  // timeline from staying stuck in drag mode after the mouse comes back.
  useEffect(() => {
    const stop = () => {
      dragging.current = false
    }
    window.addEventListener('pointerup', stop)
    window.addEventListener('pointercancel', stop)
    return () => {
      window.removeEventListener('pointerup', stop)
      window.removeEventListener('pointercancel', stop)
    }
  }, [])

  // groupByHour partitions the slots in order, so each row's position in the
  // flat array is just the running total before it. Carrying that offset here
  // avoids an indexOf per slot inside the render — a scan of the whole day for
  // every one of its ninety-six buttons, on every keystroke.
  const rows = useMemo(() => {
    let offset = 0
    return groupByHour(slots).map((row) => {
      const startIndex = offset
      offset += row.slots.length
      return { ...row, startIndex }
    })
  }, [slots])

  const commit = (from: number, to: number) => {
    const range = selectionFrom(slots[from], slots[to])
    if (isSelectableRange(slots, range.start, range.end)) {
      onSelect(range)
    }
  }

  const startAt = (index: number) => {
    const slot = slots[index]
    if (slot.busy || slot.past) return
    setAnchorIndex(index)
    setFocusIndex(index)
    commit(index, index)
  }

  const extendTo = (index: number) => {
    if (anchorIndex === null) {
      startAt(index)
      return
    }
    setFocusIndex(index)
    commit(anchorIndex, index)
  }

  const onSlotKeyDown = (e: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const perRow = rows[0]?.slots.length ?? 4
    const deltas: Record<string, number> = {
      ArrowRight: 1,
      ArrowLeft: -1,
      ArrowDown: perRow,
      ArrowUp: -perRow,
    }

    let next: number | null = null
    if (e.key in deltas) {
      next = clamp(index + deltas[e.key], 0, slots.length - 1)
    } else if (e.key === 'Home') {
      next = 0
    } else if (e.key === 'End') {
      next = slots.length - 1
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      if (e.shiftKey) extendTo(index)
      else startAt(index)
      return
    } else if (e.key === 'Escape' && selection) {
      // Escape clears the selection rather than bubbling to close a dialog:
      // there is no dialog open at this point, and abandoning a half-made
      // selection is the only thing Escape could mean here.
      e.preventDefault()
      onSelect(null)
      setAnchorIndex(null)
      return
    }

    if (next === null) return
    e.preventDefault()

    if (e.shiftKey && anchorIndex !== null) {
      extendTo(next)
    } else {
      setFocusIndex(next)
    }
    focusSlot(containerRef.current, next)
  }

  return (
    <div ref={containerRef} className="space-y-1">
      {/*
        The instructions are visible rather than tucked into an aria-label.
        A drag-to-select affordance is not discoverable on its own, and a
        sighted keyboard user needs the hint just as much as a screen-reader
        user does.
      */}
      <p className="pb-2 text-sm text-muted-fg">
        Click a slot to start, then shift-click or drag to extend. Arrow keys
        move; shift-arrow extends.
      </p>

      {rows.map((row) => (
        <div key={row.label.toISOString()} className="flex items-center gap-3">
          <span className="w-16 shrink-0 text-right font-mono text-xs tabular-nums text-muted-fg">
            {formatTime(row.label)}
          </span>

          <div className="flex flex-1 gap-1">
            {row.slots.map((slot, slotIndex) => {
              const index = row.startIndex + slotIndex
              const selected =
                selection !== null &&
                slot.start >= selection.start &&
                slot.end <= selection.end
              const unavailable = slot.busy || slot.past

              return (
                <button
                  key={slot.start.toISOString()}
                  type="button"
                  // Roving tabindex: exactly one slot is in the tab order, and
                  // the arrow keys move which one. Ninety-six tab stops for a
                  // single control is its own accessibility failure.
                  tabIndex={index === focusIndex ? 0 : -1}
                  data-slot-index={index}
                  // aria-disabled rather than `disabled`: a disabled button is
                  // removed from the tab order and skipped by arrow-key
                  // navigation, so a screen-reader user would never learn that
                  // 2pm is taken — the slot would simply not be there.
                  aria-disabled={unavailable || undefined}
                  aria-pressed={selected}
                  aria-label={`${formatTime(slot.start)} to ${formatTime(slot.end)}${
                    slot.busy ? ', booked' : slot.past ? ', past' : ', available'
                  }`}
                  onPointerDown={() => {
                    if (unavailable) return
                    dragging.current = true
                    startAt(index)
                  }}
                  onPointerEnter={() => {
                    if (dragging.current) extendTo(index)
                  }}
                  onClick={(e) => {
                    if (unavailable) return
                    if (e.shiftKey) extendTo(index)
                  }}
                  onKeyDown={(e) => onSlotKeyDown(e, index)}
                  className={cn(
                    'h-10 flex-1 rounded-md border text-xs font-medium tabular-nums transition-colors',
                    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background',
                    selected
                      ? `${slotFill.selected} text-primary-fg shadow-[0_0_20px_-4px_oklch(var(--primary)/0.6)]`
                      : slot.busy
                        ? `cursor-not-allowed ${slotFill.busy} text-muted-fg`
                        : slot.past
                          ? `cursor-not-allowed ${slotFill.past} text-muted-fg opacity-60`
                          : `${slotFill.free} hover:border-primary/60 hover:bg-primary/10`,
                  )}
                >
                  {/*
                    The label is visual shorthand; the accessible name on the
                    button carries the full range and state.
                  */}
                  <span aria-hidden="true">
                    {slot.start.getMinutes() === 0
                      ? formatTime(slot.start)
                      : `:${String(slot.start.getMinutes()).padStart(2, '0')}`}
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}

/**
 * Legend explains the slot colours.
 *
 * Colour alone is not an accessible distinction, so the states also differ in
 * border and opacity — but a legend is what makes the grid readable at a
 * glance for everyone, which is cheaper than making each slot self-describing.
 */
export function TimelineLegend() {
  const items = [
    { label: 'Available', className: slotFill.free },
    { label: 'Selected', className: slotFill.selected },
    { label: 'Booked', className: slotFill.busy },
    { label: 'Past', className: slotFill.past },
  ]

  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-2 text-[0.68rem] font-semibold uppercase tracking-[0.1em] text-muted-fg">
      {items.map((item) => (
        <li key={item.label} className="flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className={cn('h-3 w-3 rounded-sm border', item.className)}
          />
          {item.label}
        </li>
      ))}
    </ul>
  )
}

function firstSelectableIndex(slots: Slot[]): number {
  const i = slots.findIndex((s) => !s.busy && !s.past)
  return i === -1 ? 0 : i
}

function focusSlot(container: HTMLElement | null, index: number) {
  container
    ?.querySelector<HTMLButtonElement>(`[data-slot-index="${index}"]`)
    ?.focus()
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max)
}

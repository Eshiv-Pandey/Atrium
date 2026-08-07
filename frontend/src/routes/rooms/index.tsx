import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { useRooms } from '@/api/hooks'
import { errorMessage } from '@/api/client'
import { requireAuth } from '@/lib/guards'
import {
  addMinutes,
  fromDateInputValue,
  roundUpToSlot,
  setTimeOfDay,
  toDateInputValue,
  toTimeInputValue,
  toWire,
} from '@/lib/time'
import { RoomCard, RoomCardSkeleton, formatAmenity } from '@/components/RoomCard'
import { EmptyState, ErrorState } from '@/components/ui/Card'
import { Field } from '@/components/ui/Field'
import { Button } from '@/components/ui/Button'

/**
 * Filters live in the URL rather than in component state.
 *
 * That makes a filtered view shareable and survivable: a member can send "the
 * 6-person rooms free at 2pm" to a colleague as a link, and a refresh does not
 * silently reset to the whole catalog. It also means the browser's Back button
 * steps through filter changes, which is what people expect it to do.
 *
 * `.catch()` on each field means a hand-edited or truncated URL degrades to
 * the default rather than throwing — a malformed link should show the catalog,
 * not an error page.
 */
const searchSchema = z.object({
  capacity: z.coerce.number().int().min(1).max(100).optional().catch(undefined),
  amenities: z.array(z.string()).optional().catch(undefined),
  /** Local calendar day, YYYY-MM-DD. Absent means "no time filter". */
  date: z.string().optional().catch(undefined),
  /** Local wall-clock HH:MM, paired with `date`. */
  from: z.string().optional().catch(undefined),
  to: z.string().optional().catch(undefined),
})

export const Route = createFileRoute('/rooms/')({
  validateSearch: searchSchema,
  beforeLoad: requireAuth,
  component: BrowseRooms,
})

const AMENITIES = ['quiet', 'whiteboard', 'tv', 'videoconf', 'casual']

function BrowseRooms() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const window = windowFromSearch(search)

  const rooms = useRooms({
    minCapacity: search.capacity,
    amenities: search.amenities,
    start: window?.start,
    end: window?.end,
  })

  const setSearch = (patch: Partial<typeof search>) => {
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })
  }

  const toggleAmenity = (amenity: string) => {
    const current = search.amenities ?? []
    const next = current.includes(amenity)
      ? current.filter((a) => a !== amenity)
      : [...current, amenity]
    setSearch({ amenities: next.length > 0 ? next : undefined })
  }

  const hasFilters =
    search.capacity !== undefined ||
    (search.amenities?.length ?? 0) > 0 ||
    search.date !== undefined

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Rooms</h1>
        <p className="mt-1 text-sm text-muted-fg">
          {window
            ? 'Showing whether each room is free in your chosen window.'
            : 'Pick a time to see what is actually free.'}
        </p>
      </header>

      <section
        aria-labelledby="filters-heading"
        className="rounded-lg border border-border bg-card p-5"
      >
        <h2 id="filters-heading" className="sr-only">
          Filter rooms
        </h2>

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Field
            label="Date"
            type="date"
            value={search.date ?? ''}
            min={toDateInputValue(new Date())}
            onChange={(e) =>
              setSearch(
                e.target.value
                  ? { date: e.target.value, ...defaultTimesFor(e.target.value, search) }
                  : { date: undefined, from: undefined, to: undefined },
              )
            }
          />
          <Field
            label="From"
            type="time"
            step={900}
            value={search.from ?? ''}
            disabled={!search.date}
            onChange={(e) => setSearch({ from: e.target.value || undefined })}
          />
          <Field
            label="Until"
            type="time"
            step={900}
            value={search.to ?? ''}
            disabled={!search.date}
            onChange={(e) => setSearch({ to: e.target.value || undefined })}
          />
          <Field
            label="Minimum seats"
            type="number"
            min={1}
            inputMode="numeric"
            value={search.capacity ?? ''}
            onChange={(e) =>
              setSearch({
                capacity: e.target.value ? Number(e.target.value) : undefined,
              })
            }
          />
        </div>

        <fieldset className="mt-4">
          <legend className="text-sm font-medium">Amenities</legend>
          <div className="mt-2 flex flex-wrap gap-2">
            {AMENITIES.map((amenity) => {
              const active = search.amenities?.includes(amenity) ?? false
              return (
                <button
                  key={amenity}
                  type="button"
                  // aria-pressed rather than a styled checkbox: these are
                  // toggle buttons, and the pressed state is what a screen
                  // reader needs to announce.
                  aria-pressed={active}
                  onClick={() => toggleAmenity(amenity)}
                  className={
                    active
                      ? 'rounded-full border border-primary bg-primary/10 px-3 py-1 text-sm text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
                      : 'rounded-full border border-border px-3 py-1 text-sm text-muted-fg hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
                  }
                >
                  {formatAmenity(amenity)}
                </button>
              )
            })}
          </div>
        </fieldset>

        {hasFilters ? (
          <div className="mt-4">
            <Button
              variant="ghost"
              size="sm"
              onClick={() =>
                void navigate({ search: {}, replace: true })
              }
            >
              Clear filters
            </Button>
          </div>
        ) : null}
      </section>

      <section aria-live="polite" aria-busy={rooms.isLoading}>
        {rooms.isLoading ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }, (_, i) => (
              <RoomCardSkeleton key={i} />
            ))}
          </div>
        ) : rooms.isError ? (
          <ErrorState
            title="Could not load rooms"
            description={errorMessage(rooms.error)}
            onRetry={() => void rooms.refetch()}
          />
        ) : rooms.data && rooms.data.length > 0 ? (
          <>
            <p className="sr-only">{rooms.data.length} rooms found.</p>
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {rooms.data.map((room) => (
                <RoomCard
                  key={room.id}
                  room={room}
                  href={{
                    to: '/rooms/$roomId',
                    params: { roomId: room.id },
                    search: { date: search.date },
                  }}
                />
              ))}
            </div>
          </>
        ) : (
          <EmptyState
            title="No rooms match those filters"
            description="Try fewer amenities, a smaller party, or a different time."
            action={
              hasFilters ? (
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => void navigate({ search: {}, replace: true })}
                >
                  Clear filters
                </Button>
              ) : undefined
            }
          />
        )}
      </section>
    </div>
  )
}

/**
 * windowFromSearch builds the API's time window from the URL's local fields.
 *
 * Returns undefined unless all three parts are present and the range is
 * positive. A half-filled window would otherwise be sent as a filter the user
 * did not finish specifying, and every room would come back marked unavailable
 * for a range that makes no sense.
 */
function windowFromSearch(search: {
  date?: string
  from?: string
  to?: string
}): { start: string; end: string } | undefined {
  if (!search.date || !search.from || !search.to) return undefined

  const day = fromDateInputValue(search.date)
  const start = applyTime(day, search.from)
  const end = applyTime(day, search.to)
  if (!start || !end || end <= start) return undefined

  return { start: toWire(start), end: toWire(end) }
}

function applyTime(day: Date, hhmm: string): Date | undefined {
  const [h, m] = hhmm.split(':').map(Number)
  if (!Number.isFinite(h) || !Number.isFinite(m)) return undefined
  return setTimeOfDay(day, h, m)
}

/**
 * defaultTimesFor fills in a sensible hour when a date is first chosen.
 *
 * Picking a date and being shown nothing until two more fields are filled is a
 * dead end. For today it offers the next slot boundary from now; for a future
 * day it offers 9am. Either way the user gets an answer immediately and can
 * adjust from there.
 */
function defaultTimesFor(
  dateValue: string,
  search: { from?: string; to?: string },
): { from: string; to: string } {
  if (search.from && search.to) {
    return { from: search.from, to: search.to }
  }

  const day = fromDateInputValue(dateValue)
  const now = new Date()
  const isToday = day.toDateString() === now.toDateString()

  const start = isToday ? roundUpToSlot(now) : setTimeOfDay(day, 9)
  const end = addMinutes(start, 60)

  return { from: toTimeInputValue(start), to: toTimeInputValue(end) }
}

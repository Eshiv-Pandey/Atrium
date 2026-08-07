import { useMemo, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { z } from 'zod'
import { errorMessage } from '@/api/client'
import { useAvailability } from '@/api/hooks'
import { BookingDialog } from '@/components/BookingDialog'
import { Timeline, TimelineLegend, type Selection } from '@/components/Timeline'
import { formatAmenity } from '@/components/RoomCard'
import { Badge, Card, ErrorState, Skeleton } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Field } from '@/components/ui/Field'
import { requireAuth } from '@/lib/guards'
import { buildSlots } from '@/lib/slots'
import {
  addDays,
  formatDuration,
  formatRange,
  fromDateInputValue,
  setTimeOfDay,
  toDateInputValue,
  toWire,
} from '@/lib/time'

/**
 * The bookable day, in local hours.
 *
 * Hard-coded rather than configurable: a co-working space has opening hours,
 * and inventing an admin setting for them would be scope the brief never asked
 * for. It is one constant to change, and the README records it as an
 * assumption rather than leaving a reviewer to infer it from the timeline.
 */
const DAY_START_HOUR = 8
const DAY_END_HOUR = 20

const searchSchema = z.object({
  date: z.string().optional().catch(undefined),
})

export const Route = createFileRoute('/rooms/$roomId')({
  validateSearch: searchSchema,
  beforeLoad: requireAuth,
  component: RoomDetail,
})

function RoomDetail() {
  const { roomId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const [selection, setSelection] = useState<Selection | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)

  const day = search.date ? fromDateInputValue(search.date) : new Date()
  const windowStart = setTimeOfDay(day, DAY_START_HOUR)
  const windowEnd = setTimeOfDay(day, DAY_END_HOUR)

  const availability = useAvailability(roomId, toWire(windowStart), toWire(windowEnd))

  // `now` is captured per render rather than read inside buildSlots so that
  // every slot in one render agrees on where the past ends. Reading the clock
  // per slot could put a boundary slot on both sides of "now" within a single
  // pass.
  const slots = useMemo(() => {
    if (!availability.data) return []
    return buildSlots(windowStart, windowEnd, availability.data.busy, new Date())
    // windowStart/windowEnd are new Date objects each render; their epoch
    // values are what matter, so those are the dependencies.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [availability.data, windowStart.getTime(), windowEnd.getTime()])

  const setDay = (value: string | undefined) => {
    setSelection(null)
    void navigate({ search: (prev) => ({ ...prev, date: value }), replace: true })
  }

  const room = availability.data?.room

  return (
    <div className="space-y-8">
      <div>
        <Link
          to="/rooms"
          className="text-sm text-muted-fg hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          ← All rooms
        </Link>
      </div>

      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {room ? room.name : <Skeleton className="h-8 w-48" />}
          </h1>
          {room ? (
            <>
              <p className="mt-1 text-sm text-muted-fg">Seats {room.capacity}</p>
              {room.amenities.length > 0 ? (
                <ul className="mt-3 flex flex-wrap gap-1.5">
                  {room.amenities.map((a) => (
                    <li key={a}>
                      <Badge>{formatAmenity(a)}</Badge>
                    </li>
                  ))}
                </ul>
              ) : null}
            </>
          ) : null}
        </div>

        <div className="flex items-end gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setDay(toDateInputValue(addDays(day, -1)))}
            aria-label="Previous day"
          >
            ←
          </Button>
          <Field
            label="Date"
            type="date"
            value={toDateInputValue(day)}
            min={toDateInputValue(new Date())}
            onChange={(e) => setDay(e.target.value || undefined)}
          />
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setDay(toDateInputValue(addDays(day, 1)))}
            aria-label="Next day"
          >
            →
          </Button>
        </div>
      </header>

      <Card className="p-5">
        <div className="mb-4 flex items-center justify-between gap-4">
          <h2 className="font-medium">Availability</h2>
          <TimelineLegend />
        </div>

        <div aria-live="polite" aria-busy={availability.isLoading}>
          {availability.isLoading ? (
            <div className="space-y-1">
              {Array.from({ length: 12 }, (_, i) => (
                <Skeleton key={i} className="h-9 w-full" />
              ))}
            </div>
          ) : availability.isError ? (
            <ErrorState
              title="Could not load availability"
              description={errorMessage(availability.error)}
              onRetry={() => void availability.refetch()}
            />
          ) : (
            <Timeline slots={slots} selection={selection} onSelect={setSelection} />
          )}
        </div>
      </Card>

      {/*
        The action bar is rendered only once something is selected, and it is
        sticky at the bottom. On a long timeline the selection is often made
        near the top and the button would otherwise sit below the fold — the
        classic "I selected it, now what" dead end.
      */}
      {selection && room ? (
        <div className="sticky bottom-4 flex flex-wrap items-center justify-between gap-4 rounded-lg border border-border bg-card p-4 shadow-lg">
          <div>
            <p className="text-sm font-medium">
              {formatRange(selection.start, selection.end)}
            </p>
            <p className="text-sm text-muted-fg">
              {formatDuration(selection.start, selection.end)}
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={() => setSelection(null)}>
              Clear
            </Button>
            <Button onClick={() => setDialogOpen(true)}>Book this slot</Button>
          </div>
        </div>
      ) : null}

      {room ? (
        <BookingDialog
          open={dialogOpen}
          room={room}
          selection={selection}
          onClose={() => setDialogOpen(false)}
          onBooked={() => setSelection(null)}
        />
      ) : null}
    </div>
  )
}

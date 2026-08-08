import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { errorMessage } from '@/api/client'
import { useAllBookings, useRooms } from '@/api/hooks'
import type { Booking } from '@/api/schemas'
import { Badge, Card, EmptyState, ErrorState, Skeleton } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Field } from '@/components/ui/Field'
import {
  formatDuration,
  formatRange,
  fromDateInputValue,
  setTimeOfDay,
  toWire,
} from '@/lib/time'

const searchSchema = z.object({
  roomId: z.string().uuid().optional().catch(undefined),
  status: z.enum(['confirmed', 'cancelled', 'released']).optional().catch(undefined),
  from: z.string().optional().catch(undefined),
  to: z.string().optional().catch(undefined),
})

export const Route = createFileRoute('/admin/bookings')({
  validateSearch: searchSchema,
  component: AdminBookings,
})

const filterLabel =
  'block text-[0.68rem] font-semibold uppercase tracking-[0.16em] text-muted-fg'
const filterSelect =
  'block w-full rounded-lg border border-input bg-background/60 px-3.5 py-2.5 text-sm transition-colors focus-visible:border-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60'

function AdminBookings() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const rooms = useRooms()

  const bookings = useAllBookings({
    roomId: search.roomId,
    status: search.status,
    // The date inputs are local calendar days; the API wants instants. A day
    // filter means "from local midnight to the following local midnight", not
    // "from 00:00 UTC" — those differ by hours for most of the world.
    from: search.from ? toWire(setTimeOfDay(fromDateInputValue(search.from), 0)) : undefined,
    to: search.to ? toWire(setTimeOfDay(fromDateInputValue(search.to), 24)) : undefined,
  })

  const setSearch = (patch: Partial<typeof search>) => {
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })
  }

  const hasFilters = Object.values(search).some((v) => v !== undefined)
  const items = bookings.data?.items ?? []

  return (
    <div className="space-y-6">
      <h2 className="font-display text-xl font-bold uppercase tracking-[-0.02em]">
        Bookings
      </h2>

      <section aria-labelledby="booking-filters" className="panel p-5 sm:p-6">
        <h2 id="booking-filters" className="sr-only">
          Filter bookings
        </h2>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {/*
            Hand-rolled rather than routed through Field, which renders an
            input. The label and control classes are copied from it deliberately
            so the two sit on the same baseline in the same grid row.
          */}
          <div className="space-y-2">
            <label htmlFor="filter-room" className={filterLabel}>
              Room
            </label>
            <select
              id="filter-room"
              value={search.roomId ?? ''}
              onChange={(e) => setSearch({ roomId: e.target.value || undefined })}
              className={filterSelect}
            >
              <option value="">All rooms</option>
              {rooms.data?.map((room) => (
                <option key={room.id} value={room.id}>
                  {room.name}
                </option>
              ))}
            </select>
          </div>

          <div className="space-y-2">
            <label htmlFor="filter-status" className={filterLabel}>
              Status
            </label>
            <select
              id="filter-status"
              value={search.status ?? ''}
              onChange={(e) =>
                setSearch({
                  status: (e.target.value || undefined) as typeof search.status,
                })
              }
              className={filterSelect}
            >
              <option value="">Any status</option>
              <option value="confirmed">Confirmed</option>
              <option value="cancelled">Cancelled</option>
              <option value="released">Released (no-show)</option>
            </select>
          </div>

          <Field
            label="From"
            type="date"
            value={search.from ?? ''}
            onChange={(e) => setSearch({ from: e.target.value || undefined })}
          />
          <Field
            label="To"
            type="date"
            value={search.to ?? ''}
            onChange={(e) => setSearch({ to: e.target.value || undefined })}
          />
        </div>

        {hasFilters ? (
          <div className="mt-6">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void navigate({ search: {}, replace: true })}
            >
              Clear filters
            </Button>
          </div>
        ) : null}
      </section>

      <div aria-live="polite" aria-busy={bookings.isLoading}>
        {bookings.isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        ) : bookings.isError ? (
          <ErrorState
            title="Could not load bookings"
            description={errorMessage(bookings.error)}
            onRetry={() => void bookings.refetch()}
          />
        ) : items.length === 0 ? (
          <EmptyState
            title="No bookings match those filters"
            description="Widen the date range or clear the room filter."
          />
        ) : (
          <>
            <Card className="overflow-x-auto">
              <table className="w-full text-sm">
                <caption className="sr-only">
                  All bookings matching the current filters.
                </caption>
                <thead className="border-b border-border/70 bg-muted/40 text-left [&_th]:text-[0.66rem] [&_th]:font-semibold [&_th]:uppercase [&_th]:tracking-[0.14em] [&_th]:text-muted-fg">
                  <tr>
                    <th scope="col" className="px-4 py-3">Room</th>
                    <th scope="col" className="px-4 py-3">Booked by</th>
                    <th scope="col" className="px-4 py-3">When</th>
                    <th scope="col" className="px-4 py-3">Seats</th>
                    <th scope="col" className="px-4 py-3">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((b) => (
                    <tr
                      key={b.id}
                      className="border-b border-border/60 transition-colors last:border-0 hover:bg-muted/30"
                    >
                      <th scope="row" className="px-4 py-3 text-left font-medium">
                        {b.room?.name ?? '—'}
                      </th>
                      <td className="px-4 py-3">
                        {b.user ? (
                          <>
                            <span className="block">{b.user.name}</span>
                            <span className="block text-xs text-muted-fg">
                              {b.user.email}
                            </span>
                          </>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="px-4 py-3" data-numeric>
                        <span className="block">{formatRange(b.startTime, b.endTime)}</span>
                        <span className="block text-xs text-muted-fg">
                          {formatDuration(b.startTime, b.endTime)}
                        </span>
                      </td>
                      <td className="px-4 py-3 tabular-nums" data-numeric>
                        {b.attendeeCount}
                      </td>
                      <td className="px-4 py-3">
                        <StatusBadge booking={b} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>

            {/*
              The cursor is surfaced rather than hidden. This page caps at the
              server's page size, and silently showing the first 50 of 300
              bookings while the filters imply completeness is the kind of
              quiet wrongness an admin screen cannot afford.
            */}
            {bookings.data?.nextCursor ? (
              <p className="mt-4 text-sm text-muted-fg">
                Showing the first {items.length}. Narrow the date range to see
                the rest.
              </p>
            ) : null}
          </>
        )}
      </div>
    </div>
  )
}

function StatusBadge({ booking }: { booking: Booking }) {
  if (booking.status === 'cancelled') return <Badge tone="danger">Cancelled</Badge>
  if (booking.status === 'released') return <Badge tone="warning">No-show</Badge>
  if (booking.checkedInAt) return <Badge tone="success">Checked in</Badge>
  return <Badge>Confirmed</Badge>
}

import { createFileRoute, Link } from '@tanstack/react-router'
import { errorMessage } from '@/api/client'
import { useCancelBooking, useCheckIn, useMyBookings } from '@/api/hooks'
import type { Booking } from '@/api/schemas'
import { notify } from '@/components/Toast'
import { Button } from '@/components/ui/Button'
import { Badge, Card, EmptyState, ErrorState, Skeleton } from '@/components/ui/Card'
import { requireAuth } from '@/lib/guards'
import { formatDuration, formatRange, fromWire } from '@/lib/time'

export const Route = createFileRoute('/bookings')({
  beforeLoad: requireAuth,
  component: MyBookings,
})

function MyBookings() {
  const bookings = useMyBookings()

  // Split rather than filtered to a tab. Upcoming is what you came for; past is
  // context you occasionally want. Two headings on one page means no state to
  // manage, no empty tab to land on, and the whole history is one Ctrl-F away.
  const now = Date.now()
  const items = bookings.data?.items ?? []
  const upcoming = items
    .filter((b) => fromWire(b.endTime).getTime() >= now)
    // The API returns newest-first, which is right for history and backwards
    // for a schedule: the next thing you have to attend belongs at the top.
    .sort((a, b) => fromWire(a.startTime).getTime() - fromWire(b.startTime).getTime())
  const past = items.filter((b) => fromWire(b.endTime).getTime() < now)

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">My bookings</h1>
        <p className="mt-1 text-sm text-muted-fg">
          Check in from five minutes before your booking starts. Fifteen minutes
          after, an unclaimed room is released for someone else.
        </p>
      </header>

      <div aria-live="polite" aria-busy={bookings.isLoading}>
        {bookings.isLoading ? (
          <div className="space-y-3">
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-28 w-full" />
            ))}
          </div>
        ) : bookings.isError ? (
          <ErrorState
            title="Could not load your bookings"
            description={errorMessage(bookings.error)}
            onRetry={() => void bookings.refetch()}
          />
        ) : items.length === 0 ? (
          <EmptyState
            title="No bookings yet"
            description="Find a room and reserve a slot — it takes about ten seconds."
            action={
              // A link styled as a button, not a Button wrapping a link:
              // navigation is an anchor's job, and nesting one inside a button
              // is invalid HTML that breaks middle-click and "open in new tab".
              <Link
                to="/rooms"
                className="inline-flex h-10 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
              >
                Browse rooms
              </Link>
            }
          />
        ) : (
          <div className="space-y-10">
            <section aria-labelledby="upcoming-heading">
              <h2 id="upcoming-heading" className="mb-3 text-sm font-medium text-muted-fg">
                Upcoming
              </h2>
              {upcoming.length === 0 ? (
                <EmptyState
                  title="Nothing coming up"
                  description="Your past bookings are below."
                />
              ) : (
                <ul className="space-y-3">
                  {upcoming.map((b) => (
                    <li key={b.id}>
                      <BookingRow booking={b} />
                    </li>
                  ))}
                </ul>
              )}
            </section>

            {past.length > 0 ? (
              <section aria-labelledby="past-heading">
                <h2 id="past-heading" className="mb-3 text-sm font-medium text-muted-fg">
                  Past
                </h2>
                <ul className="space-y-3">
                  {past.map((b) => (
                    <li key={b.id}>
                      <BookingRow booking={b} past />
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}
          </div>
        )}
      </div>
    </div>
  )
}

function BookingRow({ booking, past }: { booking: Booking; past?: boolean }) {
  const checkIn = useCheckIn()
  const cancel = useCancelBooking()

  const busy = checkIn.isPending || cancel.isPending

  return (
    <Card className={past ? 'p-5 opacity-70' : 'p-5'}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-medium">{booking.room?.name ?? 'Room'}</h3>
            <StatusBadge booking={booking} />
          </div>
          <p className="mt-1 text-sm text-muted-fg">
            {formatRange(booking.startTime, booking.endTime)} ·{' '}
            {formatDuration(booking.startTime, booking.endTime)}
          </p>
          <p className="mt-0.5 text-sm text-muted-fg">
            {booking.attendeeCount}{' '}
            {booking.attendeeCount === 1 ? 'attendee' : 'attendees'}
          </p>
        </div>

        {/*
          Both actions are driven by server-computed flags rather than by
          re-deriving the time windows here. The client's clock can be wrong,
          and a check-in button that is enabled when the server will reject it
          is worse than no button at all.
        */}
        <div className="flex shrink-0 gap-2">
          {booking.canCheckInNow ? (
            <Button
              size="sm"
              loading={checkIn.isPending}
              disabled={busy}
              onClick={() =>
                checkIn.mutate(booking.id, {
                  onSuccess: () => notify.success('Checked in.'),
                  onError: (err) => notify.error(errorMessage(err)),
                })
              }
            >
              Check in
            </Button>
          ) : null}

          {booking.canCancel ? (
            <Button
              size="sm"
              variant="secondary"
              loading={cancel.isPending}
              disabled={busy}
              onClick={() => {
                // A native confirm rather than a second dialog component.
                // Cancelling is reversible only by rebooking, and rebooking can
                // fail if someone takes the slot in between — so it earns a
                // confirmation, but not a bespoke one.
                if (!window.confirm('Cancel this booking? The room will be released.')) {
                  return
                }
                cancel.mutate(booking.id, {
                  onSuccess: () => notify.success('Booking cancelled.'),
                  onError: (err) => notify.error(errorMessage(err)),
                })
              }}
            >
              Cancel
            </Button>
          ) : null}
        </div>
      </div>
    </Card>
  )
}

function StatusBadge({ booking }: { booking: Booking }) {
  if (booking.status === 'cancelled') return <Badge tone="danger">Cancelled</Badge>
  if (booking.status === 'released') {
    return (
      <Badge tone="warning" title="Nobody checked in, so the room was released.">
        Released
      </Badge>
    )
  }
  if (booking.checkedInAt) return <Badge tone="success">Checked in</Badge>
  return <Badge>Confirmed</Badge>
}

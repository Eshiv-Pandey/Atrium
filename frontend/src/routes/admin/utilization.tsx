import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { errorMessage } from '@/api/client'
import { useUtilization } from '@/api/hooks'
import { Card, EmptyState, ErrorState, Skeleton } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import {
  addDays,
  formatDate,
  startOfDay,
  toDateInputValue,
  toWire,
} from '@/lib/time'

const searchSchema = z.object({
  /** Local calendar day the week starts on. Defaults to the current Monday. */
  week: z.string().optional().catch(undefined),
})

export const Route = createFileRoute('/admin/utilization')({
  validateSearch: searchSchema,
  component: AdminUtilization,
})

function AdminUtilization() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const weekStart = search.week
    ? startOfDay(new Date(`${search.week}T00:00:00`))
    : startOfMonday(new Date())
  const weekEnd = addDays(weekStart, 7)

  const utilization = useUtilization(toWire(weekStart), toWire(weekEnd))

  const shiftWeek = (days: number) => {
    void navigate({
      search: { week: toDateInputValue(addDays(weekStart, days)) },
      replace: true,
    })
  }

  const rows = utilization.data ?? []

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="font-display text-xl font-bold uppercase tracking-[-0.02em]">
            Utilization
          </h2>
          <p className="text-sm text-muted-fg" data-numeric>
            {formatDate(weekStart)} – {formatDate(addDays(weekEnd, -1))}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" onClick={() => shiftWeek(-7)}>
            ← Previous week
          </Button>
          <Button variant="secondary" size="sm" onClick={() => shiftWeek(7)}>
            Next week →
          </Button>
        </div>
      </div>

      <div aria-live="polite" aria-busy={utilization.isLoading}>
        {utilization.isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 5 }, (_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        ) : utilization.isError ? (
          <ErrorState
            title="Could not load utilization"
            description={errorMessage(utilization.error)}
            onRetry={() => void utilization.refetch()}
          />
        ) : rows.length === 0 ? (
          <EmptyState
            title="Nothing to report"
            description="No rooms were bookable during this week."
          />
        ) : (
          <Card className="overflow-x-auto">
            <table className="w-full text-sm">
              <caption className="sr-only">
                Booked hours per room for the week of {formatDate(weekStart)}.
              </caption>
              <thead className="border-b border-border/70 bg-muted/40 text-left [&_th]:text-[0.66rem] [&_th]:font-semibold [&_th]:uppercase [&_th]:tracking-[0.14em] [&_th]:text-muted-fg">
                <tr>
                  <th scope="col" className="px-4 py-3">Room</th>
                  <th scope="col" className="w-1/3 px-4 py-3">Utilization</th>
                  <th scope="col" className="px-4 py-3 text-right">Booked</th>
                  <th scope="col" className="px-4 py-3 text-right">Bookings</th>
                  <th scope="col" className="px-4 py-3 text-right">No-shows</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const pct = Math.round(row.utilizationRate * 100)
                  return (
                    <tr
                      key={row.roomId}
                      className="border-b border-border/60 transition-colors last:border-0 hover:bg-muted/30"
                    >
                      <th scope="row" className="px-4 py-3 text-left font-medium">
                        {row.roomName}
                        <span className="block text-xs font-normal text-muted-fg">
                          Seats {row.capacity}
                        </span>
                      </th>
                      <td className="px-4 py-3">
                        {/*
                          A div bar rather than a charting library. One
                          dependency avoided for a horizontal rectangle, and
                          the number is written next to it — a bar alone is
                          unreadable to a screen reader, and a bar with a
                          number beside it needs no ARIA at all.
                        */}
                        <div className="flex items-center gap-3">
                          <div
                            className="h-2.5 flex-1 overflow-hidden rounded-full bg-muted/70"
                            aria-hidden="true"
                          >
                            <div
                              className="h-full rounded-full bg-primary shadow-[0_0_12px_-2px_oklch(var(--primary)/0.8)]"
                              style={{ width: `${Math.min(pct, 100)}%` }}
                            />
                          </div>
                          <span className="w-11 shrink-0 text-right font-mono text-xs tabular-nums">
                            {pct}%
                          </span>
                        </div>
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums">
                        {row.bookedHours.toFixed(1)}h
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums">
                        {row.bookingCount}
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums">
                        {row.noShowCount > 0 ? (
                          <span className="text-destructive">{row.noShowCount}</span>
                        ) : (
                          row.noShowCount
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </Card>
        )}
      </div>

      <p className="max-w-3xl text-sm leading-relaxed text-muted-fg">
        Utilization is booked hours over bookable hours, computed server-side so
        every view reports the same number. Released no-shows count as bookings
        but contribute no booked hours — the room was in fact empty.
      </p>
    </div>
  )
}

/**
 * startOfMonday returns the Monday of the week containing `date`.
 *
 * Weeks start on Monday because a working week does. Sunday-start would put
 * the weekend at both ends of the row and split every ordinary week across two
 * pages of this report.
 */
function startOfMonday(date: Date): Date {
  const d = startOfDay(date)
  // getDay is 0 for Sunday, so Sunday needs to go back six days rather than
  // forward one.
  const offset = (d.getDay() + 6) % 7
  return addDays(d, -offset)
}

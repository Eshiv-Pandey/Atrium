import { Link } from '@tanstack/react-router'
import type { Room } from '@/api/schemas'
import { Badge, Card } from '@/components/ui/Card'
import { cn } from '@/lib/utils'

/**
 * RoomCard is the unit of the browse grid.
 *
 * `available` is a tri-state and is treated as one: true, false, or absent
 * when the caller did not supply a time window. Collapsing absent into false
 * would mark the entire catalog unavailable on first load, before anyone has
 * picked a time.
 */
export function RoomCard({
  room,
  href,
}: {
  room: Room
  href: { to: string; params?: Record<string, string>; search?: Record<string, unknown> }
}) {
  const unavailable = room.available === false

  return (
    <Card
      className={cn(
        'group relative transition-colors',
        unavailable ? 'opacity-60' : 'hover:border-primary/50',
      )}
    >
      <div className="p-5">
        <div className="flex items-start justify-between gap-3">
          <h3 className="font-medium">
            {/*
              The link covers the whole card via an ::after overlay rather than
              wrapping it. Wrapping would put the amenity list inside the
              anchor, so a screen reader would read the entire card contents as
              one long link name.
            */}
            <Link
              {...href}
              className="after:absolute after:inset-0 after:content-[''] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              {room.name}
            </Link>
          </h3>

          {room.available === true ? (
            <Badge tone="success">Free</Badge>
          ) : room.available === false ? (
            <Badge tone="danger">Booked</Badge>
          ) : null}
        </div>

        <p className="mt-1 text-sm text-muted-fg">
          Seats {room.capacity}
        </p>

        {room.amenities.length > 0 ? (
          <ul className="mt-3 flex flex-wrap gap-1.5">
            {room.amenities.map((a) => (
              <li key={a}>
                <Badge>{formatAmenity(a)}</Badge>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </Card>
  )
}

/**
 * Amenities are stored lowercase so Postgres's `@>` containment operator can
 * match them exactly against an index. That is a storage decision, not a
 * display one, so the presentation layer capitalises them back.
 */
/* eslint-disable-next-line react-refresh/only-export-components --
 * A formatter for this card's own data. Moving it to lib/ to satisfy Fast
 * Refresh would separate it from the only place it is used.
 */
export function formatAmenity(amenity: string): string {
  const special: Record<string, string> = {
    tv: 'TV',
    videoconf: 'Video conf',
  }
  return special[amenity] ?? amenity.charAt(0).toUpperCase() + amenity.slice(1)
}

export function RoomCardSkeleton() {
  return (
    <Card className="p-5">
      <div className="h-5 w-2/3 animate-pulse rounded bg-muted" />
      <div className="mt-2 h-4 w-1/3 animate-pulse rounded bg-muted" />
      <div className="mt-4 flex gap-1.5">
        <div className="h-5 w-16 animate-pulse rounded-full bg-muted" />
        <div className="h-5 w-20 animate-pulse rounded-full bg-muted" />
      </div>
    </Card>
  )
}

import { formatAmenity } from '@/components/RoomCard'
import { cn } from '@/lib/utils'

/**
 * The amenities a room can carry. Mirrors the values the API stores.
 *
 * Not exported: this component is the only thing that renders the list, and an
 * exported array from a file that also exports a component breaks Fast Refresh
 * for the whole module.
 */
const AMENITIES = ['quiet', 'whiteboard', 'tv', 'videoconf', 'casual']

/**
 * A row of amenity toggles.
 *
 * Shared between the browse filters and the admin room form because the two
 * were identical markup in two files — including the list of amenities, which
 * would have silently diverged the first time one gained a value.
 *
 * `aria-pressed` rather than a styled checkbox: these are toggle buttons, and
 * the pressed state is what a screen reader needs to announce.
 */
export function AmenityToggles({
  selected,
  onToggle,
  legend = 'Amenities',
  className,
}: {
  selected: string[]
  onToggle: (amenity: string) => void
  legend?: string
  className?: string
}) {
  return (
    <fieldset className={className}>
      <legend className="micro">{legend}</legend>
      <div className="mt-3 flex flex-wrap gap-2">
        {AMENITIES.map((amenity) => {
          const active = selected.includes(amenity)
          return (
            <button
              key={amenity}
              type="button"
              aria-pressed={active}
              onClick={() => onToggle(amenity)}
              className={cn(
                'rounded-full border px-3.5 py-1.5 text-sm font-medium transition-colors',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                active
                  ? 'border-primary bg-primary/15 text-primary'
                  : 'border-border/70 text-muted-fg hover:border-foreground/30 hover:bg-muted hover:text-foreground',
              )}
            >
              {formatAmenity(amenity)}
            </button>
          )
        })}
      </div>
    </fieldset>
  )
}

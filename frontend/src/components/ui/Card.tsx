import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * Card is the workhorse surface.
 *
 * Translucent with a blur rather than a flat fill: the ember wash and film
 * grain sit behind the whole document, and an opaque panel punches a hole in
 * both — the card reads as pasted on top of the page instead of resting on it.
 */
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'rounded-xl border border-border/70 bg-card/70 text-card-fg backdrop-blur-sm',
        className,
      )}
      {...props}
    />
  )
}

type BadgeTone = 'default' | 'success' | 'warning' | 'danger'

/*
  Tones are tinted fills with a matching border, not bare coloured text. On a
  grainy background a text-only badge loses its edge and stops reading as a
  chip; the 30%-alpha ring gives it one without a second solid surface.
*/
const badgeTones: Record<BadgeTone, string> = {
  default: 'border-border/70 bg-muted/70 text-muted-fg',
  success: 'border-success/30 bg-success/10 text-success',
  warning: 'border-warning/30 bg-warning/10 text-warning',
  danger: 'border-destructive/30 bg-destructive/10 text-destructive',
}

export function Badge({
  tone = 'default',
  className,
  ...props
}: HTMLAttributes<HTMLSpanElement> & { tone?: BadgeTone }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border px-2.5 py-0.5',
        'text-[0.7rem] font-semibold uppercase tracking-[0.08em]',
        badgeTones[tone],
        className,
      )}
      {...props}
    />
  )
}

/**
 * Skeleton stands in for content while it loads.
 *
 * Sized by the caller to match what will replace it, so the page does not
 * reflow when data arrives — a spinner in the middle of an empty page is
 * followed by every element jumping into place, which reads as slower than it
 * is even when the request time is identical.
 */
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      // Hidden from assistive tech: a screen reader user gets the loading
      // state from the live region, not from a description of grey boxes.
      aria-hidden="true"
      className={cn('animate-pulse rounded-md bg-muted/70', className)}
      {...props}
    />
  )
}

/**
 * SectionHeading is the page-level title treatment.
 *
 * Every route had its own hand-rolled `<h1>` with slightly different size and
 * tracking, which is how a display face stops looking deliberate. One component
 * means the eyebrow, headline and description keep the same relationship
 * everywhere.
 */
export function SectionHeading({
  eyebrow,
  title,
  description,
  actions,
  className,
}: {
  eyebrow?: string
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between',
        className,
      )}
    >
      <div className="min-w-0">
        {eyebrow ? <p className="micro">{eyebrow}</p> : null}
        <h1 className="display-xl mt-2 text-display-sm sm:text-display-md">{title}</h1>
        {description ? (
          <p className="mt-3 max-w-2xl text-pretty leading-relaxed text-muted-fg">
            {description}
          </p>
        ) : null}
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
    </div>
  )
}

/**
 * EmptyState explains an empty list and offers the next step.
 *
 * Every list in this app renders one rather than nothing. A blank panel is
 * ambiguous — it reads identically whether the filter excluded everything, the
 * request failed silently, or there is genuinely nothing yet — and the fix
 * differs in each case.
 */
export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-border/70 bg-card/40 px-6 py-14 text-center backdrop-blur-sm">
      <p className="font-display text-lg font-bold uppercase tracking-[-0.01em] text-foreground">
        {title}
      </p>
      {description ? (
        <p className="mt-2 max-w-sm text-sm leading-relaxed text-muted-fg">{description}</p>
      ) : null}
      {action ? <div className="mt-5">{action}</div> : null}
    </div>
  )
}

/**
 * ErrorState reports a failed load and offers a retry.
 *
 * Separate from EmptyState because the two must not look alike: "no rooms
 * match your filter" and "we could not reach the server" call for different
 * actions, and collapsing them into one grey panel leaves the user adjusting
 * filters against an outage.
 */
export function ErrorState({
  title = 'Something went wrong',
  description,
  onRetry,
}: {
  title?: string
  description?: string
  onRetry?: () => void
}) {
  return (
    <div
      role="alert"
      className="flex flex-col items-center justify-center rounded-xl border border-destructive/40 bg-destructive/[0.06] px-6 py-14 text-center backdrop-blur-sm"
    >
      <p className="font-display text-lg font-bold uppercase tracking-[-0.01em] text-destructive">
        {title}
      </p>
      {description ? (
        <p className="mt-2 max-w-sm text-sm leading-relaxed text-muted-fg">{description}</p>
      ) : null}
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="mt-5 rounded-full border border-border/70 px-5 py-2 text-sm font-medium transition-colors hover:border-foreground/40 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          Try again
        </button>
      ) : null}
    </div>
  )
}

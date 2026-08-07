import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'rounded-lg border border-border bg-card text-card-fg',
        className,
      )}
      {...props}
    />
  )
}

type BadgeTone = 'default' | 'success' | 'warning' | 'danger'

const badgeTones: Record<BadgeTone, string> = {
  default: 'bg-muted text-muted-fg',
  success: 'bg-primary/10 text-primary',
  warning: 'bg-muted text-foreground',
  danger: 'bg-destructive/10 text-destructive',
}

export function Badge({
  tone = 'default',
  className,
  ...props
}: HTMLAttributes<HTMLSpanElement> & { tone?: BadgeTone }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium',
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
      className={cn('animate-pulse rounded-md bg-muted', className)}
      {...props}
    />
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
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-border px-6 py-12 text-center">
      <p className="font-medium text-foreground">{title}</p>
      {description ? (
        <p className="mt-1 max-w-sm text-sm text-muted-fg">{description}</p>
      ) : null}
      {action ? <div className="mt-4">{action}</div> : null}
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
      className="flex flex-col items-center justify-center rounded-lg border border-destructive/30 px-6 py-12 text-center"
    >
      <p className="font-medium text-destructive">{title}</p>
      {description ? (
        <p className="mt-1 max-w-sm text-sm text-muted-fg">{description}</p>
      ) : null}
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="mt-4 rounded-md border border-border px-4 py-2 text-sm font-medium hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          Try again
        </button>
      ) : null}
    </div>
  )
}

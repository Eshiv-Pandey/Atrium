import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/**
 * cn merges Tailwind classes, resolving conflicts by last-wins.
 *
 * clsx handles the conditionals; twMerge deduplicates, so a component's
 * default `px-4` and a caller's `px-8` produce `px-8` rather than both classes
 * fighting in the cascade. Without it, whether an override works depends on
 * stylesheet order — which is not something a caller can see.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * newIdempotencyKey generates a key for one booking attempt.
 *
 * Called once when a booking form opens, not on each submit. That is the whole
 * point: a double-clicked button sends the same key twice, and the server
 * returns the booking it already made instead of making a second one.
 *
 * crypto.randomUUID is available in every browser this app supports and in
 * jsdom under Node 19+; the timestamp fallback exists only so a test
 * environment without it does not fail on an unrelated import.
 */
export function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2, 11)}`
}

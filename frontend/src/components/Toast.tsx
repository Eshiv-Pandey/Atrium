/* eslint-disable react-refresh/only-export-components --
 * The store and the viewport that reads it are one unit on purpose (see below),
 * and the store is module-level mutable state. Splitting it into its own file to
 * satisfy Fast Refresh would not even work: editing this file would still reset
 * `toasts` and `nextId`. The cost is a full reload when this one file is edited
 * during development, which is the correct trade for keeping the state and its
 * only consumer legible together.
 */
import { useSyncExternalStore } from 'react'
import { cn } from '@/lib/utils'

/**
 * A minimal toast store, deliberately not a dependency.
 *
 * Toasts are the one piece of genuinely global UI state here — a mutation in a
 * dialog needs to raise a message that outlives the dialog. That is about
 * eighty lines of subscription bookkeeping, which is less than the API surface
 * of a toast library and leaves nothing to keep up to date.
 *
 * `useSyncExternalStore` rather than context: a context provider re-renders its
 * whole subtree on every toast, and the subtree here is the entire app.
 */

export type ToastTone = 'success' | 'error' | 'info'

export type Toast = {
  id: number
  tone: ToastTone
  title: string
  description?: string
}

const DISMISS_AFTER_MS = 6000

let toasts: Toast[] = []
let nextId = 1
const listeners = new Set<() => void>()

function emit() {
  // A new array identity each time, because useSyncExternalStore compares the
  // snapshot with Object.is and would not re-render on a mutated array.
  toasts = [...toasts]
  listeners.forEach((l) => l())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function getSnapshot() {
  return toasts
}

export function dismissToast(id: number) {
  toasts = toasts.filter((t) => t.id !== id)
  emit()
}

export function toast(tone: ToastTone, title: string, description?: string) {
  const id = nextId++
  toasts = [...toasts, { id, tone, title, description }]
  emit()

  // Errors stay until dismissed. An error that vanishes after six seconds is
  // one the user may never have finished reading, and it usually carries the
  // reason a booking failed.
  if (tone !== 'error') {
    setTimeout(() => dismissToast(id), DISMISS_AFTER_MS)
  }

  return id
}

export const notify = {
  success: (title: string, description?: string) =>
    toast('success', title, description),
  error: (title: string, description?: string) =>
    toast('error', title, description),
  info: (title: string, description?: string) => toast('info', title, description),
}

/** Test-only: drop every toast so cases do not leak into one another. */
export function resetToasts() {
  toasts = []
  nextId = 1
  emit()
}

const toneStyles: Record<ToastTone, string> = {
  success: 'border-success/40 bg-card/95',
  error: 'border-destructive/50 bg-card/95',
  info: 'border-border/70 bg-card/95',
}

const toneLabel: Record<ToastTone, string> = {
  success: 'Success',
  error: 'Error',
  info: 'Note',
}

export function ToastViewport() {
  const items = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  return (
    // role="status" with aria-live="polite" means a screen reader announces
    // each toast without interrupting what the user is currently doing. The
    // container is always mounted — a live region added to the DOM at the same
    // moment as its content is frequently not announced at all.
    <div
      role="status"
      aria-live="polite"
      aria-relevant="additions text"
      className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 p-4 sm:items-end"
    >
      {items.map((t) => (
        <div
          key={t.id}
          className={cn(
            'pointer-events-auto w-full max-w-sm rounded-xl border p-4 backdrop-blur-xl',
            'shadow-[0_24px_60px_-24px_oklch(0_0_0/0.9)]',
            toneStyles[t.tone],
          )}
        >
          <div className="flex items-start gap-3">
            <div className="min-w-0 flex-1">
              <p
                className={cn(
                  'text-sm font-semibold',
                  t.tone === 'error' ? 'text-destructive' : 'text-card-fg',
                )}
              >
                <span className="sr-only">{toneLabel[t.tone]}: </span>
                {t.title}
              </p>
              {t.description ? (
                <p className="mt-1 break-words text-sm text-muted-fg">
                  {t.description}
                </p>
              ) : null}
            </div>
            <button
              type="button"
              onClick={() => dismissToast(t.id)}
              className="-m-1 rounded p-1 text-muted-fg hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label={`Dismiss: ${t.title}`}
            >
              <svg
                viewBox="0 0 20 20"
                fill="currentColor"
                className="h-4 w-4"
                aria-hidden="true"
              >
                <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
              </svg>
            </button>
          </div>
        </div>
      ))}
    </div>
  )
}

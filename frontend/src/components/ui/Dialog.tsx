import { useEffect, useRef, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * Dialog wraps the platform's own modal element.
 *
 * `<dialog>.showModal()` is used instead of a hand-rolled overlay because the
 * browser already does the three things that are easy to get wrong: it renders
 * in the top layer, so no ancestor's `z-index` or `overflow: hidden` can clip
 * it; it makes the rest of the document inert, so a screen reader cannot walk
 * out of the dialog into the page behind it; and it traps Tab and handles
 * Escape. A `div` with a focus-trap effect reimplements all of that, and the
 * reimplementation is where the bugs live.
 *
 * What is left to do here is the React half: keeping the imperative
 * open/close API in step with a declarative prop, and reporting closes that
 * the browser initiated so the parent's state does not drift out of sync.
 */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
}: {
  open: boolean
  onClose: () => void
  title: string
  /** Optional supporting line, wired to `aria-describedby`. */
  description?: string
  children: ReactNode
  footer?: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const restoreTo = useRef<HTMLElement | null>(null)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return

    if (open && !dialog.open) {
      // Captured before showModal moves focus. Browsers do restore focus on
      // close, but only when the trigger is still in the document — and the
      // trigger here is often a slot button that a refetch can replace. Doing
      // it explicitly means focus lands somewhere sensible either way.
      restoreTo.current = document.activeElement as HTMLElement | null
      dialog.showModal()
    } else if (!open && dialog.open) {
      dialog.close()
    }
  }, [open])

  useEffect(() => {
    if (open) return
    const target = restoreTo.current
    // isConnected guards the case above: the element that opened the dialog may
    // have been unmounted while it was open, and focusing a detached node
    // silently drops focus to <body>.
    if (target?.isConnected) target.focus()
  }, [open])

  return (
    <dialog
      ref={ref}
      aria-labelledby="dialog-title"
      aria-describedby={description ? 'dialog-description' : undefined}
      // The `cancel` event fires for Escape; `close` covers that and every
      // programmatic close. Reporting it upward is what stops the parent from
      // believing the dialog is still open after Escape.
      onClose={onClose}
      onClick={(e) => {
        // A modal dialog's hit area includes its backdrop, so a click that
        // lands on the element itself rather than on its contents is a
        // backdrop click. Comparing against the panel's bounds would misfire
        // on the padding inside it.
        if (e.target === ref.current) onClose()
      }}
      className={cn(
        'w-[min(32rem,calc(100vw-2rem))] rounded-2xl border border-border/70 bg-card p-0 text-card-fg',
        'shadow-[0_40px_100px_-32px_oklch(0_0_0/0.95)]',
        'backdrop:bg-black/60 backdrop:backdrop-blur-sm',
      )}
    >
      <div className="border-b border-border/70 px-6 py-5">
        <h2
          id="dialog-title"
          className="font-display text-xl font-bold uppercase tracking-[-0.02em]"
        >
          {title}
        </h2>
        {description ? (
          <p id="dialog-description" className="mt-2 text-sm leading-relaxed text-muted-fg">
            {description}
          </p>
        ) : null}
      </div>

      <div className="px-6 py-6">{children}</div>

      {footer ? (
        <div className="flex justify-end gap-2 border-t border-border/70 px-6 py-4">
          {footer}
        </div>
      ) : null}
    </dialog>
  )
}

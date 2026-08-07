import { forwardRef, useId, type InputHTMLAttributes, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

export type FieldProps = InputHTMLAttributes<HTMLInputElement> & {
  label: string
  /** Server- or client-side message for this field. Presence marks it invalid. */
  error?: string
  /** Persistent guidance shown under the input when there is no error. */
  hint?: ReactNode
}

/**
 * Field pairs a label, an input, and its error message.
 *
 * Bundling the three is what makes the wiring reliable. The generated id ties
 * the label to the input, and `aria-describedby` ties the error to it, so a
 * screen reader reads "Email, invalid entry, that does not look like an email
 * address" instead of announcing a field and leaving the message stranded
 * somewhere else in the DOM. Hand-wiring this per form is where it gets
 * forgotten.
 */
export const Field = forwardRef<HTMLInputElement, FieldProps>(
  ({ label, error, hint, className, id: providedId, ...props }, ref) => {
    const generatedId = useId()
    const id = providedId ?? generatedId
    const errorId = `${id}-error`
    const hintId = `${id}-hint`

    return (
      <div className="space-y-1.5">
        <label htmlFor={id} className="block text-sm font-medium text-foreground">
          {label}
        </label>

        <input
          ref={ref}
          id={id}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : hint ? hintId : undefined}
          className={cn(
            'block w-full rounded-md border bg-background px-3 py-2 text-sm text-foreground',
            'placeholder:text-muted-fg',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            'disabled:cursor-not-allowed disabled:opacity-50',
            error ? 'border-destructive' : 'border-input',
            className,
          )}
          {...props}
        />

        {error ? (
          // The message renders in the DOM only when it exists, so the live
          // region announces it as an addition rather than as a change to an
          // empty node some browsers skip.
          <p id={errorId} className="text-sm text-destructive">
            {error}
          </p>
        ) : hint ? (
          <p id={hintId} className="text-sm text-muted-fg">
            {hint}
          </p>
        ) : null}
      </div>
    )
  },
)
Field.displayName = 'Field'

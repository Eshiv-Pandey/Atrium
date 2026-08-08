import { forwardRef, type ButtonHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

type Variant = 'primary' | 'secondary' | 'ghost' | 'destructive'
type Size = 'sm' | 'md' | 'lg'

/*
  Only `primary` carries a solid fill. The refs get their energy from one hot
  element against a lot of restraint, and a page where the secondary action is
  also a filled button has no primary action — it has two of equal weight.
*/
const variants: Record<Variant, string> = {
  primary: 'bg-primary text-primary-fg hover:brightness-110',
  secondary:
    'border border-border/70 bg-card/60 text-card-fg backdrop-blur-sm hover:border-foreground/30 hover:bg-muted',
  ghost: 'text-muted-fg hover:bg-muted hover:text-foreground',
  destructive: 'bg-destructive text-destructive-fg hover:brightness-110',
}

const sizes: Record<Size, string> = {
  sm: 'h-8 px-4 text-sm',
  md: 'h-10 px-5 text-sm',
  lg: 'h-12 px-7 text-base',
}

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: Variant
  size?: Size
  /** Disables the button and swaps the label for a spinner. */
  loading?: boolean
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { className, variant = 'primary', size = 'md', loading, children, disabled, type, ...props },
    ref,
  ) => (
    <button
      ref={ref}
      // Buttons inside a form default to type="submit", which turns any
      // unlabelled action button into an accidental form submission. Defaulting
      // to "button" means submitting is always something you asked for.
      type={type ?? 'button'}
      disabled={disabled || loading}
      // aria-busy tells a screen reader the control is working. Without it the
      // only signal is a spinner, which is purely visual.
      aria-busy={loading || undefined}
      className={cn(
        'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-full font-semibold transition-[filter,background-color,border-color,color]',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
        'disabled:pointer-events-none disabled:opacity-50',
        variants[variant],
        sizes[size],
        className,
      )}
      {...props}
    >
      {loading ? <Spinner className="h-4 w-4" /> : null}
      {children}
    </button>
  ),
)
Button.displayName = 'Button'

export function Spinner({ className }: { className?: string }) {
  return (
    <svg
      className={cn('animate-spin', className)}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle
        className="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        strokeWidth="4"
      />
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 0 1 8-8V0C5.373 0 0 5.373 0 12h4z"
      />
    </svg>
  )
}

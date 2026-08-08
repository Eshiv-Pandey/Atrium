import { cn } from '@/lib/utils'

/**
 * The Atrium mark.
 *
 * The source PNG has its dark background baked in, so it cannot sit on a light
 * surface without showing a black tile. It is placed on a deliberately dark
 * chip here — which is also the boxed-outline lockup from the reference art —
 * so the mark reads identically in both themes rather than only in dark.
 *
 * `alt=""` because every call site pairs the mark with the word "Atrium" in
 * text. Naming the image too would make a screen reader announce the brand
 * twice on a single link.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md',
        'bg-[oklch(0.13_0.018_45)] ring-1 ring-inset ring-foreground/15',
        'h-9 w-9',
        className,
      )}
    >
      <img
        src="/images/atrium-logo.png"
        alt=""
        // The glyph carries its own padding inside the PNG, so it is scaled
        // past the chip and cropped rather than sitting in a double margin.
        className="h-[150%] w-[150%] max-w-none object-contain"
        width={72}
        height={72}
        decoding="async"
      />
    </span>
  )
}

/**
 * Wordmark: the mark plus the name, tracked and weighted to match the display
 * face. Used in the nav and the footer so the lockup cannot drift between them.
 */
export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2.5', className)}>
      <Logo />
      <span className="font-display text-lg font-extrabold uppercase tracking-[-0.02em]">
        Atrium
      </span>
    </span>
  )
}

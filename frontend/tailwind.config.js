/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './index.html',
    './src/**/*.{js,ts,jsx,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        // OKLCH tokens so light and dark themes share one perceptual ramp.
        // These map to CSS custom properties defined in index.css.
        primary: 'oklch(var(--primary) / <alpha-value>)',
        'primary-fg': 'oklch(var(--primary-fg) / <alpha-value>)',
        background: 'oklch(var(--background) / <alpha-value>)',
        foreground: 'oklch(var(--foreground) / <alpha-value>)',
        card: 'oklch(var(--card) / <alpha-value>)',
        'card-fg': 'oklch(var(--card-fg) / <alpha-value>)',
        muted: 'oklch(var(--muted) / <alpha-value>)',
        'muted-fg': 'oklch(var(--muted-fg) / <alpha-value>)',
        border: 'oklch(var(--border) / <alpha-value>)',
        input: 'oklch(var(--input) / <alpha-value>)',
        ring: 'oklch(var(--ring) / <alpha-value>)',
        destructive: 'oklch(var(--destructive) / <alpha-value>)',
        'destructive-fg': 'oklch(var(--destructive-fg) / <alpha-value>)',

        // Cream, and the two status hues the booking states need. Added rather
        // than reusing `primary` so "free" and "your booking" never render in
        // the same colour as "the brand".
        accent: 'oklch(var(--accent) / <alpha-value>)',
        'accent-fg': 'oklch(var(--accent-fg) / <alpha-value>)',
        success: 'oklch(var(--success) / <alpha-value>)',
        warning: 'oklch(var(--warning) / <alpha-value>)',
      },

      fontFamily: {
        // Loaded in index.html with a preconnect. Each stack ends in a real
        // system fallback, so an offline container renders in a comparable
        // grotesque instead of dropping to Times.
        sans: ['Inter', 'ui-sans-serif', 'system-ui', '-apple-system', 'Segoe UI', 'sans-serif'],
        display: ['Archivo', 'Inter', 'Impact', 'Haettenschweiler', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },

      fontSize: {
        // Display steps. Set as [size, {lineHeight, letterSpacing}] so a
        // headline cannot be used without the tracking it was tuned with.
        'display-sm': ['2.5rem', { lineHeight: '0.95', letterSpacing: '-0.03em' }],
        'display-md': ['3.75rem', { lineHeight: '0.9', letterSpacing: '-0.04em' }],
        'display-lg': ['5.5rem', { lineHeight: '0.86', letterSpacing: '-0.045em' }],
        'display-xl': ['8rem', { lineHeight: '0.82', letterSpacing: '-0.05em' }],
      },

      keyframes: {
        'rise-in': {
          from: { opacity: '0', transform: 'translateY(0.75rem)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        marquee: {
          from: { transform: 'translateX(0)' },
          // -50% because the track renders its content twice; the loop point
          // lands exactly where the first copy ends, so there is no jump.
          to: { transform: 'translateX(-50%)' },
        },
        'pulse-ring': {
          '0%': { boxShadow: '0 0 0 0 oklch(var(--primary) / 0.5)' },
          '70%': { boxShadow: '0 0 0 0.6rem oklch(var(--primary) / 0)' },
          '100%': { boxShadow: '0 0 0 0 oklch(var(--primary) / 0)' },
        },
      },

      animation: {
        'rise-in': 'rise-in 0.5s cubic-bezier(0.16, 1, 0.3, 1) both',
        marquee: 'marquee 38s linear infinite',
        'pulse-ring': 'pulse-ring 2s ease-out infinite',
      },
    },
  },
  plugins: [],
}

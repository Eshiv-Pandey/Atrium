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
      },
    },
  },
  plugins: [],
}

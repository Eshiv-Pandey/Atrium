import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'
import { Suspense, lazy } from 'react'
import { Nav } from '@/components/Nav'
import { Wordmark } from '@/components/Logo'
import { ToastViewport } from '@/components/Toast'

// Devtools are a named export loaded lazily and only outside production, so
// the bundle a reviewer downloads does not carry a debugging panel.
const RouterDevtools = import.meta.env.PROD
  ? () => null
  : lazy(() =>
      import('@tanstack/router-devtools').then((m) => ({
        default: m.TanStackRouterDevtools,
      })),
    )

/**
 * The router's context carries the QueryClient.
 *
 * This is what lets a route's `beforeLoad` reach the same cache the components
 * read — the alternative is importing a module-level client, which works in
 * the app but makes every router test share one global cache and leak state
 * between cases.
 */
export type RouterContext = {
  queryClient: QueryClient
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFound,
})

function RootLayout() {
  return (
    // `grain` puts a fixed film-grain overlay above the whole document. It is
    // the one piece of the reference art that has to be global: applied
    // per-section, the tiling seams line up with section edges and the texture
    // reads as a set of patches rather than as one surface.
    <div className="grain relative flex min-h-dvh flex-col bg-background text-foreground">
      {/*
        Decorative light. Fixed and behind everything, so scrolling moves
        content through the wash instead of dragging the wash along with it.
        aria-hidden and pointer-events-none: it is lighting, not content.
      */}
      <div
        aria-hidden="true"
        className="ember-wash pointer-events-none fixed inset-0 -z-10"
      />

      {/*
        A skip link is the one accessibility affordance that cannot be
        retrofitted by a component library: without it, a keyboard user tabs
        through every nav item on every navigation before reaching content.
        It is visually hidden until focused.
      */}
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-[70] focus:rounded-md focus:bg-primary focus:px-4 focus:py-2 focus:font-semibold focus:text-primary-fg"
      >
        Skip to content
      </a>

      <Nav />

      <main id="main" className="mx-auto w-full max-w-6xl flex-1 px-4 py-10 sm:px-6">
        <Outlet />
      </main>

      <SiteFooter />

      <ToastViewport />

      <Suspense fallback={null}>
        <RouterDevtools position="bottom-right" />
      </Suspense>
    </div>
  )
}

/**
 * Footer.
 *
 * Carries the one operational fact worth repeating on every page — that a room
 * nobody checks into comes back — because it is the rule most likely to
 * surprise someone whose booking disappeared.
 */
function SiteFooter() {
  return (
    <footer className="mt-16 border-t border-border/60">
      <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-10 sm:flex-row sm:items-center sm:justify-between sm:px-6">
        <div className="flex items-center gap-3">
          <Wordmark />
        </div>

        <p className="max-w-sm text-sm leading-relaxed text-muted-fg">
          Check in within 15 minutes of your start time, or the room goes back
          on the board for someone else.
        </p>
      </div>
    </footer>
  )
}

function NotFound() {
  return (
    <div className="relative isolate mx-auto max-w-6xl px-4 py-24 text-center">
      <span
        aria-hidden="true"
        className="ghost-type fade-b pointer-events-none absolute left-1/2 top-4 -z-10 -translate-x-1/2 select-none text-[8rem] sm:text-[12rem]"
      >
        404
      </span>
      <p className="micro">Dead end</p>
      <h1 className="display-xl mt-3 text-display-sm">
        We couldn&rsquo;t find that page
      </h1>
      <p className="mx-auto mt-4 max-w-md text-pretty leading-relaxed text-muted-fg">
        The link may be out of date, or the page may have moved.
      </p>
      {/*
        A plain anchor, not a router Link. This renders for paths the route tree
        does not know, including ones outside it, so a full document load is the
        honest way back rather than a client-side navigation from nowhere.
      */}
      <a
        href="/"
        className="mt-8 inline-flex h-10 items-center justify-center rounded-full bg-primary px-5 font-semibold text-primary-fg transition-[filter] hover:brightness-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        Back to home
      </a>
    </div>
  )
}

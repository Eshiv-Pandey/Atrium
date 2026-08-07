import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'
import { Suspense, lazy } from 'react'
import { Nav } from '@/components/Nav'
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
    <div className="min-h-dvh bg-background text-foreground">
      {/*
        A skip link is the one accessibility affordance that cannot be
        retrofitted by a component library: without it, a keyboard user tabs
        through every nav item on every navigation before reaching content.
        It is visually hidden until focused.
      */}
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-primary focus:px-4 focus:py-2 focus:text-primary-fg"
      >
        Skip to content
      </a>

      <Nav />

      <main id="main" className="mx-auto w-full max-w-6xl px-4 py-8 sm:px-6">
        <Outlet />
      </main>

      <ToastViewport />

      <Suspense fallback={null}>
        <RouterDevtools position="bottom-right" />
      </Suspense>
    </div>
  )
}

function NotFound() {
  return (
    <div className="mx-auto max-w-6xl px-4 py-24 text-center">
      <p className="text-sm font-medium text-muted-fg">404</p>
      <h1 className="mt-2 text-2xl font-semibold tracking-tight">
        We couldn&rsquo;t find that page
      </h1>
      <p className="mt-2 text-muted-fg">
        The link may be out of date, or the page may have moved.
      </p>
      <a
        href="/"
        className="mt-6 inline-block rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-fg hover:opacity-90"
      >
        Back to home
      </a>
    </div>
  )
}

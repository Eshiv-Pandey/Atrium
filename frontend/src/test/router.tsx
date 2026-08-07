import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { render } from '@testing-library/react'
import type { ReactNode } from 'react'

/**
 * renderWithRouter renders a component that uses router primitives.
 *
 * Anything containing a `<Link>` needs a router in context or it throws. A real
 * memory router is used rather than a mocked Link because the thing worth
 * asserting on is the resolved `href` — a mock would happily accept a route that
 * does not exist and the test would pass on a broken link.
 *
 * Routes are declared here rather than imported from routeTree.gen.ts: that file
 * is generated, pulls in every real route and its loaders, and would make a
 * component test depend on the whole application graph.
 *
 * Async because the router resolves its first match on a microtask, so nothing
 * is in the document on the tick `render` returns. Awaiting `router.load()`
 * matters more than it looks: without it a `queryByText(...).not.toBeInTheDocument`
 * assertion passes against an empty body, so the negative half of a tri-state
 * test would report green while rendering nothing at all.
 */
export async function renderWithRouter(
  ui: ReactNode,
  { paths = [], initialPath = '/' }: { paths?: string[]; initialPath?: string } = {},
) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })

  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: () => <>{ui}</>,
  })

  // Destinations a link under test can point at. They render a marker rather
  // than nothing so a navigation assertion has something to find.
  const targets = paths.map((path) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path,
      component: () => <div data-testid="navigated">{path}</div>,
    }),
  )

  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute, ...targets]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  })

  // The generic parameter of RouterProvider is tied to a concrete route tree;
  // this one is assembled per call, so the cast is the price of a reusable
  // helper rather than a hint that something is wrong.
  await router.load()
  const result = render(<RouterProvider router={router as never} />)

  return { router, ...result }
}

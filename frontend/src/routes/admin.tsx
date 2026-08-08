import { createFileRoute, Link, Outlet } from '@tanstack/react-router'
import { requireAdmin } from '@/lib/guards'

/**
 * Layout route for the admin section.
 *
 * The guard sits here rather than on each child, so a route added later is
 * protected by default instead of by remembering. Forgetting a guard is a
 * silent failure — the page simply works for everyone — which is exactly the
 * kind of mistake that should be structurally impossible rather than
 * caught in review.
 */
export const Route = createFileRoute('/admin')({
  beforeLoad: requireAdmin,
  component: AdminLayout,
})

const tabs = [
  { to: '/admin/rooms', label: 'Rooms' },
  { to: '/admin/bookings', label: 'Bookings' },
  { to: '/admin/utilization', label: 'Utilization' },
] as const

function AdminLayout() {
  return (
    <div className="space-y-8">
      <header className="relative isolate">
        <span
          aria-hidden="true"
          className="ghost-type fade-b pointer-events-none absolute -top-6 left-0 -z-10 select-none text-[5rem] sm:text-[8rem]"
        >
          Admin
        </span>
        <p className="micro">Operations</p>
        <h1 className="display-xl mt-2 text-display-sm sm:text-display-md">Admin</h1>
      </header>

      <nav aria-label="Admin sections" className="border-b border-border/70">
        <ul className="flex gap-1 overflow-x-auto">
          {tabs.map((tab) => (
            <li key={tab.to}>
              <Link
                to={tab.to}
                // activeProps is how the router marks the current tab. The
                // aria-current goes on with the styling, so the state is
                // announced and not merely coloured.
                activeProps={{
                  className:
                    '-mb-px border-b-2 border-primary text-foreground',
                  'aria-current': 'page',
                }}
                // The router matches this tab's path as a prefix by default,
                // which would light every tab up on a nested route. The admin
                // tabs are siblings, so an exact match is what "current" means
                // here.
                activeOptions={{ exact: true }}
                inactiveProps={{ className: 'border-b-2 border-transparent text-muted-fg' }}
                className="inline-block whitespace-nowrap px-4 py-2.5 text-[0.72rem] font-semibold uppercase tracking-[0.14em] transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {tab.label}
              </Link>
            </li>
          ))}
        </ul>
      </nav>

      <Outlet />
    </div>
  )
}

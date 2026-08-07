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
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Admin</h1>
      </header>

      <nav aria-label="Admin sections" className="border-b border-border">
        <ul className="flex gap-1">
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
                inactiveProps={{ className: 'border-b-2 border-transparent text-muted-fg' }}
                className="inline-block px-4 py-2 text-sm font-medium transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
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

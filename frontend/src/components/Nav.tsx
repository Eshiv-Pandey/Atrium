import { Link } from '@tanstack/react-router'
import { useSession, useLogout } from '@/api/hooks'
import { Wordmark } from '@/components/Logo'
import { cn } from '@/lib/utils'

/**
 * Primary navigation.
 *
 * Sticky and translucent: the ember wash and grain sit behind the whole
 * document, so an opaque bar would cut a flat band across the texture. The
 * blur keeps the links legible over whatever scrolls under them.
 */
export function Nav() {
  const { data: user, isLoading } = useSession()
  const logout = useLogout()

  return (
    <header className="sticky top-0 z-50 border-b border-border/60 bg-background/70 backdrop-blur-xl">
      <nav className="mx-auto flex h-16 max-w-6xl items-center justify-between gap-4 px-4 sm:px-6">
        <div className="flex items-center gap-8">
          <Link to="/" className="transition-opacity hover:opacity-80" aria-label="Atrium — home">
            <Wordmark />
          </Link>

          {user ? (
            <div className="hidden items-center gap-1 sm:flex">
              <NavLink to="/rooms">Rooms</NavLink>
              <NavLink to="/bookings">My bookings</NavLink>
              {user.role === 'admin' ? <NavLink to="/admin">Admin</NavLink> : null}
            </div>
          ) : null}
        </div>

        <div className="flex items-center gap-3">
          {isLoading ? (
            <div className="h-8 w-24 animate-pulse rounded-full bg-muted" />
          ) : user ? (
            <>
              {/* The email is identity, not navigation — kept quiet, and kept
                  off narrow screens where it would crowd the sign-out control. */}
              <span className="hidden text-sm text-muted-fg md:inline" data-numeric>
                {user.email}
              </span>
              <button
                onClick={() => logout.mutate()}
                className="rounded-full border border-border/70 px-4 py-1.5 text-sm font-medium text-muted-fg transition-colors hover:border-foreground/30 hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                Sign out
              </button>
            </>
          ) : (
            <Link
              to="/login"
              className="rounded-full bg-primary px-5 py-2 text-sm font-semibold text-primary-fg transition-transform hover:scale-[1.03] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              Sign in
            </Link>
          )}
        </div>
      </nav>

      {/* Mobile row. The links move below the lockup rather than into a
          hamburger: there are at most three, and a menu that hides three items
          costs a tap to reveal what would have fit on the screen anyway. */}
      {user ? (
        <div className="flex items-center gap-1 overflow-x-auto border-t border-border/60 px-4 py-2 sm:hidden">
          <NavLink to="/rooms">Rooms</NavLink>
          <NavLink to="/bookings">My bookings</NavLink>
          {user.role === 'admin' ? <NavLink to="/admin">Admin</NavLink> : null}
        </div>
      ) : null}
    </header>
  )
}

function NavLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      className={cn(
        'whitespace-nowrap rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors',
        'text-muted-fg hover:bg-muted hover:text-foreground',
      )}
      // The active pill is the only filled element in the bar, so current
      // location is readable without comparing font weights.
      activeProps={{ className: 'bg-foreground/10 text-foreground' }}
    >
      {children}
    </Link>
  )
}

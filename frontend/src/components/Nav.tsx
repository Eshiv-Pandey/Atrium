import { Link } from '@tanstack/react-router'
import { useSession, useLogout } from '@/api/hooks'

export function Nav() {
  const { data: user, isLoading } = useSession()
  const logout = useLogout()

  return (
    <nav className="border-b border-border bg-card">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-4 sm:px-6">
        <div className="flex items-center gap-8">
          <Link
            to="/"
            className="text-lg font-semibold text-card-fg hover:text-primary"
          >
            Atrium
          </Link>

          {user ? (
            <div className="flex gap-6 text-sm">
              <Link
                to="/rooms"
                className="text-muted-fg hover:text-foreground"
                activeProps={{ className: 'text-foreground font-medium' }}
              >
                Rooms
              </Link>
              <Link
                to="/bookings"
                className="text-muted-fg hover:text-foreground"
                activeProps={{ className: 'text-foreground font-medium' }}
              >
                My bookings
              </Link>
              {user.role === 'admin' ? (
                <Link
                  to="/admin"
                  className="text-muted-fg hover:text-foreground"
                  activeProps={{ className: 'text-foreground font-medium' }}
                >
                  Admin
                </Link>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className="flex items-center gap-4">
          {isLoading ? (
            <div className="h-8 w-20 animate-pulse rounded bg-muted" />
          ) : user ? (
            <>
              <span className="text-sm text-muted-fg">{user.email}</span>
              <button
                onClick={() => logout.mutate()}
                className="rounded-md px-3 py-1.5 text-sm text-muted-fg hover:bg-muted hover:text-foreground"
              >
                Sign out
              </button>
            </>
          ) : (
            <Link
              to="/login"
              className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-fg hover:opacity-90"
            >
              Sign in
            </Link>
          )}
        </div>
      </div>
    </nav>
  )
}

import { redirect } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'
import { sessionQueryOptions } from '@/api/hooks'
import type { User } from '@/api/schemas'

/**
 * Route guards run in `beforeLoad`, before a protected component ever mounts.
 *
 * Doing this in the router rather than inside components matters for more than
 * tidiness. A component-level check has to render something first — usually a
 * spinner or, worse, a flash of the protected page — and then redirect from an
 * effect. Guarding in `beforeLoad` means the navigation itself is redirected:
 * the protected component is never constructed, so its queries never fire and
 * there is nothing to flash.
 *
 * These are not a security boundary. Every rule enforced here is enforced
 * again by the server on each request, because anything running in the browser
 * can be bypassed with devtools. What they buy is a correct, non-flickering UI
 * for an honest user.
 */

type GuardContext = {
  queryClient: QueryClient
}

/**
 * requireAuth redirects to the login page when there is no session.
 *
 * `ensureQueryData` resolves from cache when the session is already known and
 * fetches only on a cold load, so an in-app navigation between two protected
 * routes costs no request.
 *
 * The attempted path is carried through as a search param so login can send
 * the user where they were actually going, rather than dumping everyone on a
 * dashboard and making them navigate again.
 */
export async function requireAuth({
  context,
  location,
}: {
  context: GuardContext
  location: { href: string }
}): Promise<{ user: User }> {
  const user = await context.queryClient.ensureQueryData(sessionQueryOptions)

  if (!user) {
    throw redirect({
      to: '/login',
      search: { redirect: location.href },
    })
  }

  return { user }
}

/**
 * requireAdmin additionally rejects members.
 *
 * A signed-in member who follows an admin link is sent to the app rather than
 * to login: bouncing them to a sign-in form they have already completed reads
 * as a broken session, when the real answer is that this page is not for them.
 */
export async function requireAdmin({
  context,
  location,
}: {
  context: GuardContext
  location: { href: string }
}): Promise<{ user: User }> {
  const { user } = await requireAuth({ context, location })

  if (user.role !== 'admin') {
    throw redirect({ to: '/rooms' })
  }

  return { user }
}

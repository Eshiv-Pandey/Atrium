import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { http, HttpResponse } from 'msw'
import { ApiError } from '@/api/client'
import { keys } from '@/api/hooks'
import { API, server } from '@/test/server'
import { requireAdmin, requireAuth } from './guards'

// Guards are plain async functions, so they are tested by calling them rather
// than by rendering a route. A redirect is thrown, not returned, which is what
// lets `beforeLoad` abort a navigation — so every assertion here is on what
// comes out of a rejection.
function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
}

const MEMBER = {
  id: '22222222-2222-4222-8222-222222222222',
  name: 'Ada',
  email: 'ada@atrium.local',
  role: 'member' as const,
}

const ADMIN = { ...MEMBER, role: 'admin' as const }

/** Captures whatever a guard threw, so the test can assert on its shape. */
async function thrownBy(fn: () => Promise<unknown>): Promise<any> {
  try {
    await fn()
  } catch (err) {
    return err
  }
  throw new Error('expected the guard to throw, but it returned')
}

/**
 * Unwraps a thrown redirect to the options it was built with.
 *
 * `redirect()` returns `{ options: { to, search, statusCode } }` rather than a
 * flat object, so reading `.to` off the throw silently yields undefined — and
 * an assertion against undefined would make a broken guard look correct. The
 * router's own `isRedirect` predicate gates that unwrap, so a plain Error can
 * never be mistaken for a redirect with missing fields.
 */
async function redirectFrom(fn: () => Promise<unknown>): Promise<any> {
  const err = await thrownBy(fn)
  expect(isRedirect(err)).toBe(true)
  return err.options
}

function signedOut() {
  server.use(
    http.get(`${API}/auth/me`, () =>
      HttpResponse.json(
        { error: { code: 'unauthorized', message: 'Please sign in.' } },
        { status: 401 },
      ),
    ),
  )
}

function signedInAs(user: typeof MEMBER | typeof ADMIN) {
  server.use(http.get(`${API}/auth/me`, () => HttpResponse.json(user)))
}

describe('requireAuth', () => {
  it('returns the user when a session exists', async () => {
    signedInAs(MEMBER)

    const result = await requireAuth({
      context: { queryClient: newClient() },
      location: { href: '/bookings' },
    })

    expect(result.user.email).toBe('ada@atrium.local')
  })

  it('redirects a signed-out visitor to login', async () => {
    signedOut()

    const redirect = await redirectFrom(() =>
      requireAuth({
        context: { queryClient: newClient() },
        location: { href: '/bookings' },
      }),
    )

    expect(redirect.to).toBe('/login')
  })

  it('carries the attempted path so login can return the user to it', async () => {
    // The reason this is asserted rather than assumed: without the search param
    // every protected link lands on the same default page after sign-in, and a
    // shared URL silently loses its destination.
    signedOut()

    const redirect = await redirectFrom(() =>
      requireAuth({
        context: { queryClient: newClient() },
        location: { href: '/rooms/abc?date=2026-06-15' },
      }),
    )

    expect(redirect.search).toEqual({ redirect: '/rooms/abc?date=2026-06-15' })
  })

  it('propagates a server failure instead of redirecting to login', async () => {
    // A 500 is not a signed-out state. Redirecting on it would show a login form
    // during an outage and invite the user to re-enter credentials that were
    // never the problem.
    server.use(
      http.get(`${API}/auth/me`, () =>
        HttpResponse.json({ error: { code: 'internal', message: 'boom' } }, { status: 500 }),
      ),
    )

    const err = await thrownBy(() =>
      requireAuth({
        context: { queryClient: newClient() },
        location: { href: '/bookings' },
      }),
    )

    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(500)
    expect(err.isRedirect).not.toBe(true)
  })

  it('resolves from cache without a request when the session is known', async () => {
    // Guards run on every navigation. If each one refetched, moving between two
    // protected routes would cost a round trip per hop — and with no handler
    // registered, `onUnhandledRequest: 'error'` makes any request here a failure.
    const qc = newClient()
    qc.setQueryData(keys.me, MEMBER)

    const result = await requireAuth({
      context: { queryClient: qc },
      location: { href: '/bookings' },
    })

    expect(result.user).toEqual(MEMBER)
  })
})

describe('requireAdmin', () => {
  it('admits an admin', async () => {
    signedInAs(ADMIN)

    const result = await requireAdmin({
      context: { queryClient: newClient() },
      location: { href: '/admin/rooms' },
    })

    expect(result.user.role).toBe('admin')
  })

  it('sends a signed-in member to the app, not to login', async () => {
    // The distinction worth keeping: a member who follows an admin link has a
    // perfectly valid session. Bouncing them to a sign-in form they already
    // completed reads as a broken login.
    signedInAs(MEMBER)

    const redirect = await redirectFrom(() =>
      requireAdmin({
        context: { queryClient: newClient() },
        location: { href: '/admin/rooms' },
      }),
    )

    expect(redirect.to).toBe('/rooms')
    // No `redirect` param: there is nothing to return them to, because they were
    // never going to be allowed in.
    expect(redirect.search).toBeUndefined()
  })

  it('still sends a signed-out visitor to login', async () => {
    signedOut()

    const redirect = await redirectFrom(() =>
      requireAdmin({
        context: { queryClient: newClient() },
        location: { href: '/admin/rooms' },
      }),
    )

    expect(redirect.to).toBe('/login')
    expect(redirect.search).toEqual({ redirect: '/admin/rooms' })
  })
})

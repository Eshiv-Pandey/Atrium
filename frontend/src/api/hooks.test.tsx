import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import type { ReactNode } from 'react'
import { API, server } from '@/test/server'
import { keys, useCheckIn, useSession } from './hooks'
import type { Booking } from './schemas'

// retry: false, because a test that retries turns an assertion about one
// response into a wait for three. gcTime: Infinity so a cache entry cannot be
// collected between the mutation and the assertion that reads it.
function newClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  })
}

function wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
}

const ROOM = { id: '11111111-1111-4111-8111-111111111111', name: 'Aurora' }
const USER = {
  id: '22222222-2222-4222-8222-222222222222',
  name: 'Ada',
  email: 'ada@atrium.local',
}

function booking(overrides: Partial<Booking> = {}): Booking {
  return {
    id: '33333333-3333-4333-8333-333333333333',
    roomId: ROOM.id,
    userId: USER.id,
    startTime: '2026-06-15T09:00:00Z',
    endTime: '2026-06-15T10:00:00Z',
    attendeeCount: 2,
    status: 'confirmed',
    checkedInAt: null,
    createdAt: '2026-06-01T12:00:00Z',
    canCheckInNow: true,
    canCancel: true,
    ...overrides,
  }
}

describe('useSession', () => {
  it('resolves to null on 401 rather than throwing', async () => {
    // A 401 is the expected answer for a signed-out visitor. If it threw, every
    // route guard would have to tell "no session" apart from "the server is
    // down", and the landing page would flash an error at someone who simply
    // has not logged in.
    server.use(
      http.get(`${API}/auth/me`, () =>
        HttpResponse.json(
          { error: { code: 'unauthorized', message: 'Please sign in.' } },
          { status: 401 },
        ),
      ),
    )

    const qc = newClient()
    const { result } = renderHook(() => useSession(), { wrapper: wrapper(qc) })

    await waitFor(() => expect(result.current.isPending).toBe(false))
    expect(result.current.data).toBeNull()
    expect(result.current.isError).toBe(false)
  })

  it('still surfaces a 500 as an error', async () => {
    // The other half of the rule above: only 401 is a normal outcome. A server
    // failure that resolved to null would render the app as signed-out and hide
    // a real outage.
    server.use(
      http.get(`${API}/auth/me`, () =>
        HttpResponse.json(
          { error: { code: 'internal', message: 'boom' } },
          { status: 500 },
        ),
      ),
    )

    const qc = newClient()
    const { result } = renderHook(() => useSession(), { wrapper: wrapper(qc) })

    await waitFor(() => expect(result.current.isError).toBe(true))
  })
})

describe('useCheckIn cache patching', () => {
  it('keeps the room and user a single-booking response does not carry', async () => {
    // The bug this guards: check-in returns one booking, which carries no room
    // or user reference. A straight replace would blank the room name out of a
    // row that is on screen at the time.
    const listed = { ...booking(), room: ROOM, user: USER }
    const qc = newClient()
    qc.setQueryData(keys.myBookings({}), { items: [listed], nextCursor: null })

    server.use(
      http.post(`${API}/bookings/${listed.id}/check-in`, () =>
        HttpResponse.json(booking({ checkedInAt: '2026-06-15T09:02:00Z', canCheckInNow: false })),
      ),
    )

    const { result } = renderHook(() => useCheckIn(), { wrapper: wrapper(qc) })
    result.current.mutate(listed.id)

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const cached = qc.getQueryData<{ items: Booking[] }>(keys.myBookings({}))
    const patched = cached!.items[0]

    // The fields check-in changed are updated...
    expect(patched.checkedInAt).toBe('2026-06-15T09:02:00Z')
    expect(patched.canCheckInNow).toBe(false)
    // ...and the ones only the list response ever had survive.
    expect(patched.room).toEqual(ROOM)
    expect(patched.user).toEqual(USER)
  })

  it('patches the same booking under every cached filter key', async () => {
    // The member's list and the admin table hold the same booking under
    // different keys. Missing one leaves two views on screen disagreeing.
    const listed = { ...booking(), room: ROOM, user: USER }
    const qc = newClient()
    qc.setQueryData(keys.myBookings({}), { items: [listed], nextCursor: null })
    qc.setQueryData(keys.myBookings({ status: 'confirmed' }), {
      items: [listed],
      nextCursor: null,
    })
    qc.setQueryData(keys.allBookings({ roomId: ROOM.id }), {
      items: [listed],
      nextCursor: null,
    })

    server.use(
      http.post(`${API}/bookings/${listed.id}/check-in`, () =>
        HttpResponse.json(booking({ checkedInAt: '2026-06-15T09:02:00Z' })),
      ),
    )

    const { result } = renderHook(() => useCheckIn(), { wrapper: wrapper(qc) })
    result.current.mutate(listed.id)
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    for (const key of [
      keys.myBookings({}),
      keys.myBookings({ status: 'confirmed' }),
      keys.allBookings({ roomId: ROOM.id }),
    ]) {
      const cached = qc.getQueryData<{ items: Booking[] }>(key)
      expect(cached!.items[0].checkedInAt).toBe('2026-06-15T09:02:00Z')
      expect(cached!.items[0].room).toEqual(ROOM)
    }
  })

  it('leaves other bookings in the list untouched', async () => {
    const target = { ...booking(), room: ROOM, user: USER }
    const other = {
      ...booking({ id: '44444444-4444-4444-8444-444444444444' }),
      room: ROOM,
      user: USER,
    }
    const qc = newClient()
    qc.setQueryData(keys.myBookings({}), {
      items: [target, other],
      nextCursor: null,
    })

    server.use(
      http.post(`${API}/bookings/${target.id}/check-in`, () =>
        HttpResponse.json(booking({ checkedInAt: '2026-06-15T09:02:00Z' })),
      ),
    )

    const { result } = renderHook(() => useCheckIn(), { wrapper: wrapper(qc) })
    result.current.mutate(target.id)
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const cached = qc.getQueryData<{ items: Booking[] }>(keys.myBookings({}))
    expect(cached!.items[1]).toBe(other)
    expect(cached!.items[1].checkedInAt).toBeNull()
  })

  it('returns the cache entry unchanged when the booking is not in it', async () => {
    // Identity, not just equality. Returning a new object for a list that did
    // not change would re-render every table subscribed to that key.
    const unrelated = {
      items: [{ ...booking({ id: '55555555-5555-4555-8555-555555555555' }), room: ROOM }],
      nextCursor: null,
    }
    const qc = newClient()
    qc.setQueryData(keys.myBookings({}), unrelated)

    const checkedIn = booking({ checkedInAt: '2026-06-15T09:02:00Z' })
    server.use(
      http.post(`${API}/bookings/${checkedIn.id}/check-in`, () =>
        HttpResponse.json(checkedIn),
      ),
    )

    const { result } = renderHook(() => useCheckIn(), { wrapper: wrapper(qc) })
    result.current.mutate(checkedIn.id)
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(qc.getQueryData(keys.myBookings({}))).toBe(unrelated)
  })
})

describe('query keys', () => {
  it('gives rooms a prefix that invalidation can match', () => {
    // `keys.rooms()` is what mutations invalidate; `keys.rooms(filters)` is what
    // a component fetches. The first has to be a prefix of the second or a
    // successful booking would never refresh the room list.
    const prefix = keys.rooms()
    const withFilters = keys.rooms({ minCapacity: 4 })
    expect(withFilters.slice(0, prefix.length)).toEqual([...prefix])
  })

  it('keys both booking lists under a shared "bookings" root', () => {
    // Mutations invalidate ['bookings'], which has to reach the member list and
    // the admin table alike.
    expect(keys.myBookings({})[0]).toBe('bookings')
    expect(keys.allBookings({})[0]).toBe('bookings')
  })

  it('separates availability by room and window', () => {
    const a = keys.availability(ROOM.id, '2026-06-15T09:00:00Z', '2026-06-15T17:00:00Z')
    const b = keys.availability(ROOM.id, '2026-06-16T09:00:00Z', '2026-06-16T17:00:00Z')
    expect(a).not.toEqual(b)
  })
})

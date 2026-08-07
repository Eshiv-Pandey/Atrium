import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { ApiError, api, buildQuery } from './client'
import {
  authResponseSchema,
  userSchema,
  roomSchema,
  bookingSchema,
  availabilitySchema,
  utilizationSchema,
  listResponseSchema,
  type Booking,
  type Room,
  type Utilization,
} from './schemas'

// Query keys are centralised so an invalidation and a fetch can never disagree
// about the key they are talking about. A typo in an inline array is silent —
// the mutation succeeds, the list simply never refreshes — and that class of
// bug is invisible until someone notices stale data.
export const keys = {
  me: ['auth', 'me'] as const,
  rooms: (params?: unknown) =>
    params === undefined ? (['rooms'] as const) : (['rooms', params] as const),
  availability: (roomId: string, start: string, end: string) =>
    ['rooms', roomId, 'availability', start, end] as const,
  myBookings: (params?: unknown) => ['bookings', 'me', params] as const,
  allBookings: (params?: unknown) => ['bookings', 'all', params] as const,
  utilization: (start: string, end: string) =>
    ['utilization', start, end] as const,
}

const roomListSchema = listResponseSchema(roomSchema)
const bookingListSchema = listResponseSchema(bookingSchema)
const utilizationListSchema = listResponseSchema(utilizationSchema)

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

/**
 * sessionQueryOptions is shared by the hook and by route guards.
 *
 * Defined once as options rather than only as a hook so a router `beforeLoad`
 * can `ensureQueryData(sessionQueryOptions)` and get the same cache entry the
 * components read. If the guard fetched separately, a protected route would
 * check one copy of the session while the page rendered from another.
 *
 * A 401 here is the expected answer for a signed-out visitor, not a failure —
 * so it resolves to null rather than throwing. If it threw, every route guard
 * would have to distinguish "no session" from "the server is down", and the
 * landing page would flash an error to someone who simply has not logged in.
 * Any other error still throws, because that genuinely is a failure.
 */
export const sessionQueryOptions = {
  queryKey: keys.me,
  queryFn: async ({ signal }: { signal?: AbortSignal }) => {
    try {
      return await api.get('/auth/me', userSchema, signal)
    } catch (err) {
      if (err instanceof ApiError && err.isUnauthorized) return null
      throw err
    }
  },

  // Retrying a 401 would delay the signed-out render for no benefit; the
  // answer will not change on a second attempt.
  retry: false,

  // The session only changes through a mutation on this page, and each of
  // those writes the cache directly. Refetching on every mount would put a
  // request in front of every navigation for data that cannot have gone stale
  // without us knowing.
  staleTime: Infinity,
}

/** useSession reports who is signed in, or null. */
export function useSession() {
  return useQuery(sessionQueryOptions)
}

export function useRegister() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; password: string; name: string }) =>
      api.post('/auth/register', authResponseSchema, input),
    onSuccess: (data) => qc.setQueryData(keys.me, data.user),
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api.post('/auth/login', authResponseSchema, input),
    onSuccess: (data) => qc.setQueryData(keys.me, data.user),
  })
}

export function useDemoLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (role: 'member' | 'admin') =>
      api.post('/auth/demo-login', authResponseSchema, { role }),
    onSuccess: (data) => qc.setQueryData(keys.me, data.user),
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.post('/auth/logout', z.void()),
    onSuccess: () => {
      // Everything cached was fetched as this user: their bookings, and room
      // availability they were authorised to see. Clearing rather than
      // invalidating means the next person to sign in on this browser cannot
      // see a frame of the previous person's data before the refetch lands.
      qc.clear()
      qc.setQueryData(keys.me, null)
    },
  })
}

// ---------------------------------------------------------------------------
// Rooms
// ---------------------------------------------------------------------------

export type RoomFilters = {
  minCapacity?: number
  amenities?: string[]
  /** RFC 3339. Supplying both bounds annotates each room with `available`. */
  start?: string
  end?: string
}

export function useRooms(filters: RoomFilters = {}) {
  return useQuery({
    queryKey: keys.rooms(filters),
    queryFn: ({ signal }) =>
      api.get(
        `/rooms${buildQuery({
          min_capacity: filters.minCapacity,
          amenities: filters.amenities,
          start: filters.start,
          end: filters.end,
        })}`,
        roomListSchema,
        signal,
      ),
    select: (data): Room[] => data.items,
  })
}

export function useAvailability(roomId: string, start: string, end: string) {
  return useQuery({
    queryKey: keys.availability(roomId, start, end),
    queryFn: ({ signal }) =>
      api.get(
        `/rooms/${roomId}/availability${buildQuery({ start, end })}`,
        availabilitySchema,
        signal,
      ),
    enabled: Boolean(roomId && start && end),

    // Availability is the one thing another member can invalidate from
    // another browser. A short window keeps the timeline honest without
    // polling.
    staleTime: 15_000,
  })
}

// ---------------------------------------------------------------------------
// Bookings
// ---------------------------------------------------------------------------

export type BookingFilters = {
  from?: string
  to?: string
  status?: 'confirmed' | 'cancelled' | 'released'
  limit?: number
  cursor?: string
}

export function useMyBookings(filters: BookingFilters = {}) {
  return useQuery({
    queryKey: keys.myBookings(filters),
    queryFn: ({ signal }) =>
      api.get(
        `/bookings/me${buildQuery({
          from: filters.from,
          to: filters.to,
          status: filters.status,
          limit: filters.limit,
          cursor: filters.cursor,
        })}`,
        bookingListSchema,
        signal,
      ),
  })
}

export type CreateBookingInput = {
  roomId: string
  startTime: string
  endTime: string
  attendeeCount: number
  /**
   * Generated once per form session, not per submit. That is the whole point:
   * two submits from one filled-in form carry the same key, so the second is
   * recognised as a replay and returns the first booking instead of making a
   * second one.
   */
  idempotencyKey: string
}

export function useCreateBooking() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ idempotencyKey, ...body }: CreateBookingInput) =>
      api.post('/bookings', bookingSchema, body, idempotencyKey),
    onSuccess: (booking) => {
      qc.invalidateQueries({ queryKey: ['bookings'] })
      // The room is now busy for that span, so any cached availability and any
      // room list carrying an `available` flag are both wrong.
      qc.invalidateQueries({ queryKey: keys.rooms() })
      void booking
    },
    onError: (err) => {
      // A 409 means someone else took the slot between the page loading and
      // the click. The timeline on screen is stale, and refetching it is the
      // difference between "that didn't work" and showing what is actually
      // free now.
      if (err instanceof ApiError && err.isConflict) {
        qc.invalidateQueries({ queryKey: keys.rooms() })
      }
    },
  })
}

export function useCheckIn() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post(`/bookings/${id}/check-in`, bookingSchema),
    onSuccess: (updated) => {
      // The server returns the updated booking, so every cached list can be
      // patched in place. Invalidating instead would blank the row and
      // re-render it a moment later — a visible flicker for data already in
      // hand.
      patchBookingInCaches(qc, updated)
    },
  })
}

export function useCancelBooking() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/bookings/${id}`, z.void()),
    onSuccess: (_data, id) => {
      qc.invalidateQueries({ queryKey: ['bookings'] })
      // Cancelling frees the slot for everyone else.
      qc.invalidateQueries({ queryKey: keys.rooms() })
      void id
    },
  })
}

/**
 * patchBookingInCaches replaces one booking wherever it is already cached.
 *
 * Walks every cached list rather than a known key, because the same booking
 * appears in the member's list and, for an admin, in the all-bookings table —
 * under different filter keys. Missing one would leave two views of the same
 * row disagreeing on screen.
 */
function patchBookingInCaches(
  qc: ReturnType<typeof useQueryClient>,
  updated: Booking,
) {
  qc.setQueriesData<{ items: Booking[]; nextCursor: string | null }>(
    { queryKey: ['bookings'] },
    (old) => {
      if (!old) return old
      let changed = false
      const items = old.items.map((b) => {
        if (b.id !== updated.id) return b
        changed = true
        // Merged rather than replaced. `updated` came from check-in, which is a
        // single-booking response and therefore carries no room or user
        // reference — a straight swap would blank the room name out of a row
        // that is on screen at the time. The cached row already has those, and
        // they cannot have changed: a booking never moves rooms or owners.
        return {
          ...updated,
          room: updated.room ?? b.room,
          user: updated.user ?? b.user,
        }
      })
      return changed ? { ...old, items } : old
    },
  )
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

export function useAllBookings(
  filters: BookingFilters & { roomId?: string; userId?: string } = {},
) {
  return useQuery({
    queryKey: keys.allBookings(filters),
    queryFn: ({ signal }) =>
      api.get(
        `/bookings${buildQuery({
          room_id: filters.roomId,
          user_id: filters.userId,
          from: filters.from,
          to: filters.to,
          status: filters.status,
          limit: filters.limit,
          cursor: filters.cursor,
        })}`,
        bookingListSchema,
        signal,
      ),
  })
}

export function useUtilization(start: string, end: string) {
  return useQuery({
    queryKey: keys.utilization(start, end),
    queryFn: ({ signal }) =>
      api.get(
        `/admin/utilization${buildQuery({ start, end })}`,
        utilizationListSchema,
        signal,
      ),
    select: (data): Utilization[] => data.items,
    enabled: Boolean(start && end),
  })
}

export function useCreateRoom() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; capacity: number; amenities: string[] }) =>
      api.post('/rooms', roomSchema, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.rooms() }),
  })
}

export function useUpdateRoom() {
  const qc = useQueryClient()
  return useMutation({
    // Only the supplied keys are sent, which is what makes this a PATCH: an
    // omitted field is left alone server-side rather than overwritten with a
    // zero value.
    mutationFn: ({
      id,
      ...patch
    }: {
      id: string
      name?: string
      capacity?: number
      amenities?: string[]
    }) => api.patch(`/rooms/${id}`, roomSchema, patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.rooms() }),
  })
}

export function useDeleteRoom() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/rooms/${id}`, z.void()),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.rooms() })
      qc.invalidateQueries({ queryKey: ['bookings'] })
    },
  })
}

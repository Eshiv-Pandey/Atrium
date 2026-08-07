import { http, HttpResponse } from 'msw'
import { z } from 'zod'
import { API, server } from '@/test/server'
import {
  ApiError,
  SchemaError,
  api,
  buildQuery,
  errorMessage,
} from './client'

// A minimal schema, so a failure points at the client rather than at a
// disagreement about the shape of a booking.
const thingSchema = z.object({ id: z.string(), n: z.number() })

describe('request', () => {
  it('prefixes /api and parses a valid body', async () => {
    let seenUrl = ''
    server.use(
      http.get(`${API}/things`, ({ request }) => {
        seenUrl = request.url
        return HttpResponse.json({ id: 'a', n: 1 })
      }),
    )

    const got = await api.get('/things', thingSchema)

    expect(got).toEqual({ id: 'a', n: 1 })
    // The caller passed '/things', not '/api/things'. Exactly one place knows
    // the base path, and this is the assertion that keeps it that way.
    expect(new URL(seenUrl).pathname).toBe('/api/things')
  })

  it('sends the session cookie on every request', async () => {
    let credentials: RequestCredentials | undefined
    server.use(
      http.get(`${API}/things`, ({ request }) => {
        credentials = request.credentials
        return HttpResponse.json({ id: 'a', n: 1 })
      }),
    )

    await api.get('/things', thingSchema)

    // The session is httpOnly, so it is only ever sent by the browser. A
    // request that omitted credentials would be anonymous and 401 in a way that
    // looks like an expired login.
    expect(credentials).toBe('same-origin')
  })

  it('sets Content-Type only when there is a body', async () => {
    const seen: Array<string | null> = []
    server.use(
      http.get(`${API}/things`, ({ request }) => {
        seen.push(request.headers.get('content-type'))
        return HttpResponse.json({ id: 'a', n: 1 })
      }),
      http.post(`${API}/things`, ({ request }) => {
        seen.push(request.headers.get('content-type'))
        return HttpResponse.json({ id: 'a', n: 1 })
      }),
    )

    await api.get('/things', thingSchema)
    await api.post('/things', thingSchema, { n: 1 })

    expect(seen[0]).toBeNull()
    expect(seen[1]).toContain('application/json')
  })

  it('sends Idempotency-Key when given, and omits it otherwise', async () => {
    const seen: Array<string | null> = []
    server.use(
      http.post(`${API}/bookings`, ({ request }) => {
        seen.push(request.headers.get('idempotency-key'))
        return HttpResponse.json({ id: 'a', n: 1 })
      }),
    )

    await api.post('/bookings', thingSchema, { roomId: 'r' }, 'key-123')
    await api.post('/bookings', thingSchema, { roomId: 'r' })

    expect(seen[0]).toBe('key-123')
    // Absent rather than empty: the backend treats a blank key as present and
    // would scope a replay to the empty string.
    expect(seen[1]).toBeNull()
  })

  it('returns undefined for 204 without trying to parse a body', async () => {
    server.use(
      http.delete(`${API}/bookings/abc`, () => new HttpResponse(null, { status: 204 })),
    )

    // z.void() is what callers pass for these. Parsing a 204 as JSON would
    // throw on an empty body, which would surface as a failed cancellation for
    // a request that in fact succeeded.
    await expect(api.delete('/bookings/abc', z.void())).resolves.toBeUndefined()
  })
})

describe('error envelope', () => {
  it('carries status, code, message and fields off the envelope', async () => {
    server.use(
      http.post(`${API}/bookings`, () =>
        HttpResponse.json(
          {
            error: {
              code: 'validation_failed',
              message: 'Bookings run from 15 minutes to 8 hours.',
              fields: { endTime: 'must be after startTime' },
            },
          },
          { status: 422 },
        ),
      ),
    )

    const err = await api.post('/bookings', thingSchema, {}).catch((e) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(422)
    expect(err.code).toBe('validation_failed')
    expect(err.message).toBe('Bookings run from 15 minutes to 8 hours.')
    expect(err.fields).toEqual({ endTime: 'must be after startTime' })
  })

  it.each([
    [401, 'isUnauthorized'],
    [409, 'isConflict'],
    [422, 'isValidation'],
  ] as const)('exposes %i as %s', async (status, flag) => {
    server.use(
      http.get(`${API}/things`, () =>
        HttpResponse.json(
          { error: { code: 'x', message: 'nope' } },
          { status },
        ),
      ),
    )

    const err: ApiError = await api.get('/things', thingSchema).catch((e) => e)

    // Components branch on these — a 409 on booking triggers an availability
    // refetch — so each has to be true for its own status and nothing else.
    expect(err[flag]).toBe(true)
    const others = (['isUnauthorized', 'isConflict', 'isValidation'] as const).filter(
      (f) => f !== flag,
    )
    for (const other of others) expect(err[other]).toBe(false)
  })

  it('falls back to a readable message when the body is not JSON', async () => {
    // A proxy timeout page or a crash before the handler ran. There is still a
    // user waiting, so this has to produce something showable rather than
    // throwing while building the error.
    server.use(
      http.get(`${API}/things`, () =>
        new HttpResponse('<html>502 Bad Gateway</html>', {
          status: 502,
          headers: { 'Content-Type': 'text/html' },
        }),
      ),
    )

    const err: ApiError = await api.get('/things', thingSchema).catch((e) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(502)
    expect(err.code).toBe('unknown')
    expect(err.message).toMatch(/something went wrong/i)
  })

  it('falls back when the JSON does not match the envelope', async () => {
    server.use(
      http.get(`${API}/things`, () =>
        HttpResponse.json({ detail: 'not our shape' }, { status: 403 }),
      ),
    )

    const err: ApiError = await api.get('/things', thingSchema).catch((e) => e)

    expect(err.code).toBe('unknown')
    expect(err.message).toMatch(/permission/i)
  })
})

describe('SchemaError', () => {
  it('throws SchemaError, not ApiError, when a 200 body is the wrong shape', async () => {
    // The distinction is the point: an ApiError is a normal outcome to show the
    // user, a SchemaError is a backend change that landed without a frontend
    // change. Collapsing them would let a contract break render as "something
    // went wrong" and never get investigated.
    server.use(
      http.get(`${API}/things`, () => HttpResponse.json({ id: 'a', n: 'not-a-number' })),
    )

    const err = await api.get('/things', thingSchema).catch((e) => e)

    expect(err).toBeInstanceOf(SchemaError)
    expect(err).not.toBeInstanceOf(ApiError)
    expect(err.path).toBe('/things')
    expect(err.issues.length).toBeGreaterThan(0)
  })
})

describe('errorMessage', () => {
  it("prefers the server's own message", () => {
    const err = new ApiError(409, 'conflict', 'That room is already booked.')
    expect(errorMessage(err)).toBe('That room is already booked.')
  })

  it('describes a schema mismatch without leaking internals', () => {
    const msg = errorMessage(new SchemaError('/things', []))
    expect(msg).toMatch(/unexpected response/i)
    expect(msg).not.toMatch(/things/)
  })

  it('treats anything else as a connection problem', () => {
    expect(errorMessage(new TypeError('Failed to fetch'))).toMatch(/connection/i)
    expect(errorMessage('a bare string')).toMatch(/connection/i)
  })
})

describe('buildQuery', () => {
  it('returns an empty string when everything is absent', () => {
    // Not '?' — a bare question mark would make the URL differ from the one
    // TanStack Query keyed the cache on.
    expect(buildQuery({ a: undefined, b: null, c: '' })).toBe('')
  })

  it('omits empty values rather than sending blanks', () => {
    // `?status=` arrives as an empty string and is rejected as an invalid
    // status, where an absent parameter means "no filter".
    expect(buildQuery({ status: '', minCapacity: 4 })).toBe('?minCapacity=4')
  })

  it('keeps zero, which is a value rather than an absence', () => {
    expect(buildQuery({ minCapacity: 0 })).toBe('?minCapacity=0')
  })

  it('joins arrays with commas and drops empty ones', () => {
    expect(buildQuery({ amenities: ['tv', 'whiteboard'] })).toBe(
      '?amenities=tv%2Cwhiteboard',
    )
    expect(buildQuery({ amenities: [] })).toBe('')
  })

  it('encodes values that would otherwise break the query string', () => {
    // The '+' in an RFC 3339 offset decodes to a space if it is not escaped,
    // and the backend rejects "09:00:00 05:30" — correctly, since it cannot
    // know which of the two was meant.
    const q = buildQuery({ start: '2026-01-02T09:00:00+05:30' })
    expect(q).toContain('%2B05%3A30')
    expect(new URLSearchParams(q.slice(1)).get('start')).toBe(
      '2026-01-02T09:00:00+05:30',
    )
  })
})

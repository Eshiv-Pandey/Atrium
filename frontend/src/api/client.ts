import { z } from 'zod'
import { apiErrorSchema } from './schemas'

/**
 * ApiError is what every failed request throws.
 *
 * The status and code are carried separately from the message so components
 * can branch on them — a 409 on booking triggers an availability refetch and a
 * specific explanation, which would be impossible if the only thing thrown was
 * a string.
 */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly fields?: Record<string, string>,
  ) {
    super(message)
    this.name = 'ApiError'
  }

  /** True when the server rejected the request on business rules. */
  get isValidation(): boolean {
    return this.status === 422
  }

  /** True when the slot was taken, or the state changed underneath us. */
  get isConflict(): boolean {
    return this.status === 409
  }

  /** True when the session is missing or expired. */
  get isUnauthorized(): boolean {
    return this.status === 401
  }
}

/**
 * SchemaError means the server answered with a shape we did not expect.
 *
 * Kept distinct from ApiError because the two need different handling: an
 * ApiError is a normal outcome to show the user, while a SchemaError is a bug
 * — a backend change that landed without a frontend change. Collapsing them
 * would let a genuine contract break render as "something went wrong" and
 * never get investigated.
 */
export class SchemaError extends Error {
  constructor(
    readonly path: string,
    readonly issues: z.ZodIssue[],
  ) {
    super(`Response from ${path} did not match the expected shape`)
    this.name = 'SchemaError'
  }
}

type RequestOptions = {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  body?: unknown
  /** Sent as the Idempotency-Key header. Only meaningful on POST /bookings. */
  idempotencyKey?: string
  signal?: AbortSignal
}

/**
 * request performs one API call and validates the response.
 *
 * Every call goes through here, so there is exactly one place that knows the
 * API's base path, sends credentials, parses the error envelope, and validates
 * a body. A second fetch call somewhere in a component would be a second place
 * that could forget any of those.
 */
async function request<T>(
  path: string,
  schema: z.ZodType<T>,
  options: RequestOptions = {},
): Promise<T> {
  const { method = 'GET', body, idempotencyKey, signal } = options

  const headers: Record<string, string> = {}
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  if (idempotencyKey) {
    headers['Idempotency-Key'] = idempotencyKey
  }

  const response = await fetch(`/api${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,

    // The session is an httpOnly cookie, so it is never readable from
    // JavaScript and must be sent by the browser rather than attached by us.
    // 'same-origin' is the default for same-origin requests, but stating it
    // documents that this is deliberate rather than incidental.
    credentials: 'same-origin',
  })

  if (!response.ok) {
    throw await toApiError(response)
  }

  // 204 has no body to parse. Callers pass z.void() for these.
  if (response.status === 204) {
    return undefined as T
  }

  const json: unknown = await response.json()
  const parsed = schema.safeParse(json)
  if (!parsed.success) {
    // Logged as well as thrown: a schema mismatch is a contract break, and the
    // console is where a developer will look for the field that moved.
    console.error(`Schema mismatch on ${path}`, parsed.error.issues)
    throw new SchemaError(path, parsed.error.issues)
  }

  return parsed.data
}

/**
 * toApiError turns a non-2xx response into an ApiError.
 *
 * The backend returns one error envelope for every failure, so this parses it
 * once. A response that is not JSON at all — a proxy timeout page, a crash
 * before the handler ran — still has to produce a usable error, hence the
 * fallback.
 */
async function toApiError(response: Response): Promise<ApiError> {
  let payload: unknown
  try {
    payload = await response.json()
  } catch {
    return new ApiError(
      response.status,
      'unknown',
      genericMessage(response.status),
    )
  }

  const parsed = apiErrorSchema.safeParse(payload)
  if (!parsed.success) {
    return new ApiError(
      response.status,
      'unknown',
      genericMessage(response.status),
    )
  }

  const { code, message, fields } = parsed.data.error
  return new ApiError(response.status, code, message, fields)
}

function genericMessage(status: number): string {
  if (status >= 500) return 'Something went wrong on our end. Please try again.'
  if (status === 404) return 'That could not be found.'
  if (status === 401) return 'Please sign in to continue.'
  if (status === 403) return 'You do not have permission to do that.'
  return 'That request could not be completed.'
}

/**
 * errorMessage extracts something worth showing a user from any thrown value.
 *
 * The server's own message is preferred wherever there is one, because it is
 * specific — "that room is already booked for part of this window" beats any
 * phrasing the client could invent without knowing the rule that failed.
 * Everything else is a network or programming failure, which gets a generic
 * line rather than a stack trace.
 */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof SchemaError) {
    return 'The server sent an unexpected response. Please refresh and try again.'
  }
  return 'Please check your connection and try again.'
}

/**
 * buildQuery serialises search parameters, omitting empty ones.
 *
 * Omitting rather than sending blanks matters: the backend treats an absent
 * parameter as "no filter", and `?status=` would otherwise arrive as an empty
 * string and be rejected as an invalid status.
 */
export function buildQuery(
  params: Record<string, string | number | string[] | undefined | null>,
): string {
  const search = new URLSearchParams()

  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue

    if (Array.isArray(value)) {
      if (value.length > 0) search.set(key, value.join(','))
      continue
    }
    search.set(key, String(value))
  }

  const query = search.toString()
  return query ? `?${query}` : ''
}

export const api = {
  get: <T>(path: string, schema: z.ZodType<T>, signal?: AbortSignal) =>
    request(path, schema, { method: 'GET', signal }),

  post: <T>(
    path: string,
    schema: z.ZodType<T>,
    body?: unknown,
    idempotencyKey?: string,
  ) => request(path, schema, { method: 'POST', body, idempotencyKey }),

  patch: <T>(path: string, schema: z.ZodType<T>, body?: unknown) =>
    request(path, schema, { method: 'PATCH', body }),

  delete: <T>(path: string, schema: z.ZodType<T>) =>
    request(path, schema, { method: 'DELETE' }),
}

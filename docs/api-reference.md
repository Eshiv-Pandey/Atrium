# API reference

The Atrium API is a small JSON-over-HTTP surface under the `/api` prefix. Every response uses the same error shape, and every route sits in exactly one of three authorization groups. This page lists the endpoints, who can reach them, the error contract they all share, and the handful of behaviors (idempotency, pagination, information hiding) that are worth understanding before you build against it.

## Authorization

The whole authorization model lives in one function, `http/router.go:NewRouter`, so the answer to "can a member reach this?" never requires reading every handler. There are three groups.

| Access | Endpoints |
|---|---|
| Public | `POST /api/auth/register`, `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/healthz`, and `POST /api/auth/demo-login` (only when demo login is enabled) |
| Member (any signed-in user) | `GET /api/auth/me`, `GET /api/rooms`, `GET /api/rooms/{id}/availability`, `POST /api/bookings`, `GET /api/bookings/me`, `POST /api/bookings/{id}/check-in`, `DELETE /api/bookings/{id}` |
| Admin | `POST /api/rooms`, `PATCH /api/rooms/{id}`, `DELETE /api/rooms/{id}`, `GET /api/bookings`, `GET /api/admin/utilization` |

Two things about this table repay a second look.

**Same path, different method, different authority.** `GET /api/rooms` is a member route, but `POST /api/rooms` is admin. Reading the room catalogue is what members do; changing it is not. The method is part of the authorization decision, not just the path.

**Demo login is enforced by absence.** When demo login is disabled, the route is not mounted at all. There is no flag check inside a handler that someone could forget to write or could get wrong; the endpoint simply does not exist. A route that is not mounted cannot be reached by a misconfigured proxy or a stale client. See [Configuration](configuration.md#demo-login) for when it is on.

Authorization is enforced by middleware: `RequireAuth` validates the session for member routes, and `RequireAdmin` additionally rejects non-admins. Both run before the handler, so a handler never has to check "is this user allowed here" itself.

## Sessions

Authentication is a signed JWT carried in an httpOnly cookie. You obtain it by registering or logging in, the browser sends it automatically on every subsequent request, and JavaScript can never read it. There is no bearer-token header to set and no token to store client-side.

- `POST /api/auth/register` creates an account and signs it in.
- `POST /api/auth/login` signs in an existing account.
- `POST /api/auth/logout` clears the cookie.
- `GET /api/auth/me` returns the current user, and is how the frontend knows who you are.

An expired or missing token on a protected route returns a 401, which the frontend handles by redirecting to `/login` with a `redirect` search param carrying the attempted URL. Tokens are not revocable before they expire; the reasoning is in [Configuration](configuration.md#sessions-and-tokens).

## The error contract

Every failure, from every endpoint, returns one envelope:

```json
{
  "error": {
    "code": "conflict",
    "message": "That slot is already booked.",
    "fields": { }
  }
}
```

`code` is a stable, machine-readable string the frontend switches on. `message` is written for a person and may be reworded without breaking a client. `fields` is present only on validation errors, mapping each offending input to its own message so a form can highlight the specific field rather than showing one message above everything.

This envelope is produced in exactly one place, `writeError` in `http/errors.go`. Handlers return wrapped domain errors and never choose a status code themselves, so the mapping cannot drift between endpoints:

| Domain error | Status | Code |
|---|---|---|
| `ErrValidation` | 422 | `validation_failed` |
| `ErrUnauthorized` | 401 | `unauthorized` |
| `ErrForbidden` | 403 | `forbidden` |
| `ErrNotFound` | 404 | `not_found` |
| `ErrConflict` | 409 | `conflict` |
| anything else | 500 | `internal_error` |

Two choices about the mapping are deliberate:

- **Validation is 422, not 400.** The request was syntactically valid JSON that the server parsed and understood; it just asked for something the rules forbid. A 400 would suggest the request was malformed, which it was not.
- **5xx bodies are replaced with a generic message.** The real error is logged with request context, but the client is told only "Something went wrong on our end," because internal errors routinely carry SQL, table names, and connection strings that must not leak.

## Two responses that look like bugs

Both of these are information-hiding choices, not mistakes, and they should not be "fixed":

- **Cancelling or checking into someone else's booking returns 404, not 403.** A 403 would confirm that a booking with that id exists but is not yours, which leaks the existence of other people's reservations. A 404 says only "no such booking of yours," which is all a stranger is entitled to know.
- **A malformed UUID in a path returns 404, not 422.** If invalid ids returned 422 while merely-not-yours ids returned 404, a caller could tell the two apart and use the difference to enumerate valid ids. Collapsing both to 404 removes that signal.

## Bookings

`POST /api/bookings` is the endpoint the whole system is built around. A request names a room, a start and end time (RFC 3339 with an explicit offset), and an attendee count. The server validates the business rules, then attempts the booking inside one transaction. The full sequence and the reasoning are on the [Concurrency](concurrency.md) page. From the client's point of view there are three outcomes:

- **201** with the booking, on success.
- **200** with the original booking, if this was an idempotency-key replay (see below).
- **409**, if the slot was taken by someone else first, or a 422 if the request broke a rule like the duration bounds or the attendee count.

### Idempotency

`POST /api/bookings` accepts an optional `Idempotency-Key` header. Replaying a request with a key you have already used returns the original booking with a 200 instead of creating a second one, so a double-submitted form or a retried request is harmless. Keys are scoped per user, so two people can safely generate the same client-side UUID. The replay check runs inside the booking transaction, so even a concurrent replay retries the lookup rather than being misreported as a conflict.

### The rest of the booking lifecycle

- `GET /api/bookings/me` lists the caller's own bookings, newest first.
- `POST /api/bookings/{id}/check-in` records that someone turned up. It is permitted from 5 minutes before the start until 15 minutes after it; outside that window it returns a 409 with a specific reason (not yet open, already checked in, already released as a no-show, or the window has closed).
- `DELETE /api/bookings/{id}` cancels a booking the caller owns. Cancelling an already-cancelled booking succeeds silently, because the caller's desired state already holds. A booking that has already started cannot be cancelled.

## Pagination

The list endpoints (`GET /api/bookings/me` and the admin `GET /api/bookings`) paginate with opaque base64 keyset cursors encoding `(start_time, id)`, not with `OFFSET`. The reason is correctness under change: with `OFFSET`, a booking created while a user is paging shifts every later row down and silently skips one. A keyset cursor points at a specific position in a stable ordering, so paging stays consistent even as rows are inserted. Page size is clamped server-side (default 50, maximum 200) so a client cannot request the whole table at once.

## Request parsing

The query-parameter parser (`http/params.go`) accumulates one error per field and keeps going instead of bailing on the first bad value. A request with three malformed parameters gets all three back at once, in the `fields` map, rather than costing three round trips to discover one problem at a time. Handlers check whether parsing succeeded at the end and, if not, return a single 422 listing every field that failed. This is the same reasoning that `config.Load` applies to startup configuration, applied to requests.

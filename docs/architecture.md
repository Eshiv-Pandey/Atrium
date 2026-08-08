# Architecture

Atrium is two applications that share one origin: a Go API and a React single-page app. This page walks through how each side is layered, how a request travels from a click in the browser to a row in Postgres and back, and where the important boundaries sit. For the concurrency model that the whole design is built around, see [Concurrency](concurrency.md); this page is about structure.

## The shape of it

```
Browser (React SPA)
   |
   |  same-origin /api requests, session cookie attached
   v
Go API  ->  PostgreSQL
```

In development the Vite dev server proxies `/api` to the backend. In production a static host serves the SPA and rewrites `/api/*` to the API service. Either way the browser only ever makes same-origin requests, so the session cookie works without any cross-site exemption and the frontend never learns where the API actually lives. [Deployment](deployment.md) covers the production side of that.

## Backend layers

The backend is a straight line of four layers, each of which knows only about the one below it:

```
cmd/server  ->  http/  ->  service/  ->  store/  ->  PostgreSQL
                  |
             middleware, JWT + cookies
```

| Layer | Responsibility |
|---|---|
| `cmd/server` | Entrypoint. Loads config, connects to the database, assembles the router, starts the server. |
| `http` | Handlers, middleware, and the authorization matrix. Translates between HTTP and the service layer and does nothing else: no SQL, no business rules. |
| `service` | Business rules and validation. This is where rules like "bookings run 15 minutes to 8 hours" live. |
| `store` | SQL. One store per table, using pgx directly with no ORM, so the SQL in the repo is the SQL that runs. |
| `auth` | JWT signing and verification, password hashing, cookie management. |
| `config` | Reads the environment once at startup and fails fast if anything is missing or invalid. |

A few things worth knowing about how the layers talk to each other:

**Errors are domain sentinels, mapped to status codes in one place.** The service layer returns wrapped errors like `domain.ErrConflict` and `domain.ErrValidation`. The HTTP layer catches those and maps each to a status code in exactly one function, `writeError` in `http/errors.go`. Handlers never pick a status code themselves, so the mapping cannot drift between endpoints. The [API reference](api-reference.md#the-error-contract) has the full table.

**The store translates Postgres error codes, not the driver's types.** When an insert trips the overlap constraint, Postgres raises SQLSTATE `23P01`. The store turns that into a predicate the service layer can read (`IsOverlapConflict`) without importing a driver package. That keeps the knowledge of what a `23P01` means in the one layer that talks to the database.

**The authorization matrix lives in one function.** `http/router.go:NewRouter` mounts every route under one of three groups: public, member, or admin. Putting the whole matrix in one place is deliberate. If the answer to "can a member reach this?" were spread across handlers, it would take reading every file to know, and the day someone forgets a check is the day it becomes a vulnerability rather than a bug. See the [API reference](api-reference.md#authorization) for the resulting table.

## Frontend layers

The frontend mirrors the same idea of a single line with clear responsibilities:

```
main.tsx  ->  routes/  ->  lib/guards.ts (beforeLoad)  ->  api/hooks.ts  ->  api/client.ts
```

| Piece | Responsibility |
|---|---|
| `routes/` | File-based routes. Each file exports a `Route`, and the file's location is its URL. |
| `lib/guards.ts` | Route guards that run in `beforeLoad`, before a protected component ever mounts. |
| `api/hooks.ts` | TanStack Query hooks. All server state lives here, cached and shared across routes. |
| `api/client.ts` | The single fetch wrapper. Knows the `/api` base path, sends the cookie, and parses the error envelope. |
| `api/schemas.ts` | Zod schemas that validate every API response at the boundary. |

**Guards run before the component mounts, and they are not a security boundary.** The guards in `lib/guards.ts` run in TanStack Router's `beforeLoad`, so the navigation itself is redirected when a user is not allowed somewhere. The protected component is never constructed, its queries never fire, and there is no flash of content to redirect away from. But every rule a guard enforces is enforced again by the server on every request, because anything running in a browser can be bypassed with devtools. Guards exist to give an honest user a correct, non-flickering UI, not to keep anyone out. The real boundary is the backend's authorization matrix.

**Server state lives in one place, cached across routes.** TanStack Query owns everything that comes from the API. The session in particular (`GET /api/auth/me`) is cached and reused, so a guard checking who you are on one route and a guard checking again on the next route share a single request rather than firing two. Navigating between two protected routes costs no network round trip.

**Zod validates the contract at the boundary.** Every response is parsed against a schema in `api/schemas.ts` before the rest of the app sees it. A backend contract change surfaces as a clear schema error at the edge instead of a mystery `undefined` three components deep. A schema mismatch is treated as a bug, distinct from an ordinary API error, because it means the frontend and backend disagree about the shape of the data.

**One fetch wrapper knows the base path and the cookie.** Every request goes through `api/client.ts`, which is the single place that knows the `/api` prefix, sends credentials, and understands the error envelope. There is no second fetch call somewhere that could forget to attach the cookie.

### The generated route tree

`src/routeTree.gen.ts` is generated from the route files and is not committed. Two things write it: the TanStack Router Vite plugin while the dev server runs, and the `tsr generate` one-shot (the `routes` npm script). The `build` and `typecheck` scripts both run `tsr generate` first, because `tsc` would otherwise fail on a fresh clone against an import from `main.tsx` that does not exist yet. Do not hand-edit that file; it is regenerated on every build and any change you make to it will be overwritten.

## How a booking request travels

To make the layering concrete, here is what happens when a member books a room:

1. The member submits the booking dialog. A TanStack Query mutation in `api/hooks.ts` calls `api.post('/bookings', ...)` in `api/client.ts`, which attaches the session cookie and posts JSON to the backend.
2. The request hits the middleware stack: a request id is attached, a panic recoverer wraps everything, the request is logged, and the body size is capped. Then `RequireAuth` validates the session cookie's JWT and rejects the request with a 401 if it is missing or expired.
3. The bookings handler decodes and validates the request shape, then calls `BookingService.Create`.
4. The service validates the business rules (duration bounds, not in the past, attendee count within capacity) and then opens one transaction. Inside it: check for an idempotency-key replay, take an advisory lock on the room, release any stale no-shows for that room, and insert. There is deliberately no availability check first; the database's exclusion constraint is the authority. [Concurrency](concurrency.md) explains every step of that transaction and why it is in that order.
5. The insert either commits, and the booking is returned, or it trips the overlap constraint and comes back as SQLSTATE `23P01`, which the store recognizes and the handler turns into a 409.
6. The response flows back out through the same layers. The client parses it against a Zod schema, Query updates its cache, and the UI reflects the new booking without a full refetch.

Every step in that chain is a layer doing only its own job, which is what makes each one testable on its own. How that testing is arranged is covered in [Testing](testing.md).

## Sessions in brief

Sessions are stateless JWTs carried in an httpOnly cookie, so JavaScript can never read the token. The backend validates the JWT on every request. There is no server-side session store and no revocation: a token stays valid until it expires, one hour by default. The trade-off and the reasoning are in [Configuration](configuration.md#sessions-and-tokens).

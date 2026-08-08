<p align="center">
  <img src="frontend/public/images/atrium-logo.png" alt="Atrium logo" width="280" />
</p>

# Atrium

A room booking system for a co-working space. Go and PostgreSQL on the back, React and TypeScript on the front.

The hard part of a booking system isn't the CRUD, it's what happens when two people reach for the same room at the same second. Atrium settles that in the database with a PostgreSQL exclusion constraint instead of application-level locking, and most of the design falls out of that one choice.

## Quick start

```bash
docker compose up
```

That's the whole setup. You don't need an `.env` file, since every variable ships with a working development default.

| | |
|---|---|
| Web | http://localhost:5173 |
| API | http://localhost:8080/api |

Services start in dependency order, gated by health checks rather than `sleep`: `postgres` (healthy) then `migrate` (exits 0) then `seed` (exits 0) then `api` (healthy) then `web`. Nothing comes up against a database that isn't ready, and the API never starts against an unmigrated schema.

### Signing in

Seeded accounts:

| Role | Email | Password |
|---|---|---|
| Admin | `admin@atrium.local` | `admin123` |
| Member | `member@atrium.local` | `member123` |

The compose stack also enables `POST /api/auth/demo-login`, a one-click sign-in for reviewers. It's off by default in the application and turned on only here, because compose is a dev environment. It hands out a valid session with no password, so it must never run in production. When disabled it isn't mounted at all, so there's no flag inside a handler for someone to forget.

## Documentation

This README is the overview. The full documentation lives in the [`docs/`](docs/) folder, with one page per topic:

| Page | What it covers |
|---|---|
| [Getting started](docs/getting-started.md) | Prerequisites, bringing the stack up, seeded accounts, and running the backend or frontend on their own. |
| [Architecture](docs/architecture.md) | The layers on both sides and how a request travels from a click to a row and back. |
| [Concurrency](docs/concurrency.md) | Why two people cannot book the same room at the same second, and why that guarantee lives in the database. Start here if you read only one. |
| [Database](docs/database.md) | The schema, the constraints that enforce the rules, the indexes, and how time is represented. |
| [API reference](docs/api-reference.md) | Every endpoint, who can reach it, and the single error shape the whole API returns. |
| [Configuration](docs/configuration.md) | Every environment variable and booking policy constant, with the reasoning behind the ones that have no safe default. |
| [Testing](docs/testing.md) | How the suites are structured, why the integration tests need a real database, and the one test that carries the most weight. |
| [Deployment](docs/deployment.md) | The Render blueprint, running migrations and seeding against a hosted database, and the free-tier caveats. |

[`docs/README.md`](docs/README.md) is the same index with a suggested reading order.

## Tech stack

The brief asked for Go, React + Vite + TypeScript, PostgreSQL, and Docker Compose. Everything beyond that earns its place:

- **chi** (router) and **pgx** (Postgres driver): both are thin. chi is `net/http` with middleware, and pgx talks to Postgres directly with no ORM, so the SQL in this repo is the SQL that runs.
- **argon2id** for password hashing: a memory-hard algorithm with a per-hash salt, rather than rolling my own.
- **TanStack Router + Query**: the router runs auth guards in `beforeLoad`, so a protected page never mounts for a user who can't see it. Query owns all server state and caches the session across route changes.
- **Zod**: validates every API response against a schema at the boundary, so a backend contract change surfaces as a clear error instead of a mystery `undefined` three components deep.
- **Tailwind**: styling without a second naming system to maintain.

## The core idea

The most important lines in the repo live in `backend/migrations/000001_init.up.sql`:

```sql
ALTER TABLE bookings
    ADD CONSTRAINT bookings_no_overlap
    EXCLUDE USING gist (
        room_id WITH =,
        tstzrange(start_time, end_time, '[)') WITH &&
    )
    WHERE (status = 'confirmed');
```

Two confirmed bookings for the same room with overlapping times can't exist. Not "are unlikely", not "are guarded against". They can't exist. Three things follow from that:

**No check-then-insert.** The service never asks whether a room is free before booking it. It inserts, and the constraint accepts or rejects. A `SELECT` first would be a race window that no amount of care in Go can close, because two requests can both read "free" before either writes. A rejection comes back as SQLSTATE `23P01` and becomes a `409`.

**No-show release is lazy, with no scheduler.** A booking nobody checks into within the 15 minute grace period flips to `status = 'released'`, which drops it out of the partial index behind the constraint. That flip runs inside the booking transaction for the room being booked, so a stale slot is freed and refilled atomically. No cron job, no sweep, no second mechanism to drift out of sync with the first.

**The database clock decides state changes.** Check-in and release put their whole condition in SQL against `now()`, inside the `UPDATE`'s `WHERE` clause, so the check and the write can't be split by a concurrent writer. Go's `time.Now()` is used only for friendly pre-flight validation (a `422`), never as the authority over stored state.

### Ordering, not deciding

One extra piece makes the constraint behave under real contention, and it's worth being exact about what it does.

Postgres checks an exclusion constraint by inserting the index entry first and scanning for conflicts second. When several transactions all insert before any of them scans, each finds the others in progress and waits on their transaction ids: a cycle with no winner that only the deadlock detector can break, one victim per `deadlock_timeout`. Measured on this schema with 8 requests racing for one slot:

| | Result | Time |
|---|---|---|
| Without the room lock | 1 success, 7 x `40P01` deadlock | 7.9s |
| With the room lock | 1 success, 7 x `23P01` conflict | 0.13s |

Both store exactly one booking. The invariant held either way. What changed was the answer the seven losers got: an arbitrary deadlock surfaced as a `500`, versus an honest "that slot is taken".

So `store.LockRoom` takes a transaction-scoped advisory lock on the room. It reads no bookings and decides nothing. Remove it and every booking outcome is identical, because the constraint is still the only thing that accepts or rejects a row. It just orders arrivals. That's also why it isn't the `SELECT ... FOR UPDATE` the design avoids: that would hold a lock across an availability read, scale with the rows examined, and have nothing to lock in the case that matters, since a free slot has no row. This lock covers one `INSERT` and one narrow `UPDATE`, both sub-millisecond, and different rooms never contend.

## Database schema

Three tables: `users`, `rooms`, and `bookings`.

<p align="center">
  <img src="frontend/public/images/db_schema.png" alt="Atrium database schema" width="640" />
</p>

A few decisions the diagram can't show:

- **`bookings_no_overlap`** is the exclusion constraint above. It's the reason the schema exists.
- **`amenities` is a `text[]`, not a join table.** At this scale it's a tag set, only ever filtered as "room has all of these", and a GIN index makes that fast. If amenities ever grew their own attributes (an icon, a per-room count) a normalized table would win. They don't, so it stays an array.
- **`room_id` is `ON DELETE RESTRICT`.** Deleting a room shouldn't silently erase the bookings people are counting on. The admin delete path checks for future confirmed bookings and returns `409` listing them; RESTRICT is the backstop.
- **Status timestamps are checked against status.** A `cancelled` row must have `cancelled_at`, a `released` row must have `released_at` and no check-in. An inconsistent row can't be written even if future code forgets to set one.

## Architecture

```
cmd/server  ->  http/  ->  service/  ->  store/  ->  PostgreSQL
                  |
             middleware, JWT + cookies
```

| Layer | Responsibility |
|---|---|
| `cmd/server` | Entrypoint: config, DB connection, router assembly |
| `http` | Handlers, middleware, the authorization matrix |
| `service` | Business rules and validation |
| `store` | SQL, one store per table, pgx directly, no ORM |
| `auth` | JWT signing, password hashing, cookie management |
| `config` | Environment read once at startup, fails fast |

The frontend mirrors it:

```
main.tsx  ->  routes/  ->  lib/guards.ts (beforeLoad)  ->  api/hooks.ts  ->  api/client.ts
```

Route guards run in `beforeLoad`, so a protected component never mounts for a user who can't see it: no flash of content, no queries fired that the user can't complete. They are not a security boundary. Every rule they enforce is enforced again server-side on every request, because anything in a browser can be bypassed with devtools. They exist to give an honest user a correct, non-flickering UI.

### API surface

The whole authorization model lives in one function, `http/router.go:NewRouter`, so "can a member reach this?" never means reading every handler.

| Access | Endpoints |
|---|---|
| Public | `POST /api/auth/{register,login,logout}`, `GET /api/healthz` |
| Member | `GET /api/auth/me`, `GET /api/rooms`, `GET /api/rooms/{id}/availability`, `POST /api/bookings`, `GET /api/bookings/me`, `POST /api/bookings/{id}/check-in`, `DELETE /api/bookings/{id}` |
| Admin | `POST\|PATCH\|DELETE /api/rooms{/id}`, `GET /api/bookings`, `GET /api/admin/utilization` |

`GET /api/rooms` is a member route while `POST /api/rooms` is admin: same path, different method, different authority.

### Error contract

Every failure returns one envelope, from a single function:

```json
{ "error": { "code": "...", "message": "...", "fields": { } } }
```

Handlers return wrapped domain sentinels and never pick a status code themselves, so the mapping can't drift: `ErrValidation` to 422, `ErrUnauthorized` to 401, `ErrForbidden` to 403, `ErrNotFound` to 404, `ErrConflict` to 409, anything else to 500. The bodies of 5xx responses are replaced with a generic message and the real error is logged, because internal errors routinely carry SQL and connection strings.

Two choices look like bugs until you know why:

- Cancelling or checking into someone else's booking returns `404`, not `403`. Telling a stranger a booking exists but isn't theirs leaks its existence.
- A malformed UUID in a path returns `404`, not `422`, so a caller can't tell "invalid" from "not yours" and use the difference to enumerate.

### Other decisions worth knowing

**Time.** All `timestamptz`, all UTC. RFC 3339 with an explicit offset is the only accepted input. A naive `2026-01-02T09:00` is rejected rather than guessed at, because getting the zone wrong by an hour is the most common booking bug there is. Intervals are half-open `[start, end)`, so a meeting ending at 11:00 and one starting at 11:00 don't overlap and back-to-back bookings are legal.

**Sessions** are stateless JWTs in httpOnly cookies, never readable from JavaScript. There is no revocation: a stolen token stays valid until it expires. Revoking a stateless token needs a revocation list, which is state, and this design chose statelessness. The mitigation is a short TTL, 1 hour by default.

**Idempotency.** `POST /api/bookings` accepts an `Idempotency-Key` header. Replaying a key returns the original booking with `200` instead of double-booking. Keys are scoped per user, and the check runs inside the booking transaction, so a concurrent replay retries the lookup rather than being misreported as a conflict.

**Pagination** uses opaque base64 keyset cursors over `(start_time, id)`, not `OFFSET`. With `OFFSET`, a booking created while you page shifts every later row and silently skips one.

**Request parsing** collects one error per field and keeps going instead of bailing on the first bad value, so a request with three malformed parameters gets all three back at once instead of costing three round trips.

## Development

### Backend

```bash
cd backend

go run ./cmd/server          # requires DATABASE_URL, JWT_SECRET
go build ./... && go vet ./...
go run ./cmd/seed
```

### Frontend

```bash
cd frontend
npm install                  # node_modules is not checked in

npm run dev                  # dev server, proxies /api to the backend
npm run build                # route tree + tsc + production build
npm run typecheck
npm run lint                 # --max-warnings 0
npm test
```

`src/routeTree.gen.ts` is generated and not committed. `build` and `typecheck` regenerate it first, so a fresh clone works. Don't hand-edit it.

### Database access

```bash
psql postgres://atrium:atrium_dev_pass@localhost:5432/atrium
```

## Tests

```bash
cd backend
go test ./...                # unit tests only
```

A green run there does not mean the booking invariant was exercised. Integration tests skip silently without a database. To actually test the constraint:

```bash
createdb -h localhost -U atrium atrium_test
export TEST_DATABASE_URL='postgres://atrium:atrium_dev_pass@localhost:5432/atrium_test?sslmode=disable'
go test ./... -v

# just the one that matters
go test ./internal/service -run TestCreate_ConcurrentSameSlot -v
```

That silent skip flips when `CI` is set: a missing `TEST_DATABASE_URL` becomes a hard failure, because a suite that quietly skips its most important test in CI is worse than no suite.

Integration tests run against real Postgres with the real migrations applied, on purpose. The exclusion constraint is the enforcement mechanism, so a fake store would only test the fake. They share one database and reset between tests, which is why they must not call `t.Parallel`.

`TestCreate_ConcurrentSameSlot` is the load-bearing test. Eight distinct users are released from a `sync.WaitGroup` barrier onto the same slot, a barrier rather than a loop, because a loop lets the first request commit before the last one spawns and would pass against code with no concurrency safety at all. Exactly one insert must win, and the test checks both what the service reported and what the database actually stored, since a service that returns one success while writing two rows would satisfy only the first.

Frontend tests are Vitest under jsdom, with the boundary mocked at HTTP via MSW rather than by stubbing modules, so `client.ts` runs its real request, status handling and Zod parse. Stubbing the client instead would leave the error envelope and every schema untested.

```bash
cd frontend
npm test
npx vitest run src/api/client.test.ts     # one file
npx vitest run -t 'returns the cached session'
```

For the full picture, how the suites are structured, why the integration tests refuse to fake the database, and why `TestCreate_ConcurrentSameSlot` is the load-bearing test, see [docs/testing.md](docs/testing.md).

## Configuration

Every variable has a working default in `docker-compose.yml` or `config/config.go`. See `.env.example` for the full list and copy it to `.env` only to override something. Two have no safe default:

- **`JWT_SECRET`**: the server refuses to start if it's missing or shorter than 32 bytes. A default secret that works locally is exactly how you reach production with a known key. Generate one with `openssl rand -base64 32`.
- **`DEMO_LOGIN_ENABLED`**: must be off outside development.

Booking policy lives in `config/config.go` as constants, not environment variables, because they're product decisions rather than deployment ones. A room that releases after 15 minutes in staging and 5 in production would make the no-show behavior untestable.

| Constant | Value |
|---|---|
| `MinBookingDuration` | 15 minutes |
| `MaxBookingDuration` | 8 hours |
| `CheckInWindowBefore` | 5 minutes |
| `CheckInGracePeriod` | 15 minutes |
| `MaxBookingHorizon` | 90 days |

Only the duration bounds are mirrored in SQL, where the database is the enforcement point. The Go constants exist so the API can return a helpful `422` instead of a raw constraint name.

## Deployment

There's a `render.yaml` blueprint that stands up three things on Render's free tier: the Go API (Docker), the SPA (static site), and Postgres. The one rule the whole topology is built around is same-origin. The session cookie is `__Host-` prefixed and `SameSite=Lax`, so a browser won't send it on a cross-site `fetch`. The static site owns the public URL and rewrites `/api/*` to the API service server-side, so everything is one origin to the browser, exactly like the Vite proxy does locally.

Two things the blueprint can't do for you, both because the API image is distroless and has no shell or migrate tool:

1. **Migrations** run from your machine against the database's external URL:

   ```bash
   docker run --rm -v "$(pwd)/backend/migrations:/migrations" \
     migrate/migrate:v4.17.1 \
     -path=/migrations -database "$EXTERNAL_DATABASE_URL&sslmode=require" up
   ```

2. **Seeding** is the same seed binary used locally, pointed at the external URL:

   ```bash
   cd backend
   DATABASE_URL="$EXTERNAL_DATABASE_URL" JWT_SECRET="$(openssl rand -base64 32)" go run ./cmd/seed
   ```

   `JWT_SECRET` here is a throwaway. The seed binary calls `config.Load`, which insists on one, but never signs anything with it.

Free-tier caveats worth knowing before you rely on it: the API sleeps after about 15 minutes idle and takes a few seconds to wake on the next request, and a free Postgres instance is removed after about 30 days. Fine for a review deployment, not for anything real.

## Assumptions

A few things I decided rather than asked:

- One booking holds one room for one continuous block. No recurring bookings, no multi-room events.
- Anyone who registers is a `member`. Admins are promoted in the database, since self-service admin signup isn't something a real space would want.
- Capacity is advisory. A room with capacity 8 will take a booking for 10 attendees; the number is shown to help people pick, not to block them.
- A single co-working location, so rooms have no building or floor dimension yet.

## How I used AI

I used AI tools as an assistant, not an autopilot. The design decisions, the tradeoffs, and the code are mine to explain.

- **Claude Code (Anthropic)** did most of the mechanical work: scaffolding handlers and stores, wiring the TanStack routes, writing the first pass of tests, and drafting this README. It's fast at the parts that are typing rather than thinking.
- **Where I drove and it followed:** the concurrency model is the clearest example. I chose the exclusion constraint over application locking, then found the deadlock behavior under real contention, benchmarked it (the table in "Ordering, not deciding" is from those runs), and settled on the advisory lock as ordering rather than a `SELECT ... FOR UPDATE`. AI helped me write and measure the concurrency test; it didn't hand me the answer.
- **Where I pushed back on it:** the first drafts reached for a background sweep to release no-shows and a check-then-insert in the service. Both are exactly the mistakes this design avoids, so I rejected them and moved the logic into the booking transaction.
- **Reviewing its output:** I read everything it produced. The Zod boundary, the error envelope, and the information-hiding choices (404 over 403, 404 over 422) came out of that review, where the goal was a contract I could defend line by line.

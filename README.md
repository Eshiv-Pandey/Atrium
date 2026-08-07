# Atrium

A room booking system. Go + PostgreSQL on the back, React + TypeScript on the front.

The interesting problem in a booking system is not CRUD, it is what happens when two people
want the same room at the same moment. Atrium answers that with a PostgreSQL exclusion
constraint rather than with application-level locking, and most of the design follows from
that one decision.

---

## Quick start

```bash
docker compose up
```

That is the whole setup. No `.env` file is needed — every variable has a working
development default.

| | |
|---|---|
| Web | http://localhost:5173 |
| API | http://localhost:8080/api |

Services come up in dependency order, enforced by health checks rather than by sleeping:
`postgres` (healthy) → `migrate` (exits 0) → `seed` (exits 0) → `api` (healthy) → `web`.
Nothing starts against a database that is not ready, and the API never starts against an
unmigrated schema.

### Signing in

The seeded accounts:

| Role | Email | Password |
|---|---|---|
| Admin | `admin@atrium.local` | `admin123` |
| Member | `member@atrium.local` | `member123` |

The compose stack also enables `POST /api/auth/demo-login`, a one-click sign-in for
reviewers. It is **off by default in the application** and turned on only here, because
compose is a development environment. It is an unauthenticated route that hands out a valid
session, so it must never be enabled in production — and when disabled it is not mounted at
all, rather than guarded by a flag inside a handler that someone could forget.

---

## The core invariant

The most important lines in this repository are in `backend/migrations/000001_init.up.sql`:

```sql
ALTER TABLE bookings
    ADD CONSTRAINT bookings_no_overlap
    EXCLUDE USING gist (
        room_id WITH =,
        tstzrange(start_time, end_time, '[)') WITH &&
    )
    WHERE (status = 'confirmed');
```

Two confirmed bookings for the same room with overlapping times are **impossible at the
database level**. Not unlikely, not guarded against — impossible.

Three consequences shape the rest of the code:

**There is no check-then-insert.** The service layer never asks whether a room is free
before booking it. It inserts, and the constraint accepts or rejects. This is not a
shortcut: a `SELECT` beforehand is a race window that no amount of care in Go can close,
because two requests can both read "free" before either writes. A conflict surfaces as
SQLSTATE `23P01` and becomes a `409`.

**No-show release is lazy and has no scheduler.** A booking nobody checks into within the
15-minute grace period flips to `status = 'released'`, which drops it out of the partial
index backing the constraint. That release runs *inside* the booking transaction for the
room being booked, so a stale booking is freed and immediately replaced atomically. No cron
job, no sweep process, no second mechanism to drift out of sync with the first.

**The database clock decides state transitions.** Check-in and release express their whole
condition in SQL against `now()`, inside the `UPDATE`'s `WHERE` clause, so the check and the
write cannot be separated by a concurrent writer. Go's `time.Now()` is used only for
advisory validation that produces a friendly `422`, never as authority over stored state.

### Ordering versus deciding

One addition is needed to make that constraint behave well under real contention, and it is
worth being precise about what it does and does not do.

Postgres validates an exclusion constraint by inserting the index entry *first* and scanning
for conflicts *second*. Transactions that all insert before any of them scans therefore each
find the others in progress and wait on the others' transaction ids — a cycle with no
winner, which only the deadlock detector can break, one victim per `deadlock_timeout`.
Measured against this schema with 8 requests racing for one slot:

| | Result | Time |
|---|---|---|
| Without the room lock | 1 success, 7 × `40P01` deadlock | 7.9s |
| With the room lock | 1 success, 7 × `23P01` conflict | 0.13s |

Both store exactly one booking. **The invariant held either way** — what differed was the
answer the seven losers received: an arbitrary deadlock, surfaced as a `500`, instead of the
honest "that slot is taken".

So `store.LockRoom` takes a transaction-scoped advisory lock on the room. It reads no
bookings and decides nothing; remove it and every booking *outcome* is identical, because
the constraint remains the only thing that accepts or rejects a row. It orders arrivals.
The distinction matters, and it is why this is not the `SELECT ... FOR UPDATE` that the
design deliberately avoids: that would hold a lock across an availability *read*, scaling
with the rows examined, and would have nothing to lock in the case that matters, since a
free slot has no row. This lock covers one `INSERT` and one narrow `UPDATE`, both
sub-millisecond, and different rooms never contend.

---

## Architecture

```
cmd/server  →  http/  →  service/  →  store/  →  PostgreSQL
                 ↓
            middleware, JWT + cookies
```

| Layer | Responsibility |
|---|---|
| `cmd/server` | Entrypoint: config, DB connection, router assembly |
| `http` | Handlers, middleware, the authorization matrix |
| `service` | Business rules and validation |
| `store` | SQL. One store per table, `pgx` directly, no ORM |
| `auth` | JWT signing, password hashing, cookie management |
| `config` | Environment read once at startup; fails fast |

The frontend mirrors it:

```
main.tsx  →  routes/  →  lib/guards.ts (beforeLoad)  →  api/hooks.ts  →  api/client.ts
```

Route guards run in `beforeLoad`, so a protected component never mounts for a user who
cannot see it — no flash of content, no queries fired that the user cannot complete. They
are **not a security boundary**: every rule they enforce is enforced again server-side on
every request, because anything in a browser can be bypassed with devtools. They exist to
give an honest user a correct, non-flickering UI.

### API surface

The entire authorization model lives in one function, `http/router.go:NewRouter`, so
"can a member reach this?" never requires reading every handler.

| Access | Endpoints |
|---|---|
| Public | `POST /api/auth/{register,login,logout}`, `GET /api/healthz` |
| Member | `GET /api/auth/me`, `GET /api/rooms`, `GET /api/rooms/{id}/availability`, `POST /api/bookings`, `GET /api/bookings/me`, `POST /api/bookings/{id}/check-in`, `DELETE /api/bookings/{id}` |
| Admin | `POST\|PATCH\|DELETE /api/rooms{/id}`, `GET /api/bookings`, `GET /api/admin/utilization` |

`GET /api/rooms` is a member route while `POST /api/rooms` is admin: same path, different
method, different authority.

### Error contract

Every failure returns one envelope, produced by a single function:

```json
{ "error": { "code": "...", "message": "...", "fields": { } } }
```

Handlers return wrapped domain sentinels and never choose a status code themselves, so the
mapping cannot drift: `ErrValidation`→422, `ErrUnauthorized`→401, `ErrForbidden`→403,
`ErrNotFound`→404, `ErrConflict`→409, anything else→500. Bodies of 5xx responses are
replaced with a generic message and the real error is logged, because internal errors
routinely carry SQL and connection strings.

Two information-hiding choices are deliberate and look like bugs if you do not know why:

- Cancelling or checking into **someone else's** booking returns `404`, not `403`. Telling
  a stranger that a booking exists but is not theirs leaks its existence.
- A **malformed UUID** in a path returns `404`, not `422`, so a caller cannot distinguish
  "invalid" from "not yours" and use the difference to enumerate.

### Other decisions worth knowing

**Time.** All `timestamptz`, all UTC. RFC 3339 with an explicit offset is the only accepted
input — a naive `2026-01-02T09:00` is rejected rather than guessed at, because guessing the
zone wrong by an hour is the most common booking bug there is. Intervals are half-open
`[start, end)`, so a meeting ending at 11:00 and one starting at 11:00 do not overlap and
back-to-back reservations are legal.

**Sessions** are stateless JWTs in httpOnly cookies, never readable from JavaScript. There
is **no revocation**: a stolen token stays valid until it expires. Revoking a stateless
token requires a revocation list, which is state, and this design chose statelessness. The
mitigation is a short TTL (1 hour default).

**Idempotency.** `POST /api/bookings` accepts an `Idempotency-Key` header; replaying a key
returns the original booking with `200` instead of double-booking. Keys are scoped per user.
The check happens inside the booking transaction, so a concurrent replay retries the lookup
rather than being misreported as a conflict.

**Pagination** uses opaque base64 keyset cursors over `(start_time, id)`, not `OFFSET`. With
`OFFSET`, a booking created while the user pages shifts every subsequent row and silently
skips one.

**Request parsing** accumulates one error per field and keeps going rather than returning at
the first bad value, so a request with three malformed parameters gets all three back at
once instead of costing three round trips.

---

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

`src/routeTree.gen.ts` is generated and not committed. `build` and `typecheck` regenerate it
first, so a fresh clone works; do not hand-edit it.

### Database access

```bash
psql postgres://atrium:atrium_dev_pass@localhost:5432/atrium
```

---

## Tests

```bash
cd backend
go test ./...                # unit tests only
```

**A green run there does not mean the booking invariant was exercised.** Integration tests
skip silently without a database. To actually test the constraint:

```bash
createdb -h localhost -U atrium atrium_test
export TEST_DATABASE_URL='postgres://atrium:atrium_dev_pass@localhost:5432/atrium_test?sslmode=disable'
go test ./... -v

# just the one that matters
go test ./internal/service -run TestCreate_ConcurrentSameSlot -v
```

That silent skip inverts when `CI` is set: a missing `TEST_DATABASE_URL` becomes a hard
failure, because a suite that quietly skips its most important test in CI is worse than no
suite at all.

Integration tests run against real Postgres with the real migrations applied, deliberately:
the exclusion constraint *is* the enforcement mechanism, so a fake store would only test the
fake. They share one database and reset between tests, which is why they must not call
`t.Parallel`.

`TestCreate_ConcurrentSameSlot` is the load-bearing test. Eight distinct users are released
from a `sync.WaitGroup` barrier onto the identical slot — a barrier rather than a loop,
because a loop lets the first request commit before the last one spawns and would pass
against code with no concurrency safety whatsoever. Exactly one insert must win, and the
test asserts both what the service reported *and* what the database actually stored, since a
service returning one success while writing two rows would satisfy the former alone.

Frontend tests are Vitest under jsdom, with the boundary mocked at HTTP via MSW rather than
by stubbing modules — so `client.ts` runs its real request, status handling and Zod parse.
Stubbing the client instead would leave the error envelope and every schema untested.

```bash
cd frontend
npm test
npx vitest run src/api/client.test.ts     # one file
npx vitest run -t 'returns the cached session'
```

---

## Configuration

Every variable has a working default in `docker-compose.yml` or `config/config.go`; see
`.env.example` for the full list. Copy it to `.env` only to override something.

Two have no safe default:

- **`JWT_SECRET`** — the server refuses to start if it is missing or shorter than 32 bytes.
  A default secret that works locally is exactly how one reaches production with a known
  key. Generate one with `openssl rand -base64 32`.
- **`DEMO_LOGIN_ENABLED`** — must be off outside development.

Booking policy lives in `config/config.go` as constants rather than environment variables,
because they are product decisions and not deployment ones — a room that releases after 15
minutes in staging and 5 in production would make the no-show behaviour untestable.

| Constant | Value |
|---|---|
| `MinBookingDuration` | 15 minutes |
| `MaxBookingDuration` | 8 hours |
| `CheckInWindowBefore` | 5 minutes |
| `CheckInGracePeriod` | 15 minutes |
| `MaxBookingHorizon` | 90 days |

Only the duration bounds are mirrored in SQL, where the database is the enforcement point;
the Go constants exist so the API can return a helpful `422` instead of a constraint name.

---

## Deploying

- Set `SECURE_COOKIES=true`. It enables the `Secure` flag and the `__Host-` cookie prefix,
  both of which require HTTPS.
- Generate a real `JWT_SECRET`.
- Turn `DEMO_LOGIN_ENABLED` off.
- Build the frontend with `npm run build` and serve `frontend/dist` as static files, with
  `/api` proxied to the backend so both are same-origin. The session cookie then needs no
  cross-site exemption, and the frontend never learns where the API is hosted.

The API image is distroless and contains no shell, so its health check is the server binary
probing its own `/api/healthz` endpoint.

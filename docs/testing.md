# Testing

Atrium has tests on both sides, and they are structured around one belief: a test is only worth writing if it can fail for the reason you care about. That belief shapes several choices that look unusual at first, like integration tests that need a real database and refuse to fake it, a frontend suite that mocks HTTP rather than modules, and one concurrency test that the whole backend suite effectively rests on. This page explains how to run everything and why it is arranged the way it is.

## Quick reference

```bash
# Backend, unit tests only (integration tests skip without a database)
cd backend
go test ./...

# Backend, unit + integration (this is the run that tests the invariant)
export TEST_DATABASE_URL='postgres://atrium:atrium_dev_pass@localhost:5432/atrium_test?sslmode=disable'
go test ./... -v

# Backend, a single test
go test ./internal/service -run TestCreate_ConcurrentSameSlot -v

# Frontend
cd frontend
npm test                                   # vitest run
npm run test:watch                         # watch mode
npx vitest run src/api/client.test.ts      # one file
npx vitest run -t 'returns the cached session'   # one test by name
```

## Backend: unit versus integration

The backend has two kinds of test, and the difference matters.

Unit tests need nothing external. They cover the pure logic: interval math, validation rules, the check-in failure reasons, request parsing, the error mapping. `go test ./...` runs them and they are fast.

Integration tests run against a real PostgreSQL with the real migrations applied. This is deliberate and not negotiable, because the rule the whole system is built around (two overlapping confirmed bookings for one room cannot exist) is enforced by a Postgres exclusion constraint, not by Go. A fake store would exercise the fake and prove nothing about the invariant. So the tests that matter connect to an actual database and let the actual constraint accept or reject rows.

**A green `go test ./...` does not mean the invariant was exercised.** Without `TEST_DATABASE_URL` set, the integration tests skip silently, so a passing run on a machine with no database has not tested the one thing this project is about. To actually run them, point them at a scratch database:

```bash
docker compose up -d postgres
createdb -h localhost -U atrium atrium_test        # once
export TEST_DATABASE_URL='postgres://atrium:atrium_dev_pass@localhost:5432/atrium_test?sslmode=disable'
go test ./... -v
```

That scratch database is fair game for destruction: the test helper drops its schema on the first call and truncates every table between tests, so it must not be a database anyone cares about.

**The silent skip flips to a hard failure in CI.** When the `CI` environment variable is set, a missing `TEST_DATABASE_URL` is an error rather than a skip. The reasoning is simple: letting a contributor without a database still get the unit tests is friendly, but a suite that silently skips its most important test in the one place that is supposed to guarantee correctness is worse than no suite at all. So locally it skips, and in CI it insists.

### How the integration harness works

`testutil.DB(t)` returns a connection to `TEST_DATABASE_URL` with migrations already applied and every table empty. The connection pool is shared across the whole test binary, because connecting is the slow part, while the per-test reset is just a truncate that takes a few milliseconds.

The direct consequence is that **integration tests must not call `t.Parallel`**. They share one database, and parallel tests would see each other's rows. This is the single most important rule when adding a backend test that touches the database.

The choice of an environment variable over something like testcontainers is intentional too. The Compose stack already stands up the exact Postgres this project targets, with the two extensions the migrations need, so pointing the tests at it costs one variable and no new dependencies. Testcontainers would add a large dependency tree to solve a problem the repo has already solved, and would still need a running Docker daemon anyway.

## The load-bearing test

`TestCreate_ConcurrentSameSlot` in `service/booking_concurrency_test.go` is the test the whole backend suite effectively rests on. It is worth understanding in detail, because every part of it is there for a reason.

It releases eight goroutines from a `sync.WaitGroup` barrier so they all attempt to book the same slot at the same instant, then it checks two separate facts:

- **Exactly one booking was stored.** This is the invariant. Nothing may paper over a failure here.
- **Exactly seven callers got a conflict.** This is the error contract: the seven losers must get an honest 409, not an arbitrary 500.

Read those two assertions separately when the test fails, because they fail for unrelated reasons. `stored != 1` means the invariant itself broke. `conflicts != 7` while `stored == 1` means the invariant held but the error contract broke, which is exactly what the deadlock behavior looked like before the room lock existed: one booking correctly stored, seven members told 500. [Concurrency](concurrency.md#the-subtle-piece-ordering-not-deciding) has the measurements behind that.

Three details of the setup are load-bearing:

- **A barrier, not a loop.** Starting the eight requests in a loop would let the first commit before the last one even spawns, so a loop would pass against code with no concurrency safety at all. The barrier forces genuine simultaneity, which is the only thing that actually tests the constraint under contention.
- **Eight distinct users.** Eight is below the connection pool's maximum of 10, so nothing serializes on the pool by accident. Distinct users matter because a shared user could make the test pass via the idempotency index firing instead of the overlap constraint, which would be the right answer for the wrong reason.
- **Both what the service reported and what the database stored are checked.** A service that returned one success while writing two rows would satisfy an assertion that only looked at the return value. The test reads the database directly to be sure.

A companion test, `TestCreate_ConcurrentDistinctRoomsAllSucceed`, guards the other direction: bookings for different rooms must all succeed in parallel. It would fail if the room lock's key were ever widened to a constant, which is why the concurrency page warns against doing that.

## Frontend: mock HTTP, not modules

The frontend suite runs under Vitest with jsdom. The configuration lives in `vite.config.ts` rather than a separate file, so the `@` alias and plugin list cannot drift from the ones the real build uses.

The defining choice is where the boundary is mocked. Requests are intercepted at the HTTP layer with MSW (Mock Service Worker), not by stubbing the client module. That means `api/client.ts` runs its real request, its real status handling, and its real Zod parse in every test. Stubbing the client module instead would skip exactly the parts of that file that are worth testing: the error envelope handling and the schema validation, which is most of what the file does. This is why the guidance is firm: mock the HTTP response with an MSW handler, never mock `api/client.ts`.

Three things in `src/test/setup.ts` are load-bearing, and each exists because of a specific failure it prevents:

- **`onUnhandledRequest: 'error'`.** An unmocked request would otherwise fail as a network error, and the assertion that follows would report on that network failure instead of the behavior under test. Making it an error means an undeclared request fails loudly and immediately.
- **`server.resetHandlers()` between tests.** A per-test handler override otherwise leaks forward, making one file's result depend on the order the others ran in. Resetting keeps each test independent.
- **A pinned jsdom URL of `http://localhost`.** The client fetches relative `/api/...` paths, so the origin they resolve against has to match what the handlers name. Leaving it to jsdom's default would break every handler at once with a confusing message the day the default changed.

### One trap worth knowing

The router resolves its first route match asynchronously, on a microtask. On the tick that `render` returns, the document is still empty, which means a test whose only assertions are negative (`queryBy(...).not.toBeInTheDocument()`) passes happily against a blank page without testing anything. The rule is: **never write a frontend test whose only assertions are negative.** When you are testing a tri-state (loading, empty, populated), assert one thing that must be present alongside the things that must be absent, so the test can actually fail.

The `renderWithRouter` helper in `src/test/router.tsx` handles this by being `async` and awaiting `router.load()`, so components that contain a `<Link>` (which throws without a router in context) render fully before assertions run.

## When you add a test

- Adding a backend test that touches the database? Use `testutil.DB(t)`, and do not call `t.Parallel`.
- Adding a frontend test that hits the API? Add an MSW handler in `src/test/server.ts` or override it per test with `server.use`. Do not stub the client.
- Either way, make sure the test can fail for the reason you intend, not for an incidental one. That is the whole philosophy of this suite.

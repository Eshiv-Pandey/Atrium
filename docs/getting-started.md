# Getting started

This page gets Atrium running on your machine and points you at the seeded accounts so you can sign in. The fastest path is Docker Compose, which brings up the whole stack with one command. If you would rather run the backend and frontend directly, that is covered further down.

## Prerequisites

For the Compose path you only need Docker with the Compose plugin. Everything else, Go, Node, Postgres, runs inside containers.

If you want to run the pieces on your host instead, you will need:

- Go 1.22 or newer for the backend.
- Node 20 or newer for the frontend.
- A PostgreSQL 16 instance you can reach, with the `pgcrypto` and `btree_gist` extensions available. The migrations create them, so a standard Postgres image is enough.

## The fast path: Docker Compose

```bash
docker compose up
```

That is the whole setup. You do not need a `.env` file, since every variable ships with a working development default baked into `docker-compose.yml`.

Once it is up:

| | |
|---|---|
| Web | http://localhost:5173 |
| API | http://localhost:8080/api |

The services start in dependency order, gated by health checks rather than by sleeping. Postgres comes up and reports healthy, then `migrate` applies the schema and exits, then `seed` loads the demo data and exits, then the `api` comes up healthy, and finally the `web` dev server starts. Nothing comes up against a database that is not ready, and the API never starts against an unmigrated schema. If you watch the logs you will see them start in exactly that sequence.

To stop the stack, press Ctrl-C. To stop it and throw away the database volume so the next start is completely fresh:

```bash
docker compose down -v
```

## Signing in

Seeding creates two accounts:

| Role | Email | Password |
|---|---|---|
| Admin | `admin@atrium.local` | `admin123` |
| Member | `member@atrium.local` | `member123` |

The member account can browse rooms, make bookings, and manage its own reservations. The admin account can additionally create and edit rooms, view every booking, and see utilization figures. The full split of who can do what is in the [API reference](api-reference.md).

The Compose stack also enables a one-click sign-in for reviewers at `POST /api/auth/demo-login`. It hands out a valid session with no password, which is exactly why it is turned on only in this development environment and must never run in production. When it is disabled the route is not mounted at all, so there is no flag inside a handler for anyone to forget. See [Configuration](configuration.md#demo-login) for the details.

## Running the pieces on their own

Sometimes you want faster feedback than a container rebuild, or you want to attach a debugger. Both sides run directly on your host.

### Backend

```bash
cd backend

# The server needs a database URL and a JWT secret. Point DATABASE_URL at a
# Postgres you can reach; the one from the Compose stack works if it is running.
go run ./cmd/server

# Build and vet before committing.
go build ./... && go vet ./...

# Load the demo data (idempotent, so running it twice is safe).
go run ./cmd/seed
```

Two variables have no default and the server refuses to start without them: `DATABASE_URL` and `JWT_SECRET`. The secret must be at least 32 bytes. If you leave either out, the process tells you exactly what is missing and stops, rather than failing later on the first request that needs it. [Configuration](configuration.md) has the full list.

### Frontend

```bash
cd frontend

npm install          # node_modules is not checked in

npm run dev          # dev server on :5173, proxies /api to the backend
npm run build        # regenerate route tree, type check, production build
npm run typecheck    # route tree + tsc --noEmit
npm run lint         # eslint, fails on any warning
```

The dev server proxies `/api` to the backend, so the browser only ever makes same-origin requests and the session cookie works without any cross-site exemption. If you are running the backend somewhere other than the default, set `VITE_API_PROXY_TARGET` before `npm run dev`.

One generated file to be aware of: `src/routeTree.gen.ts` is produced from the route files and is not committed. Both `build` and `typecheck` regenerate it first, so a fresh clone works, but if you ever import from it before running either command you will see a missing-module error. Do not hand-edit it; it is regenerated on every build. The [architecture page](architecture.md#frontend-layers) explains why it works this way.

## Talking to the database directly

While the Compose stack is running, Postgres is exposed on `5432`, so you can connect with `psql`:

```bash
psql postgres://atrium:atrium_dev_pass@localhost:5432/atrium
```

If port 5432 is already taken on your machine, set `POSTGRES_PORT` to something free in a `.env` file before starting the stack, and connect on that port instead.

## Next steps

- To understand how the code is organized, read [Architecture](architecture.md).
- To understand the one guarantee the whole system is built around, read [Concurrency](concurrency.md).
- To run the tests, read [Testing](testing.md).

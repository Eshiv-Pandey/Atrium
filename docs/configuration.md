# Configuration

Atrium reads its configuration from the environment once at startup, validates all of it eagerly, and fails immediately if anything is wrong. A misconfigured deployment stops at boot with a clear message rather than limping along and breaking on the first request that happens to need the missing value. This page lists every environment variable, calls out the two that have no safe default, and explains the booking policy constants that are deliberately not configurable at all.

## Environment variables

Every variable has a working development default in `docker-compose.yml` or `config/config.go`, so the stack runs with no `.env` file. Copy `.env.example` to `.env` only when you want to override something.

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | Port the API listens on. |
| `DATABASE_URL` | none (required) | Postgres connection string. |
| `JWT_SECRET` | none (required) | Signs session tokens. Must be at least 32 bytes. |
| `SECURE_COOKIES` | `false` | Sets the `Secure` flag and the `__Host-` cookie prefix. Both require HTTPS. |
| `DEMO_LOGIN_ENABLED` | follows `SECURE_COOKIES` | Exposes the passwordless demo-login route. |
| `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | `atrium` / `atrium` / `atrium_dev_pass` | Compose-only, used to create the dev database. |
| `POSTGRES_PORT` | `5432` | Host port for `psql` access to the Compose database. |
| `API_PORT` / `WEB_PORT` | `8080` / `5173` | Host ports the Compose stack publishes. |
| `VITE_API_PROXY_TARGET` | `http://api:8080` | Where the frontend dev server proxies `/api`. |

`config.Load` reports every problem it finds at once rather than only the first, so a deployment with three missing variables needs one fix, not three deploys. Booleans must parse as booleans; an unparseable `SECURE_COOKIES=maybe` is a startup error, not a silently-false value.

## The two with no safe default

Two variables are required and intentionally have no fallback, because a fallback would be a security problem hiding behind a working local environment.

### The JWT secret

The server refuses to start if `JWT_SECRET` is missing or shorter than 32 bytes. The length floor is not arbitrary: HMAC-SHA256 keys shorter than the 32-byte digest reduce the effective security of the signature. More importantly, there is no default value on purpose. A default secret that works locally is exactly how a project reaches production signing real sessions with a key that is printed in its own source. Generate a real one:

```bash
openssl rand -base64 32
```

The Compose stack sets a development value so a reviewer does not have to, but it is clearly labelled as dev-only and must never travel to a real deployment.

### Demo login

This exposes `POST /api/auth/demo-login`, which signs in as a seeded account with no password. That is a genuine convenience for reviewers and a genuine backdoor in production: it is an unauthenticated route that hands out a valid session.

Its default follows the security posture rather than a fixed value. It defaults on when cookies are not secured (local http development) and off once they are (`SECURE_COOKIES=true`, meaning a real deployment). So `go run ./cmd/server` gives a reviewer one-click sign-in with zero setup, while a production host does not expose an unauthenticated route just by forgetting a variable. Setting `DEMO_LOGIN_ENABLED` explicitly wins in either direction, which is how an HTTPS review deployment can deliberately opt back in.

When it is off, the route is not mounted at all. The absence is the enforcement; there is no in-handler flag check to get wrong. See the [API reference](api-reference.md#authorization).

## Sessions and tokens

Sessions are stateless JWTs in httpOnly cookies. The token's lifetime is one hour (`TokenTTL`, set in `config.Load`). There is no revocation: a stolen token stays valid until it expires.

That is a real trade-off, made on purpose. Revoking a stateless token requires a revocation list, which is server-side state, and this design chose statelessness so that validating a session is a signature check and nothing else, with no store to query. The mitigation for the missing revocation is the short TTL. If you lengthen `TokenTTL`, understand that you are lengthening exactly how long a stolen token remains usable, with no way to cut it short.

## Booking policy constants

The rules about how long a booking may run, how early you may check in, and how far ahead you may reserve are constants in `config/config.go`, not environment variables:

| Constant | Value | Meaning |
|---|---|---|
| `MinBookingDuration` | 15 minutes | Shortest booking. |
| `MaxBookingDuration` | 8 hours | Longest booking. |
| `CheckInWindowBefore` | 5 minutes | How early check-in opens before the start. |
| `CheckInGracePeriod` | 15 minutes | How long after the start a booking survives without a check-in before it is released. |
| `MaxBookingHorizon` | 90 days | How far ahead a room may be reserved. |

They are constants rather than variables for a specific reason: a room that releases after 15 minutes in staging and 5 in production would make the no-show behavior untestable. These are product decisions, not deployment ones, so they live in code where they are the same everywhere and where the service layer and the database can share one documented source of truth.

Only the two duration bounds are mirrored in SQL, as the `bookings_duration_within_bounds` check constraint. There the database is the enforcement point, and the Go constants exist so the API can return a helpful 422 ("bookings may run at most 8 hours") instead of surfacing a raw constraint name. The check-in window, grace period, and horizon are enforced only in Go and in the `WHERE` clauses of the check-in and release statements, so changing one of those three changes behavior with no schema change. See [Database](database.md#the-other-constraints) for which rules are enforced where.

## Deployment-time settings

For a real deployment, the short version is: set `SECURE_COOKIES=true`, generate a real `JWT_SECRET`, and leave `DEMO_LOGIN_ENABLED` off (which the `SECURE_COOKIES=true` default does for you unless you override it). [Deployment](deployment.md) walks through the full picture, including why the whole topology is built around same-origin.

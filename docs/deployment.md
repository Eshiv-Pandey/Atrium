# Deployment

Atrium ships with a `render.yaml` blueprint that stands up the whole system on Render's free tier: the Go API as a Docker service, the React SPA as a static site, and a managed Postgres. This page walks through that blueprint, the two steps you have to run by hand (and why), the settings that must change for a real deployment, and the free-tier caveats worth knowing before you rely on any of it.

## The one rule: same origin

The entire topology is built around a single rule: the browser must only ever make same-origin requests. The session cookie is `__Host-` prefixed and `SameSite=Lax`, which means a browser will not send it on a cross-site `fetch`. So the API and the SPA cannot live on two different origins as far as the browser is concerned.

The blueprint solves this the same way the Vite dev proxy does locally. The static site owns the public URL, and it rewrites `/api/*` to the API service server-side. To the browser everything is one origin, the cookie is sent on every request, and the frontend never learns where the API is actually hosted. If you deploy Atrium some other way, this is the property you must preserve: put the SPA and the API behind one origin, with `/api` routed to the backend.

## What the blueprint creates

`render.yaml` describes three resources:

- The **API**, built from `backend/Dockerfile`. The image is distroless, which matters for the two manual steps below.
- The **SPA**, built with `npm run build` and served as a static site, with the `/api/*` rewrite rule pointing at the API.
- A **Postgres** instance.

You point Render at the repository, it reads the blueprint, and it provisions all three. What it cannot do for you is anything that needs a shell or a migrate tool inside the API image, because a distroless image has neither. That is the reason the next two steps run from your machine.

## Migrations

The API image has no migrate tool, so migrations run from your machine against the database's external URL:

```bash
docker run --rm -v "$(pwd)/backend/migrations:/migrations" \
  migrate/migrate:v4.17.1 \
  -path=/migrations -database "$EXTERNAL_DATABASE_URL&sslmode=require" up
```

`EXTERNAL_DATABASE_URL` is the external connection string Render gives you for the Postgres instance. The `sslmode=require` is appended because a hosted database expects TLS. This is the same `migrate` tool and the same migration files the Compose stack runs locally, just pointed at the hosted database instead of the local one.

## Seeding

Seeding is the same seed binary used locally, pointed at the external URL:

```bash
cd backend
DATABASE_URL="$EXTERNAL_DATABASE_URL" JWT_SECRET="$(openssl rand -base64 32)" go run ./cmd/seed
```

The `JWT_SECRET` here is a throwaway. The seed binary calls `config.Load`, which insists on a secret being present, but the seeder never signs anything with it, so any 32-byte value satisfies the check. This is not the secret your running API uses; that one is set on the API service itself (see below).

## Settings that must change for production

The development defaults are wrong for a real deployment, on purpose, so that forgetting to change one fails safe rather than silently shipping a dev setting. Set these on the API service:

- **`SECURE_COOKIES=true`.** This enables the `Secure` flag and the `__Host-` cookie prefix, both of which require HTTPS. It also flips the demo-login default off, which is what you want.
- **A real `JWT_SECRET`.** Generate it with `openssl rand -base64 32`. The dev default must never reach production; a known signing key means anyone can forge a session.
- **`DEMO_LOGIN_ENABLED` left off.** With `SECURE_COOKIES=true` it defaults off, so you get this for free unless you explicitly turn it back on. Only turn it on for an HTTPS review deployment where a passwordless login is an acceptable convenience, and understand that it is an unauthenticated route handing out valid sessions. See [Configuration](configuration.md#demo-login).

## Free-tier caveats

The free tier is fine for a review deployment and not fine for anything real, for two reasons worth knowing up front:

- The API sleeps after roughly 15 minutes of inactivity and takes a few seconds to wake on the next request. The first visit after an idle period will be slow.
- A free Postgres instance is removed after about 30 days. Any data in it goes with it.

If you are demonstrating Atrium, both are acceptable. If you are running it for real, you want a paid database with backups and an API that does not sleep, and you want to revisit the [Configuration](configuration.md#sessions-and-tokens) notes on token lifetime and the lack of session revocation.

## A production checklist

Before you consider a deployment done:

1. Migrations applied against the hosted database.
2. Seed data loaded, or real data in place.
3. `SECURE_COOKIES=true` on the API.
4. A real, secret `JWT_SECRET` on the API.
5. `DEMO_LOGIN_ENABLED` off (the default when cookies are secured).
6. The SPA and API served from one origin, with `/api` routed to the backend.
7. A visit to the public URL that lets you register, sign in, and make a booking end to end.

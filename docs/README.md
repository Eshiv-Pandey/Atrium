# Atrium documentation

This folder is the long-form documentation for Atrium, a room booking system built on a Go and PostgreSQL backend with a React and TypeScript frontend. The top-level [README](../README.md) is the short version: what the project is, how to start it, and the one idea the design is built around. The pages here go deeper on each of those topics.

## Where to start

If you are new to the project, read them roughly in this order:

1. [Getting started](getting-started.md) covers prerequisites, bringing the stack up, the seeded accounts, and running the backend or frontend on their own.
2. [Architecture](architecture.md) walks through the layers on both sides and how a request travels through them.
3. [Concurrency](concurrency.md) is the heart of the project. It explains why two people cannot book the same room at the same second, and why that guarantee lives in the database rather than in application code. If you only read one page, read this one.

## Reference

The rest are reference pages you can jump to when you need them:

- [Database](database.md) covers the schema, the constraints that enforce the rules, the indexes, and how time is represented.
- [API reference](api-reference.md) lists every endpoint, who can reach it, and the single error shape the whole API returns.
- [Configuration](configuration.md) is the full list of environment variables and booking policy constants, with the reasoning behind the ones that have no safe default.
- [Testing](testing.md) explains how the suites are structured on both sides, why the integration tests need a real database, and which single test carries the most weight.
- [Deployment](deployment.md) walks through the Render blueprint, running migrations and seeding against a hosted database, and the free-tier caveats worth knowing first.

## A note on scope

These pages describe how Atrium actually works today, not a wish list. Where a decision looks unusual (a 404 where you expected a 403, an array column where you expected a join table, no background job where you expected one), the docs explain the reasoning rather than leaving you to guess. If something here does not match the code, the code is right and the doc is stale, so please open an issue or fix it.

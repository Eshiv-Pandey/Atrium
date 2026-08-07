-- Atrium: initial schema.
--
-- The load-bearing decision in this file is the exclusion constraint on
-- bookings (see bottom). Everything else is conventional.

BEGIN;

-- gen_random_uuid() lives in pgcrypto on PG 12 and earlier, and in core from
-- PG 13. Requesting it explicitly keeps the migration portable across the
-- Compose image and whichever managed Postgres we deploy to.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- btree_gist lets a GiST index carry a scalar (room_id, compared with =)
-- alongside a range (compared with &&). Without it, the exclusion constraint
-- below cannot be created: GiST has no native equality operator class for uuid.
CREATE EXTENSION IF NOT EXISTS btree_gist;


-- ---------------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------------

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    name          text        NOT NULL,
    role          text        NOT NULL DEFAULT 'member',
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_role_valid CHECK (role IN ('member', 'admin')),
    CONSTRAINT users_email_not_blank CHECK (length(trim(email)) > 0),
    CONSTRAINT users_name_not_blank  CHECK (length(trim(name)) > 0)
);

-- Case-insensitive uniqueness: "Ada@x.com" and "ada@x.com" are the same
-- account. Enforced in the index rather than by lowercasing in application
-- code, so it holds regardless of which code path inserts.
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));


-- ---------------------------------------------------------------------------
-- rooms
-- ---------------------------------------------------------------------------

CREATE TABLE rooms (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    capacity   int         NOT NULL,
    amenities  text[]      NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT rooms_capacity_positive CHECK (capacity > 0),
    CONSTRAINT rooms_name_not_blank    CHECK (length(trim(name)) > 0)
);

CREATE UNIQUE INDEX rooms_name_lower_key ON rooms (lower(name));

-- amenities is a text[] rather than a normalised amenity table plus a join.
-- At this scale the array is a tag set: never queried on its own, only ever
-- filtered as "room has all of these". A GIN index makes @> containment fast,
-- and we avoid two tables and a join for data with no independent identity.
-- If amenities ever needed their own attributes (an icon, a description, a
-- per-room quantity) the normalised form would win; they don't.
CREATE INDEX rooms_amenities_idx ON rooms USING gin (amenities);


-- ---------------------------------------------------------------------------
-- bookings
-- ---------------------------------------------------------------------------

CREATE TABLE bookings (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id        uuid        NOT NULL REFERENCES rooms (id) ON DELETE RESTRICT,
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    start_time     timestamptz NOT NULL,
    end_time       timestamptz NOT NULL,
    attendee_count int         NOT NULL DEFAULT 1,
    status         text        NOT NULL DEFAULT 'confirmed',
    checked_in_at  timestamptz,
    released_at    timestamptz,
    cancelled_at   timestamptz,
    idempotency_key text,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT bookings_status_valid
        CHECK (status IN ('confirmed', 'cancelled', 'released')),

    CONSTRAINT bookings_end_after_start
        CHECK (end_time > start_time),

    -- 15 minutes to 8 hours. Stated as an interval comparison rather than
    -- EXTRACT(EPOCH ...) arithmetic because it reads as the rule it encodes.
    CONSTRAINT bookings_duration_within_bounds
        CHECK (end_time - start_time BETWEEN interval '15 minutes'
                                         AND interval '8 hours'),

    CONSTRAINT bookings_attendee_count_positive
        CHECK (attendee_count > 0),

    -- Timestamps must agree with the status they describe, so an inconsistent
    -- row cannot exist even if a future code path forgets to set one.
    CONSTRAINT bookings_cancelled_at_matches_status
        CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),

    CONSTRAINT bookings_released_at_matches_status
        CHECK ((status = 'released') = (released_at IS NOT NULL)),

    -- A released booking is by definition one nobody checked into.
    CONSTRAINT bookings_released_implies_no_checkin
        CHECK (status <> 'released' OR checked_in_at IS NULL)
);

-- ROOM DELETION: ON DELETE RESTRICT above, deliberately, not CASCADE.
-- Deleting a room should not silently erase the bookings people are relying
-- on. The admin delete path checks for future confirmed bookings and returns
-- 409 with the conflicts listed; RESTRICT is the backstop if anything tries
-- to bypass that check.

-- Availability queries always filter to confirmed bookings for a room within
-- a time window, so the index is partial on that predicate. Cancelled and
-- released rows are dead weight for reads and are excluded.
CREATE INDEX bookings_room_time_idx
    ON bookings (room_id, start_time, end_time)
    WHERE status = 'confirmed';

-- "My bookings", newest first.
CREATE INDEX bookings_user_start_idx ON bookings (user_id, start_time DESC);

-- Idempotency-Key replay detection, scoped per user so two users cannot
-- collide on the same client-generated key. Partial, because the column is
-- NULL for requests that did not send the header.
CREATE UNIQUE INDEX bookings_user_idempotency_key
    ON bookings (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;


-- THE CONSTRAINT THIS SCHEMA EXISTS FOR
--
-- Two overlapping confirmed bookings for one room cannot exist. Not
-- "are prevented by the service layer" -- cannot exist.
--
-- The alternative is read-then-write in application code: SELECT to check the
-- slot is free, then INSERT. That is a lost-update race. Two concurrent
-- requests both read "free" and both insert, and no amount of care in Go
-- fixes it, because the gap between the two statements is where the bug
-- lives. Serialising on the room with SELECT ... FOR UPDATE closes the race
-- but turns every booking into a per-room lock queue.
--
-- An exclusion constraint moves the invariant into the only place that can
-- check it atomically. The happy path becomes a single INSERT with no prior
-- read at all; a conflict surfaces as SQLSTATE 23P01, which the handler maps
-- to 409.
--
-- Two details that matter:
--
--   '[)' bounds -- start inclusive, end exclusive. A booking ending at 11:00
--   and one starting at 11:00 do not overlap, so back-to-back bookings are
--   legal. Naive overlap logic (start_a < end_b AND start_b < end_a on closed
--   ranges) gets this wrong and rejects them.
--
--   WHERE status = 'confirmed' -- cancelled and released bookings stop
--   occupying their slot. This is also what makes lazy no-show release work:
--   flipping a row to 'released' drops it out of this index, freeing the slot
--   in the same transaction as the insert that replaces it.
ALTER TABLE bookings
    ADD CONSTRAINT bookings_no_overlap
    EXCLUDE USING gist (
        room_id WITH =,
        tstzrange(start_time, end_time, '[)') WITH &&
    )
    WHERE (status = 'confirmed');

COMMIT;
